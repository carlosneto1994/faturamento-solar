//go:build windows

package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32      = syscall.NewLazyDLL("user32.dll")
	messageBox  = user32.NewProc("MessageBoxW")
	shell32     = syscall.NewLazyDLL("shell32.dll")
	knownFolder = shell32.NewProc("SHGetKnownFolderPath")
	ole32       = syscall.NewLazyDLL("ole32.dll")
	coTaskFree  = ole32.NewProc("CoTaskMemFree")
)

const (
	mbOK        = 0x0
	mbYesNo     = 0x4
	mbIconError = 0x10
	mbIconQuest = 0x20
	mbIconInfo  = 0x40
	idYes       = 6
)

func caixa(texto, titulo string, tipo uintptr) int {
	t, _ := syscall.UTF16PtrFromString(texto)
	c, _ := syscall.UTF16PtrFromString(titulo)
	r, _, _ := messageBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), tipo)
	return int(r)
}

func mensagemErro(t string) { caixa(t, "Faturamento Solar", mbOK|mbIconError) }

// FOLDERID_Documents {FDD39AD0-238F-46AF-ADB4-6C85480369C7}
var folderDocuments = syscall.GUID{Data1: 0xFDD39AD0, Data2: 0x238F, Data3: 0x46AF, Data4: [8]byte{0xAD, 0xB4, 0x6C, 0x85, 0x48, 0x03, 0x69, 0xC7}}

func pastaDocumentos() string {
	var p *uint16
	r, _, _ := knownFolder.Call(uintptr(unsafe.Pointer(&folderDocuments)), 0, 0, uintptr(unsafe.Pointer(&p)))
	if r == 0 && p != nil {
		s := syscall.UTF16ToString((*[32768]uint16)(unsafe.Pointer(p))[:])
		coTaskFree.Call(uintptr(unsafe.Pointer(p)))
		if s != "" {
			return s
		}
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, "Documents")
}

func oculto(c *exec.Cmd) *exec.Cmd {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}

func navegadorApp() string {
	cands := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("ProgramFiles"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), `Google\Chrome\Application\chrome.exe`),
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Google\Chrome\Application\chrome.exe`),
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func abrirJanela(url string) {
	if nav := navegadorApp(); nav != "" {
		if exec.Command(nav, "--app="+url, "--window-size=1360,900").Start() == nil {
			return
		}
	}
	_ = oculto(exec.Command("rundll32", "url.dll,FileProtocolHandler", url)).Start()
}

func abrirPasta(p string) { _ = exec.Command("explorer", p).Start() }

func desinstalar() {
	if caixa("Remover o Faturamento Solar deste computador?\n\nSeus dados e faturas continuam guardados em Documentos\\Faturamento Solar.", "Desinstalar Faturamento Solar", mbYesNo|mbIconQuest) != idYes {
		return
	}
	if r, err := (&http.Client{Timeout: 2 * time.Second}).Post("http://127.0.0.1:47831/api/sair", "text/plain", nil); err == nil {
		r.Body.Close()
		time.Sleep(time.Second)
	}
	ps := `$n='Faturamento Solar.lnk'; foreach($d in @([Environment]::GetFolderPath('Desktop'),[Environment]::GetFolderPath('Programs'))){ $f=Join-Path $d $n; if(Test-Path $f){Remove-Item $f -Force} }`
	_ = oculto(exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps)).Run()
	_ = oculto(exec.Command("reg", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\FaturamentoSolar`, "/f")).Run()
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	caixa("Faturamento Solar removido.\nSeus dados continuam em Documentos\\Faturamento Solar.", "Faturamento Solar", mbOK|mbIconInfo)
	_ = oculto(exec.Command("cmd", "/C", "ping 127.0.0.1 -n 3 >nul & rmdir /S /Q \""+dir+"\"")).Start()
}

func prepararProcesso(c *exec.Cmd) {}
