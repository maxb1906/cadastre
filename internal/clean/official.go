package clean

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/maxb1906/cadastre/internal/sysinfo"
)

// expandPlaceholders replaces {path} {path_ps} {drive} {logname} {backup}
// in an official command argument. Commands are run without a shell: only
// {path_ps}, used inside a PowerShell -Command string, needs quoting.
func expandPlaceholders(arg, item, root string) string {
	if !strings.Contains(arg, "{") {
		return arg
	}
	drive := ""
	if len(root) >= 2 && root[1] == ':' {
		drive = root[:2]
	}
	logname := ""
	backup := ""
	if item != "" {
		base := filepath.Base(strings.ReplaceAll(item, `\`, "/"))
		logname = strings.ReplaceAll(strings.TrimSuffix(base, filepath.Ext(base)), "%4", "/")
		safe := regexp.MustCompile(`[^A-Za-z0-9._-]+`).ReplaceAllString(logname, "_")
		backup = filepath.Join(sysinfo.ProgramData(), "Cadastre", "backup", safe+"-"+time.Now().Format("20060102-150405")+".evtx")
	}
	r := strings.NewReplacer(
		"{path_ps}", PSQuote(item),
		"{path}", item,
		"{drive}", drive,
		"{logname}", logname,
		"{backup}", backup,
	)
	return r.Replace(arg)
}

// PSQuote quotes a string for PowerShell (single quotes, doubled inside).
func PSQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// RunCommand runs an official command, hidden, and returns its combined
// output. Long operations (DISM) get up to two hours.
func RunCommand(ctx context.Context, args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	for _, a := range args {
		if strings.Contains(a, "{backup}") || strings.Contains(a, `\Cadastre\backup\`) {
			_ = mkdirFor(a)
		}
	}
	cmd := sysinfo.HiddenCommand(args[0], args[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return "", err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return decodeOEM(out.Bytes()), err
	case <-ctx.Done():
		cmd.Process.Kill()
		<-done
		return decodeOEM(out.Bytes()), ctx.Err()
	}
}

func mkdirFor(path string) error {
	return osMkdirAll(filepath.Dir(path))
}

// CreateRestorePoint creates a system restore point (client Windows with
// system protection enabled).
func CreateRestorePoint(ctx context.Context) (string, error) {
	return RunCommand(ctx, []string{"powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"Checkpoint-Computer -Description 'Cadastre' -RestorePointType MODIFY_SETTINGS"})
}

var _ = exec.Command
