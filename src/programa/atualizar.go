package main

// Atualização automática: o programa consulta versao.json no GitHub, baixa o executável novo,
// confere o SHA-256 e troca o próprio arquivo (o antigo vira .old e é apagado depois).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Definida na compilação: -ldflags "-X main.versaoAtual=1.1.0"
var versaoAtual = "0.0.0-dev"

const manifestoPadrao = "https://raw.githubusercontent.com/carlosneto1994/faturamento-solar/main/versao.json"

type manifesto struct {
	Versao   string            `json:"versao"`
	Notas    string            `json:"notas"`
	Arquivos map[string]arqMan `json:"arquivos"` // chave: "windows-amd64"
}
type arqMan struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type estadoAtualizacao struct {
	mu      sync.Mutex
	Status  string `json:"status"` // ocioso, verificando, baixando, pronta, atualizado, erro
	Nova    string `json:"nova,omitempty"`
	Notas   string `json:"notas,omitempty"`
	Erro    string `json:"erro,omitempty"`
	Versao  string `json:"versao"`
	Checado string `json:"checado,omitempty"`
}

var atual = &estadoAtualizacao{Status: "ocioso"}
var reiniciando atomic.Bool

// Caminho do executável guardado ao iniciar (depois da troca, o processo em uso aponta para o .old)
var caminhoExe = func() string { e, _ := os.Executable(); return e }()

func urlManifesto() string {
	if u := os.Getenv("FATSOLAR_MANIFESTO"); u != "" {
		return u
	}
	return manifestoPadrao
}

// "1.2.10" > "1.2.9"
func versaoMaior(a, b string) bool {
	pa, pb := strings.Split(strings.SplitN(a, "-", 2)[0], "."), strings.Split(strings.SplitN(b, "-", 2)[0], ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func limparAntigo() {
	if caminhoExe != "" {
		_ = os.Remove(caminhoExe + ".old")
		_ = os.Remove(caminhoExe + ".new")
	}
}

func cicloAtualizacao() {
	time.Sleep(3 * time.Second)
	for {
		verificarAtualizacao()
		time.Sleep(6 * time.Hour)
	}
}

func verificarAtualizacao() {
	atual.mu.Lock()
	if atual.Status == "baixando" || atual.Status == "pronta" {
		atual.mu.Unlock()
		return
	}
	atual.Status = "verificando"
	atual.mu.Unlock()

	nova, notas, err := baixarSeNovo()
	atual.mu.Lock()
	defer atual.mu.Unlock()
	atual.Checado = time.Now().Format("02/01/2006 15:04")
	switch {
	case err != nil:
		atual.Status, atual.Erro = "erro", err.Error()
		log.Println("atualização:", err)
	case nova != "":
		atual.Status, atual.Nova, atual.Notas, atual.Erro = "pronta", nova, notas, ""
		log.Println("atualização pronta:", nova)
	default:
		atual.Status, atual.Erro = "atualizado", ""
	}
}

func baixarSeNovo() (string, string, error) {
	cli := &http.Client{Timeout: 20 * time.Second}
	req, _ := http.NewRequest("GET", urlManifesto()+"?t="+strconv.FormatInt(time.Now().Unix(), 10), nil)
	req.Header.Set("Cache-Control", "no-cache")
	r, err := cli.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("sem conexão com o servidor de atualizações")
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return "", "", fmt.Errorf("servidor de atualizações respondeu %d", r.StatusCode)
	}
	var m manifesto
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&m); err != nil {
		return "", "", fmt.Errorf("manifesto inválido: %v", err)
	}
	if !versaoMaior(m.Versao, versaoAtual) {
		return "", "", nil
	}
	a, ok := m.Arquivos[runtime.GOOS+"-"+runtime.GOARCH]
	if !ok || a.URL == "" || len(a.SHA256) != 64 {
		return "", "", fmt.Errorf("versão %s sem arquivo para este computador", m.Versao)
	}
	atual.mu.Lock()
	atual.Status, atual.Nova = "baixando", m.Versao
	atual.mu.Unlock()

	exe := caminhoExe
	if exe == "" {
		return "", "", errors.New("não sei onde o programa está instalado")
	}
	novo := exe + ".new"
	dl := &http.Client{Timeout: 10 * time.Minute}
	resp, err := dl.Get(a.URL)
	if err != nil {
		return "", "", fmt.Errorf("falha ao baixar a versão %s", m.Versao)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("download respondeu %d", resp.StatusCode)
	}
	f, err := os.OpenFile(novo, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 200<<20))
	f.Close()
	if err != nil {
		os.Remove(novo)
		return "", "", fmt.Errorf("download interrompido")
	}
	if hex.EncodeToString(h.Sum(nil)) != strings.ToLower(a.SHA256) {
		os.Remove(novo)
		return "", "", errors.New("arquivo baixado não confere (SHA-256); atualização cancelada")
	}
	// Troca: o executável em uso pode ser renomeado no Windows
	_ = os.Remove(exe + ".old")
	if err := os.Rename(exe, exe+".old"); err != nil {
		os.Remove(novo)
		return "", "", fmt.Errorf("não consegui substituir o programa: %v", err)
	}
	if err := os.Rename(novo, exe); err != nil {
		_ = os.Rename(exe+".old", exe)
		return "", "", fmt.Errorf("não consegui instalar a nova versão: %v", err)
	}
	return m.Versao, m.Notas, nil
}

// Reinicia já na versão nova: fecha o servidor, abre o executável novo sem nova janela e sai
func reiniciar(fecharServidor func()) {
	exe := caminhoExe
	if exe == "" {
		return
	}
	reiniciando.Store(true)
	// A versão nova espera esta liberar a porta, então pode ser aberta antes
	cmd := exec.Command(exe, "--sem-janela")
	prepararProcesso(cmd)
	if err := cmd.Start(); err != nil {
		log.Println("reiniciar:", err)
	}
	fecharServidor()
	time.Sleep(200 * time.Millisecond)
	os.Exit(0)
}
