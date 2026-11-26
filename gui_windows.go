//go:build windows

package main

import (
	"os"
	"unsafe"

	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"

	"github.com/maxb1906/cadastre/internal/cli"
	"github.com/maxb1906/cadastre/internal/console"
	"github.com/maxb1906/cadastre/internal/i18n"
	"github.com/maxb1906/cadastre/internal/sysinfo"
)

// Official WebView2 runtime download page (no bootstrapper embedded, to stay
// portable).
const webview2URL = "https://developer.microsoft.com/microsoft-edge/webview2/#download-section"

const (
	mbYesNoCancel = 0x00000003
	mbOK          = 0x00000000
	mbIconWarning = 0x00000030
	mbIconError   = 0x00000010
	idYes         = 6
	idNo          = 7
)

var procMessageBoxW = windows.NewLazySystemDLL("user32.dll").NewProc("MessageBoxW")

func messageBox(title, text string, flags uintptr) int {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
	return int(r)
}

func fatal(title, msg string) { messageBox(title, msg, mbOK|mbIconError) }

// preflight checks that a GUI can run. On Server Core it prints the CLI
// help; without WebView2 it offers to install the runtime or to fall back to
// the CLI with an HTML report opened in the default browser.
func preflight() bool {
	lang := i18n.Detect()
	if sysinfo.IsServerCore() {
		console.Attach()
		os.Stdout.WriteString(i18n.T(lang, "gui.servercore") + "\n\n")
		cli.Run([]string{"help"}, os.Stdout, os.Stderr)
		return false
	}
	if v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString(""); err == nil && v != "" {
		return true
	}
	switch messageBox("Cadastre", i18n.T(lang, "gui.nowebview2"), mbYesNoCancel|mbIconWarning) {
	case idYes:
		openPath(webview2URL)
	case idNo:
		fallbackReport(lang)
	}
	return false
}

// fallbackReport scans the system drive and opens the HTML report in the
// default browser, without any installation.
func fallbackReport(lang string) {
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = "C:"
	}
	out := os.TempDir() + `\cadastre-` + drive[:1] + `.html`
	messageBox("Cadastre", i18n.T(lang, "gui.fallback", drive, out), mbOK)
	code := cli.Run([]string{"scan", drive, "--quiet", "--report", out, "--open", "--lang", lang}, os.Stdout, os.Stderr)
	if code != 0 {
		fatal("Cadastre", i18n.T(lang, "gui.fallback_failed"))
	}
}
