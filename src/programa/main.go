// Faturamento Solar — programa local: serve o sistema numa janela do navegador e guarda os dados em Documentos.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed web
var webFS embed.FS

const porta = "127.0.0.1:47831"
const assinatura = "faturamento-solar"

type banco struct {
	mu      sync.Mutex
	arquivo string
	Dados   map[string]map[string]json.RawMessage `json:"dados"`
}

var (
	db          *banco
	pastaBase   string
	ultimoSinal = time.Now()
	sinalMu     sync.Mutex
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--desinstalar" {
		desinstalar()
		return
	}
	pastaBase = filepath.Join(pastaDocumentos(), "Faturamento Solar")
	_ = os.MkdirAll(filepath.Join(pastaBase, "Faturas"), 0o755)
	_ = os.MkdirAll(filepath.Join(pastaBase, "Backups"), 0o755)
	arqLog := filepath.Join(pastaBase, "registro.log")
	if st, err := os.Stat(arqLog); err == nil && st.Size() > 2<<20 { // mantém o registro com até ~1 MB
		if b, err := os.ReadFile(arqLog); err == nil {
			_ = os.WriteFile(arqLog, b[len(b)-(1<<20):], 0o644)
		}
	}
	if f, err := os.OpenFile(arqLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(f)
	}
	url := "http://" + porta + "/"
	if len(os.Args) > 1 && os.Args[1] == "--avisos" { // chamado pela tarefa agendada
		modoAvisos()
		return
	}
	semJanela := len(os.Args) > 1 && os.Args[1] == "--sem-janela"
	limparAntigo()

	ln, err := net.Listen("tcp", porta)
	for i := 0; err != nil && semJanela && i < 30; i++ { // reinício após atualização: espera a versão anterior liberar a porta
		time.Sleep(500 * time.Millisecond)
		ln, err = net.Listen("tcp", porta)
	}
	if err != nil {
		// Já está aberto: só mostra a janela de novo
		if jaRodando() {
			abrirJanela(url)
			return
		}
		mensagemErro("Não consegui iniciar o Faturamento Solar (porta 47831 ocupada por outro programa).")
		return
	}
	db, err = abrirBanco(filepath.Join(pastaBase, "dados.json"))
	if err != nil {
		mensagemErro("Não consegui ler os dados em " + pastaBase + ":\n" + err.Error())
		return
	}
	fazerBackup()

	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	estaticos := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".mjs") {
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		}
		w.Header().Set("Cache-Control", "no-cache")
		estaticos.ServeHTTP(w, r)
	})
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) {
		sinal()
		io.WriteString(w, assinatura)
	})
	mux.HandleFunc("/api/sair", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "método", 405)
			return
		}
		io.WriteString(w, "ok")
		go func() { time.Sleep(200 * time.Millisecond); os.Exit(0) }()
	})
	var srv *http.Server
	mux.HandleFunc("/api/atualizacao", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			go verificarAtualizacao()
		}
		atual.mu.Lock()
		atual.Versao = versaoAtual
		b, _ := json.Marshal(atual)
		atual.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	})
	mux.HandleFunc("/api/reiniciar", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "método", 405)
			return
		}
		io.WriteString(w, "ok")
		go func() {
			time.Sleep(300 * time.Millisecond)
			reiniciar(func() { db.mu.Lock(); _ = srv.Close() })
		}()
	})
	mux.HandleFunc("/api/log", func(w http.ResponseWriter, r *http.Request) {
		arq := filepath.Join(pastaBase, "registro.log")
		switch r.Method {
		case http.MethodPost:
			b, _ := io.ReadAll(io.LimitReader(r.Body, 4000))
			log.Println("[tela]", strings.ReplaceAll(string(b), "\n", " "))
			io.WriteString(w, "ok")
		case http.MethodGet:
			conteudo, _ := os.ReadFile(arq)
			linhas := strings.Split(strings.TrimSpace(string(conteudo)), "\n")
			if len(linhas) > 200 {
				linhas = linhas[len(linhas)-200:]
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			io.WriteString(w, strings.Join(linhas, "\n"))
		default:
			http.Error(w, "método", 405)
		}
	})
	mux.HandleFunc("/api/avisos/executar", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "método", 405)
			return
		}
		res := executarAvisos("verificação manual")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})
	mux.HandleFunc("/api/avisos/testar", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "método", 405)
			return
		}
		db.mu.Lock()
		cfg, empresa := lerCfgAvisos()
		db.mu.Unlock()
		msg := "☀ Faturamento Solar: teste de aviso pelo WhatsApp. Se você recebeu esta mensagem, os avisos de vencimento vão chegar aqui."
		if empresa != "" {
			msg += " (" + empresa + ")"
		}
		res := map[string]string{"whats": "ok", "windows": "ok"}
		if err := enviarWhatsApp(cfg, msg); err != nil {
			res["whats"] = err.Error()
		}
		if err := notificarWindows("Faturamento Solar: teste de aviso", "As notificações de vencimento vão aparecer assim."); err != nil {
			res["windows"] = err.Error()
		}
		log.Println("teste de avisos:", res)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})
	mux.HandleFunc("/api/avisos/agendar", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "método", 405)
			return
		}
		db.mu.Lock()
		cfg, _ := lerCfgAvisos()
		db.mu.Unlock()
		res := map[string]string{"agenda": "ok", "horario": cfg.Horario}
		if err := agendarTarefa(cfg.Horario); err != nil {
			res["agenda"] = err.Error()
			log.Println("agendar avisos:", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(res)
	})
	mux.HandleFunc("/api/dados", apiDados)
	mux.HandleFunc("/api/doc/", apiDoc)
	mux.HandleFunc("/api/arquivo", apiArquivo)
	mux.HandleFunc("/api/abrir", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "método", 405)
			return
		}
		alvo := pastaBase
		if r.URL.Query().Get("alvo") == "faturas" {
			alvo = filepath.Join(pastaBase, "Faturas")
		}
		abrirPasta(alvo)
		io.WriteString(w, "ok")
	})

	// Encerra sozinho quando a janela fica fechada por um tempo
	go func() {
		for {
			time.Sleep(time.Minute)
			sinalMu.Lock()
			parado := time.Since(ultimoSinal)
			sinalMu.Unlock()
			if parado > 15*time.Minute {
				log.Println("sem uso há 15 min, encerrando")
				os.Exit(0)
			}
		}
	}()

	if !semJanela {
		go func() { time.Sleep(300 * time.Millisecond); abrirJanela(url) }()
	}
	if os.Getenv("FATSOLAR_SEM_ATUALIZAR") == "" {
		go cicloAtualizacao()
	}
	go cicloAvisos()
	go func() {
		db.mu.Lock()
		cfg, _ := lerCfgAvisos()
		db.mu.Unlock()
		if err := agendarTarefa(cfg.Horario); err != nil {
			log.Println("agendar avisos:", err)
		}
	}()
	log.Println("iniciado versão", versaoAtual, "em", url, "dados em", pastaBase)
	srv = &http.Server{Handler: protegido(mux), ReadHeaderTimeout: 10 * time.Second}
	log.Println(srv.Serve(ln))
	if reiniciando.Load() {
		select {} // aguarda o os.Exit do reinício
	}
}

func sinal() { sinalMu.Lock(); ultimoSinal = time.Now(); sinalMu.Unlock() }

// Só aceita chamadas da própria máquina vindas da página do programa
func protegido(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != porta && r.Host != "localhost:47831" {
			http.Error(w, "acesso negado", 403)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+porta && o != "http://localhost:47831" {
			http.Error(w, "acesso negado", 403)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func jaRodando() bool {
	c := http.Client{Timeout: 2 * time.Second}
	r, err := c.Get("http://" + porta + "/api/ping")
	if err != nil {
		return false
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return string(b) == assinatura
}

// ---------------- dados ----------------

var colecoes = map[string]bool{"clientes": true, "faturas": true, "config": true}
var idValido = regexp.MustCompile(`^[A-Za-z0-9._~:@+\-]{1,200}$`)

func abrirBanco(arq string) (*banco, error) {
	b := &banco{arquivo: arq, Dados: map[string]map[string]json.RawMessage{}}
	conteudo, err := os.ReadFile(arq)
	if err == nil {
		if err := json.Unmarshal(conteudo, b); err != nil {
			return nil, fmt.Errorf("arquivo de dados danificado: %v (há cópias na pasta Backups)", err)
		}
	} else if os.IsNotExist(err) {
		b.Dados["config"] = map[string]json.RawMessage{"empresa": json.RawMessage(empresaInicial)}
		if err := b.gravarArquivo(); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	for c := range colecoes {
		if b.Dados[c] == nil {
			b.Dados[c] = map[string]json.RawMessage{}
		}
	}
	return b, nil
}

const empresaInicial = `{}`

func (b *banco) gravarArquivo() error {
	conteudo, err := json.MarshalIndent(b, "", " ")
	if err != nil {
		return err
	}
	tmp := b.arquivo + ".tmp"
	if err := os.WriteFile(tmp, conteudo, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, b.arquivo)
}

func fazerBackup() {
	dir := filepath.Join(pastaBase, "Backups")
	destino := filepath.Join(dir, "dados-"+time.Now().Format("2006-01-02")+".json")
	if _, err := os.Stat(destino); err == nil {
		return
	}
	if conteudo, err := os.ReadFile(db.arquivo); err == nil {
		_ = os.WriteFile(destino, conteudo, 0o644)
	}
	itens, _ := filepath.Glob(filepath.Join(dir, "dados-*.json"))
	sort.Strings(itens)
	for len(itens) > 60 {
		_ = os.Remove(itens[0])
		itens = itens[1:]
	}
}

func apiDados(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "método", 405)
		return
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	lista := func(col string) []json.RawMessage {
		ids := make([]string, 0, len(db.Dados[col]))
		for id := range db.Dados[col] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		out := make([]json.RawMessage, 0, len(ids))
		for _, id := range ids {
			out = append(out, comId(db.Dados[col][id], id))
		}
		return out
	}
	resp := map[string]any{
		"clientes": lista("clientes"),
		"faturas":  lista("faturas"),
		"empresa":  json.RawMessage("{}"),
		"pastas":   map[string]string{"base": pastaBase, "faturas": filepath.Join(pastaBase, "Faturas")},
	}
	if e, ok := db.Dados["config"]["empresa"]; ok {
		resp["empresa"] = e
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func comId(doc json.RawMessage, id string) json.RawMessage {
	var m map[string]any
	if json.Unmarshal(doc, &m) != nil {
		return doc
	}
	m["id"] = id
	b, _ := json.Marshal(m)
	return b
}

func apiDoc(w http.ResponseWriter, r *http.Request) {
	partes := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/doc/"), "/")
	if len(partes) != 2 || !colecoes[partes[0]] || !idValido.MatchString(partes[1]) {
		http.Error(w, "caminho inválido", 400)
		return
	}
	col, id := partes[0], partes[1]
	db.mu.Lock()
	defer db.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		corpo, err := io.ReadAll(io.LimitReader(r.Body, 20<<20))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		var m map[string]any
		if err := json.Unmarshal(corpo, &m); err != nil {
			http.Error(w, "json inválido", 400)
			return
		}
		delete(m, "id")
		limpo, _ := json.Marshal(m)
		anterior, existia := db.Dados[col][id]
		db.Dados[col][id] = limpo
		if err := db.gravarArquivo(); err != nil {
			if existia {
				db.Dados[col][id] = anterior
			} else {
				delete(db.Dados[col], id)
			}
			log.Println("erro ao gravar:", err)
			http.Error(w, err.Error(), 500)
			return
		}
	case http.MethodDelete:
		anterior, existia := db.Dados[col][id]
		delete(db.Dados[col], id)
		if err := db.gravarArquivo(); err != nil {
			if existia {
				db.Dados[col][id] = anterior
			}
			http.Error(w, err.Error(), 500)
			return
		}
	default:
		http.Error(w, "método", 405)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"ok":true}`)
}

var nomeArquivo = regexp.MustCompile(`[^A-Za-z0-9._ \-()]`)

func apiArquivo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "método", 405)
		return
	}
	nome := filepath.Base(r.URL.Query().Get("nome"))
	nome = nomeArquivo.ReplaceAllString(nome, "_")
	ext := strings.ToLower(filepath.Ext(nome))
	if nome == "" || nome == "." || (ext != ".pdf" && ext != ".png" && ext != ".csv") {
		http.Error(w, "nome inválido", 400)
		return
	}
	dir := filepath.Join(pastaBase, "Faturas")
	if ext == ".csv" {
		dir = filepath.Join(pastaBase, "Planilhas")
	}
	_ = os.MkdirAll(dir, 0o755)
	destino := filepath.Join(dir, nome)
	conteudo, err := io.ReadAll(io.LimitReader(r.Body, 100<<20))
	if err != nil || len(conteudo) == 0 {
		http.Error(w, "arquivo vazio", 400)
		return
	}
	if err := os.WriteFile(destino, conteudo, 0o644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"caminho": destino})
}
