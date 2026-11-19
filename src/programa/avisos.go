package main

// Avisos de vencimento: contas da Energisa que você paga e faturas a receber dos clientes.
// Avisa com antecedência (padrão 3 dias), no dia e quando atrasa — cada aviso uma vez só.
// Canais: notificação do Windows e WhatsApp (CallMeBot), se configurado em "Avisos".

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
)

type cfgAvisos struct {
	Ativo       *bool  `json:"ativo"`
	DiasAntes   int    `json:"diasAntes"`
	Horario     string `json:"horario"`
	WhatsNumero string `json:"whatsNumero"`
	WhatsApikey string `json:"whatsApikey"`
}

type itemAviso struct {
	Chave string
	Tipo  string // energisa | cliente
	Etapa string // antes | hoje | atraso
	Dias  int
	Venc  time.Time
	Valor float64
	Texto string
}

type faturaMin struct {
	NumeroTxt    string `json:"numeroTxt"`
	Vencimento   string `json:"vencimento"`
	Status       string `json:"status"`
	EnergisaPaga bool   `json:"energisaPaga"`
	Cliente      struct {
		Nome    string `json:"nome"`
		UC      string `json:"uc"`
		Titular string `json:"titular"`
	} `json:"cliente"`
	Conta struct {
		Vencimento string  `json:"vencimento"`
		TotalConta float64 `json:"totalConta"`
		MesRef     int     `json:"mesRef"`
		AnoRef     int     `json:"anoRef"`
	} `json:"conta"`
	Calculo struct {
		ValorCobrado    float64 `json:"valorCobrado"`
		IncluirEnergisa bool    `json:"incluirEnergisa"`
	} `json:"calculo"`
}

func lerCfgAvisos() (cfgAvisos, string) {
	var emp struct {
		Nome   string    `json:"nome"`
		Avisos cfgAvisos `json:"avisos"`
	}
	if e, ok := db.Dados["config"]["empresa"]; ok {
		_ = json.Unmarshal(e, &emp)
	}
	c := emp.Avisos
	if c.DiasAntes <= 0 {
		c.DiasAntes = 3
	}
	if c.Horario == "" {
		c.Horario = "08:00"
	}
	return c, emp.Nome
}

func dataBR(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("02/01/2006", strings.TrimSpace(s), time.Local)
	return t, err == nil
}

func reais(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	inteiro, dec := s[:len(s)-3], s[len(s)-2:]
	neg := strings.HasPrefix(inteiro, "-")
	inteiro = strings.TrimPrefix(inteiro, "-")
	var b strings.Builder
	for i, c := range inteiro {
		if i > 0 && (len(inteiro)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	r := "R$ " + b.String() + "," + dec
	if neg {
		r = "-" + r
	}
	return r
}

var mesesAbrev = []string{"", "jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}

func finalUC(uc string) string {
	if len(uc) > 6 {
		return "…" + uc[len(uc)-6:]
	}
	return uc
}

// Lista o que precisa de aviso hoje (sem olhar o que já foi enviado)
func coletarAvisos(hoje time.Time, diasAntes int) []itemAviso {
	var out []itemAviso
	for id, raw := range db.Dados["faturas"] {
		var f faturaMin
		if json.Unmarshal(raw, &f) != nil {
			continue
		}
		ref := ""
		if f.Conta.MesRef >= 1 && f.Conta.MesRef <= 12 {
			ref = fmt.Sprintf("%s/%d", mesesAbrev[f.Conta.MesRef], f.Conta.AnoRef)
		}
		add := func(tipo string, venc time.Time, valor float64, texto string) {
			d := int(venc.Sub(hoje).Hours() / 24)
			etapa := ""
			switch {
			case d < 0:
				etapa = "atraso"
			case d == 0:
				etapa = "hoje"
			case d <= diasAntes:
				etapa = "antes"
			default:
				return
			}
			out = append(out, itemAviso{Chave: id + "|" + tipo + "|" + etapa, Tipo: tipo, Etapa: etapa, Dias: d, Venc: venc, Valor: valor, Texto: texto})
		}
		if f.Calculo.IncluirEnergisa && !f.EnergisaPaga {
			if v, ok := dataBR(f.Conta.Vencimento); ok {
				tit := f.Cliente.Titular
				if tit == "" {
					tit = f.Cliente.Nome
				}
				add("energisa", v, f.Conta.TotalConta, fmt.Sprintf("UC %s (%s) %s · cliente %s", finalUC(f.Cliente.UC), tit, ref, f.Cliente.Nome))
			}
		}
		if f.Status != "paga" {
			if v, ok := dataBR(f.Vencimento); ok {
				add("cliente", v, f.Calculo.ValorCobrado, fmt.Sprintf("%s · fatura Nº %s %s", f.Cliente.Nome, f.NumeroTxt, ref))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Venc.Before(out[j].Venc) })
	return out
}

func textoQuando(a itemAviso) string {
	switch a.Etapa {
	case "atraso":
		return fmt.Sprintf("ATRASADA desde %s", a.Venc.Format("02/01"))
	case "hoje":
		return "VENCE HOJE"
	}
	if a.Dias == 1 {
		return "vence amanhã (" + a.Venc.Format("02/01") + ")"
	}
	return fmt.Sprintf("vence em %d dias (%s)", a.Dias, a.Venc.Format("02/01"))
}

func montarMensagem(itens []itemAviso, empresa string) (titulo, corpo string) {
	var pagar, receber []string
	totP, totR := 0.0, 0.0
	for _, a := range itens {
		l := fmt.Sprintf("• %s – %s – %s", textoQuando(a), reais(a.Valor), a.Texto)
		if a.Tipo == "energisa" {
			pagar = append(pagar, l)
			totP += a.Valor
		} else {
			receber = append(receber, l)
			totR += a.Valor
		}
	}
	var b strings.Builder
	b.WriteString("☀ Faturamento Solar – avisos de " + time.Now().Format("02/01/2006"))
	if empresa != "" {
		b.WriteString(" (" + empresa + ")")
	}
	if len(pagar) > 0 {
		b.WriteString(fmt.Sprintf("\n\n💡 PAGAR À ENERGISA (%s):\n%s", reais(totP), strings.Join(pagar, "\n")))
	}
	if len(receber) > 0 {
		b.WriteString(fmt.Sprintf("\n\n💰 RECEBER DOS CLIENTES (%s):\n%s", reais(totR), strings.Join(receber, "\n")))
	}
	partes := []string{}
	if len(pagar) > 0 {
		partes = append(partes, fmt.Sprintf("%d conta(s) da Energisa para pagar", len(pagar)))
	}
	if len(receber) > 0 {
		partes = append(partes, fmt.Sprintf("%d fatura(s) para receber", len(receber)))
	}
	return strings.Join(partes, " · "), b.String()
}

func enviarWhatsApp(c cfgAvisos, texto string) error {
	num := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == '+' {
			return r
		}
		return -1
	}, c.WhatsNumero)
	if num == "" || strings.TrimSpace(c.WhatsApikey) == "" {
		return fmt.Errorf("WhatsApp não configurado")
	}
	if !strings.HasPrefix(num, "+") {
		if !strings.HasPrefix(num, "55") {
			num = "55" + num
		}
		num = "+" + num
	}
	if len(texto) > 3500 {
		texto = texto[:3500] + "\n…"
	}
	u := "https://api.callmebot.com/whatsapp.php?phone=" + url.QueryEscape(num) + "&text=" + url.QueryEscape(texto) + "&apikey=" + url.QueryEscape(strings.TrimSpace(c.WhatsApikey))
	if base := os.Getenv("FATSOLAR_WHATS_URL"); base != "" { // testes
		u = base + "?phone=" + url.QueryEscape(num) + "&text=" + url.QueryEscape(texto) + "&apikey=" + url.QueryEscape(c.WhatsApikey)
	}
	cli := &http.Client{Timeout: 30 * time.Second}
	r, err := cli.Get(u)
	if err != nil {
		return fmt.Errorf("sem conexão com o serviço do WhatsApp")
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(r.Body, 4000))
	txt := strings.ToLower(string(b))
	recusou := r.StatusCode != 200 || strings.Contains(txt, "apikey is invalid") || strings.Contains(txt, "invalid apikey") || strings.Contains(txt, "need to get the apikey") || strings.Contains(txt, "not authorized")
	if recusou || (strings.Contains(txt, "error") && !strings.Contains(txt, "queued") && !strings.Contains(txt, "sent")) {
		limpo := strings.TrimSpace(stripTags(string(b)))
		if len(limpo) > 200 {
			limpo = limpo[:200]
		}
		return fmt.Errorf("o serviço do WhatsApp recusou (%d): %s", r.StatusCode, limpo)
	}
	return nil
}

func stripTags(s string) string {
	var b strings.Builder
	dentro := false
	for _, c := range s {
		switch {
		case c == '<':
			dentro = true
		case c == '>':
			dentro = false
		case !dentro:
			b.WriteRune(c)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

type resultadoAvisos struct {
	Novos    int    `json:"novos"`
	Whats    string `json:"whats"`
	Windows  string `json:"windows"`
	Mensagem string `json:"mensagem,omitempty"`
}

// Envia os avisos que ainda não foram enviados. Chamar sem db.mu travado.
func executarAvisos(origem string) resultadoAvisos {
	log.Println("avisos: verificação iniciada (" + origem + ")")
	defer func() {
		if e := recover(); e != nil {
			log.Printf("ERRO GRAVE nos avisos: %v\n%s", e, debug.Stack())
		}
	}()
	db.mu.Lock()
	cfg, empresa := lerCfgAvisos()
	res := resultadoAvisos{}
	if cfg.Ativo != nil && !*cfg.Ativo {
		db.mu.Unlock()
		res.Whats, res.Windows = "desativado", "desativado"
		return res
	}
	hoje := time.Now()
	hoje = time.Date(hoje.Year(), hoje.Month(), hoje.Day(), 0, 0, 0, 0, time.Local)
	enviados := map[string]string{}
	if e, ok := db.Dados["config"]["avisos"]; ok {
		var x struct {
			Enviados map[string]string `json:"enviados"`
		}
		_ = json.Unmarshal(e, &x)
		if x.Enviados != nil {
			enviados = x.Enviados
		}
	}
	todos := coletarAvisos(hoje, cfg.DiasAntes)
	zapOk := cfg.WhatsNumero != "" && cfg.WhatsApikey != ""
	var paraWin, paraZap []itemAviso // cada canal guarda o que já recebeu
	for _, a := range todos {
		if _, ja := enviados[a.Chave+"|win"]; !ja {
			paraWin = append(paraWin, a)
		}
		if _, ja := enviados[a.Chave+"|zap"]; zapOk && !ja {
			paraZap = append(paraZap, a)
		}
	}
	db.mu.Unlock()
	marcar := map[string]bool{}
	if len(paraWin) > 0 {
		titulo, corpo := montarMensagem(paraWin, empresa)
		res.Mensagem = corpo
		if err := notificarWindows("Faturamento Solar: "+titulo, resumoCurto(paraWin)); err != nil {
			res.Windows = "erro: " + err.Error() // a janela de aviso foi mostrada no lugar
		} else {
			res.Windows = "ok"
		}
		for _, a := range paraWin {
			marcar[a.Chave+"|win"] = true
		}
	} else {
		res.Windows = "nada novo"
	}
	if !zapOk {
		res.Whats = "não configurado"
	} else if len(paraZap) > 0 {
		_, corpo := montarMensagem(paraZap, empresa)
		res.Mensagem = corpo
		if err := enviarWhatsApp(cfg, corpo); err != nil {
			res.Whats = "erro: " + err.Error() // tenta de novo na próxima verificação
		} else {
			res.Whats = "ok"
			for _, a := range paraZap {
				marcar[a.Chave+"|zap"] = true
			}
		}
	} else {
		res.Whats = "nada novo"
	}
	n := len(paraWin)
	if len(paraZap) > n {
		n = len(paraZap)
	}
	res.Novos = n
	if n > 0 {
		log.Printf("avisos (%s): %d no computador, %d no WhatsApp; windows=%s; whatsapp=%s", origem, len(paraWin), len(paraZap), res.Windows, res.Whats)
	}
	if len(marcar) > 0 {
		db.mu.Lock()
		hojeTxt := hoje.Format("2006-01-02")
		for k := range marcar {
			enviados[k] = hojeTxt
		}
		limite := hoje.AddDate(0, 0, -120).Format("2006-01-02")
		for k, v := range enviados {
			if v < limite {
				delete(enviados, k)
			}
		}
		b, _ := json.Marshal(map[string]any{"enviados": enviados})
		db.Dados["config"]["avisos"] = b
		if err := db.gravarArquivo(); err != nil {
			log.Println("avisos: erro ao gravar:", err)
		}
		db.mu.Unlock()
	}
	return res
}

func resumoCurto(itens []itemAviso) string {
	var ls []string
	for i, a := range itens {
		if i == 4 {
			ls = append(ls, fmt.Sprintf("e mais %d…", len(itens)-4))
			break
		}
		tipo := "Pagar Energisa"
		if a.Tipo == "cliente" {
			tipo = "Receber"
		}
		ls = append(ls, fmt.Sprintf("%s %s: %s – %s", tipo, strings.ToLower(textoQuando(a)), reais(a.Valor), a.Texto))
	}
	return strings.Join(ls, "\n")
}

func cicloAvisos() {
	time.Sleep(10 * time.Second)
	for {
		executarAvisos("programa aberto")
		time.Sleep(30 * time.Minute)
	}
}

// Modo usado pela tarefa agendada do Windows: verifica e sai
func modoAvisos() {
	c := http.Client{Timeout: 5 * time.Minute}
	if jaRodando() {
		if r, err := c.Post("http://"+porta+"/api/avisos/executar", "text/plain", nil); err == nil {
			r.Body.Close()
		}
		return
	}
	var err error
	db, err = abrirBanco(pastaBase + string(os.PathSeparator) + "dados.json")
	if err != nil {
		log.Println("avisos: não consegui ler os dados:", err)
		return
	}
	executarAvisos("tarefa agendada")
}
