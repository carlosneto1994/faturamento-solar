//go:build windows

// Instalador do Faturamento Solar: copia o programa, cria atalhos e registra a desinstalação.
package main

import (
	_ "embed"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

//go:embed FaturamentoSolar.exe
var programa []byte

//go:embed icone.ico
var icone []byte

const versao = "1.0.4"

var (
	user32     = syscall.NewLazyDLL("user32.dll")
	messageBox = user32.NewProc("MessageBoxW")
)

func caixa(texto string, tipo uintptr) int {
	t, _ := syscall.UTF16PtrFromString(texto)
	c, _ := syscall.UTF16PtrFromString("Instalador - Faturamento Solar")
	r, _, _ := messageBox.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), tipo)
	return int(r)
}

func oculto(c *exec.Cmd) *exec.Cmd {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}

func aspas(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func main() {
	if caixa("Instalar o Faturamento Solar neste computador?\n\nSerá criado um atalho na área de trabalho e no menu Iniciar.", 0x4|0x20) != 6 {
		return
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		h, _ := os.UserHomeDir()
		base = filepath.Join(h, "AppData", "Local")
	}
	dir := filepath.Join(base, "Programs", "Faturamento Solar")
	exe := filepath.Join(dir, "FaturamentoSolar.exe")
	ico := filepath.Join(dir, "icone.ico")

	// Se o programa estiver aberto, fecha antes de atualizar
	c := http.Client{Timeout: 2 * time.Second}
	if r, err := c.Post("http://127.0.0.1:47831/api/sair", "text/plain", nil); err == nil {
		r.Body.Close()
		time.Sleep(1500 * time.Millisecond)
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		caixa("Não consegui criar a pasta de instalação:\n"+err.Error(), 0x10)
		return
	}
	var err error
	for i := 0; i < 5; i++ { // o arquivo pode estar preso por alguns segundos
		if err = os.WriteFile(exe, programa, 0o755); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		caixa("Não consegui copiar o programa (feche o Faturamento Solar e tente de novo):\n"+err.Error(), 0x10)
		return
	}
	_ = os.WriteFile(ico, icone, 0o644)

	// Atalhos na área de trabalho e no menu Iniciar
	ps := `$w=New-Object -ComObject WScript.Shell; foreach($d in @([Environment]::GetFolderPath('Desktop'),[Environment]::GetFolderPath('Programs'))){ ` +
		`$s=$w.CreateShortcut((Join-Path $d 'Faturamento Solar.lnk')); $s.TargetPath=` + aspas(exe) + `; $s.WorkingDirectory=` + aspas(dir) +
		`; $s.IconLocation=` + aspas(ico+",0") + `; $s.Description='Faturamento de energia solar'; $s.Save() }`
	atalhoOk := oculto(exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", ps)).Run() == nil
	if !atalhoOk { // alternativa sem PowerShell
		h, _ := os.UserHomeDir()
		url := "[InternetShortcut]\r\nURL=file:///" + filepath.ToSlash(exe) + "\r\nIconFile=" + ico + "\r\nIconIndex=0\r\n"
		atalhoOk = os.WriteFile(filepath.Join(h, "Desktop", "Faturamento Solar.url"), []byte(url), 0o644) == nil
	}

	// Aparece em "Aplicativos instalados" para poder desinstalar
	chave := `HKCU\Software\Microsoft\Windows\CurrentVersion\Uninstall\FaturamentoSolar`
	reg := func(nome, tipo, valor string) {
		_ = oculto(exec.Command("reg", "add", chave, "/v", nome, "/t", tipo, "/d", valor, "/f")).Run()
	}
	reg("DisplayName", "REG_SZ", "Faturamento Solar")
	reg("DisplayVersion", "REG_SZ", versao)
	reg("Publisher", "REG_SZ", "Faturamento Solar")
	reg("DisplayIcon", "REG_SZ", ico)
	reg("InstallLocation", "REG_SZ", dir)
	reg("UninstallString", "REG_SZ", `"`+exe+`" --desinstalar`)
	reg("NoModify", "REG_DWORD", "1")
	reg("NoRepair", "REG_DWORD", "1")

	msg := "Faturamento Solar instalado!\n\n"
	if atalhoOk {
		msg += "O atalho \"Faturamento Solar\" está na sua área de trabalho.\n"
	} else {
		msg += "Não consegui criar o atalho. O programa está em:\n" + exe + "\n"
	}
	msg += "Seus dados e faturas ficam em Documentos\\Faturamento Solar.\n\nAbrir o programa agora?"
	if caixa(msg, 0x4|0x40) == 6 {
		cmd := exec.Command(exe)
		cmd.Dir = dir
		_ = cmd.Start()
	}
}
