package cli

import (
	"os/exec"
	"path/filepath"
	"runtime"
)

// openInBrowser opens a local file with the default application.
func openInBrowser(path string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", abs).Start()
	case "darwin":
		exec.Command("open", abs).Start()
	default:
		exec.Command("xdg-open", abs).Start()
	}
}
