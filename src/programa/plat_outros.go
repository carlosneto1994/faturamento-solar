//go:build !windows

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func pastaDocumentos() string {
	if d := os.Getenv("FATSOLAR_DOCS"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Documents")
}
func abrirJanela(url string) {
	if os.Getenv("FATSOLAR_SEM_JANELA") != "" {
		return
	}
	_ = exec.Command("xdg-open", url).Start()
}
func abrirPasta(p string)   { _ = exec.Command("xdg-open", p).Start() }
func mensagemErro(t string) { fmt.Fprintln(os.Stderr, t) }
func desinstalar()          { fmt.Println("desinstalação só no Windows") }

func prepararProcesso(c *exec.Cmd) {}

func notificarWindows(titulo, corpo string) error {
	log.Println("[notificação]", titulo, "|", strings.ReplaceAll(corpo, "\n", " / "))
	return nil
}
func agendarTarefa(horario string) error { return nil }
