//go:build windows

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
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
	_ = oculto(exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", ps)).Run()
	_ = oculto(exec.Command("schtasks", "/Delete", "/F", "/TN", "Faturamento Solar Avisos")).Run()
	_ = oculto(exec.Command("reg", "delete", `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\FaturamentoSolar`, "/f")).Run()
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	caixa("Faturamento Solar removido.\nSeus dados continuam em Documentos\\Faturamento Solar.", "Faturamento Solar", mbOK|mbIconInfo)
	_ = oculto(exec.Command("cmd", "/C", "ping 127.0.0.1 -n 3 >nul & rmdir /S /Q \""+dir+"\"")).Start()
}

func prepararProcesso(c *exec.Cmd) {}

func escXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// Notificação do Windows (canto da tela). Usa o PowerShell, que já vem no Windows 10/11.
func notificarWindows(titulo, corpo string) error {
	xml := `<toast scenario="reminder"><visual><binding template="ToastGeneric"><text>` + escXML(titulo) + `</text><text>` + escXML(corpo) + `</text></binding></visual><actions><action content="OK" arguments="ok" activationType="system"/></actions></toast>`
	ps := `$ErrorActionPreference='Stop';` +
		`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null;` +
		`[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null;` +
		`$x = New-Object Windows.Data.Xml.Dom.XmlDocument; $x.LoadXml('` + strings.ReplaceAll(xml, "'", "''") + `');` +
		`$t = [Windows.UI.Notifications.ToastNotification]::new($x);` +
		`[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe').Show($t)`
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := oculto(exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps)).Run(); err != nil {
		log.Println("notificação do Windows falhou:", err)
		// sem notificação: mostra uma janela de aviso
		go caixa(titulo+"\n\n"+corpo, "Faturamento Solar - Avisos", mbOK|mbIconInfo|0x40000) // MB_TOPMOST
		time.Sleep(200 * time.Millisecond)
		return err
	}
	return nil
}

// Tarefa agendada do Windows: roda "FaturamentoSolar.exe --avisos" todo dia no horário escolhido
// (se o computador estiver desligado no horário, roda assim que ligar).
func agendarTarefa(horario string) error {
	exe := caminhoExe
	if exe == "" {
		return nil
	}
	if len(horario) != 5 || horario[2] != ':' {
		horario = "08:00"
	}
	xml := `<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo><Description>Avisos de vencimento do Faturamento Solar</Description></RegistrationInfo>
  <Triggers>
    <CalendarTrigger><StartBoundary>2026-01-01T` + horario + `:00</StartBoundary><Enabled>true</Enabled><ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay></CalendarTrigger>
  </Triggers>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <RunOnlyIfNetworkAvailable>false</RunOnlyIfNetworkAvailable>
    <ExecutionTimeLimit>PT10M</ExecutionTimeLimit>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author"><Exec><Command>` + escXML(exe) + `</Command><Arguments>--avisos</Arguments></Exec></Actions>
</Task>`
	// o schtasks lê o XML em UTF-16
	u := utf16.Encode([]rune(xml))
	buf := make([]byte, 2+2*len(u))
	buf[0], buf[1] = 0xFF, 0xFE
	for i, c := range u {
		buf[2+2*i] = byte(c)
		buf[3+2*i] = byte(c >> 8)
	}
	arq := filepath.Join(os.TempDir(), "faturamento-solar-avisos.xml")
	if err := os.WriteFile(arq, buf, 0o644); err != nil {
		return err
	}
	defer os.Remove(arq)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := oculto(exec.CommandContext(ctx, "schtasks", "/Create", "/F", "/TN", "Faturamento Solar Avisos", "/XML", arq)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
