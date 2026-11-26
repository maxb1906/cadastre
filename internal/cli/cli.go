// Package cli implements the command-line interface of Cadastre.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/maxb1906/cadastre/internal/i18n"
)

// Version is set at build time (-ldflags "-X .../cli.Version=1.0.0").
var Version = "dev"

// Env carries the I/O of a CLI invocation.
type Env struct {
	Stdout, Stderr io.Writer
	Lang           string
}

type command struct {
	name    string
	summary string // i18n key
	run     func(ctx context.Context, env *Env, args []string) error
}

func commands() []command {
	return []command{
		{"scan", "cli.cmd.scan", runScan},
		{"check", "cli.cmd.check", runCheck},
		{"version", "cli.cmd.version", runVersion},
		{"help", "cli.cmd.help", nil},
	}
}

// errUsage signals a usage error already reported to the user.
var errUsage = errors.New("usage")

// Run executes the command line and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	env := &Env{Stdout: stdout, Stderr: stderr, Lang: i18n.Detect()}
	// --lang may appear anywhere.
	rest := args[:0:0]
	for k := 0; k < len(args); k++ {
		a := args[k]
		switch {
		case a == "--lang" || a == "-lang":
			if k+1 < len(args) {
				env.Lang = langOr(args[k+1], env.Lang)
				k++
			}
		case strings.HasPrefix(a, "--lang=") || strings.HasPrefix(a, "-lang="):
			env.Lang = langOr(a[strings.IndexByte(a, '=')+1:], env.Lang)
		default:
			rest = append(rest, a)
		}
	}
	args = rest
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" || args[0] == "/?" {
		usage(env)
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	for _, c := range commands() {
		if c.name == args[0] && c.run != nil {
			err := c.run(ctx, env, args[1:])
			switch {
			case err == nil:
				return 0
			case errors.Is(err, errUsage), errors.Is(err, flag.ErrHelp):
				return 2
			case errors.Is(err, context.Canceled):
				fmt.Fprintln(stderr, i18n.T(env.Lang, "cli.canceled"))
				return 130
			default:
				fmt.Fprintf(stderr, "cadastre: %v\n", err)
				return 1
			}
		}
	}
	fmt.Fprintf(stderr, "%s\n\n", i18n.T(env.Lang, "cli.unknown", args[0]))
	usage(env)
	return 2
}

func langOr(l, def string) string {
	if n := i18n.Normalize(l); n != "" {
		return n
	}
	return def
}

func usage(env *Env) {
	p := i18n.Printer{Lang: env.Lang}
	fmt.Fprintf(env.Stdout, "Cadastre %s — %s\n\n", Version, p.T("app.tagline"))
	fmt.Fprintf(env.Stdout, "%s\n  cadastre                 %s\n", p.T("cli.usage"), p.T("cli.gui"))
	for _, c := range commands() {
		fmt.Fprintf(env.Stdout, "  cadastre %-15s %s\n", c.name, p.T(c.summary))
	}
	fmt.Fprintf(env.Stdout, "\n%s\n", p.T("cli.more"))
}

// parseFlags parses args with fs, allowing flags after positional arguments
// ("scan C: --top 20"), and returns the positional arguments.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var flags, pos []string
	for k := 0; k < len(args); k++ {
		a := args[k]
		if a == "--" {
			pos = append(pos, args[k+1:]...)
			break
		}
		if len(a) > 1 && a[0] == '-' && !isNegativeNumber(a) {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if strings.Contains(name, "=") {
				continue
			}
			if f := fs.Lookup(name); f != nil {
				if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
					continue
				}
				if k+1 < len(args) {
					flags = append(flags, args[k+1])
					k++
				}
			}
			continue
		}
		pos = append(pos, a)
	}
	if err := fs.Parse(flags); err != nil {
		return nil, err
	}
	return pos, nil
}

func isNegativeNumber(s string) bool {
	if len(s) < 2 || s[0] != '-' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func newFlagSet(env *Env, name, usageKey string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() {
		fmt.Fprintf(env.Stderr, "%s\n\n", i18n.T(env.Lang, usageKey))
		fs.PrintDefaults()
	}
	return fs
}

func runVersion(_ context.Context, env *Env, _ []string) error {
	fmt.Fprintf(env.Stdout, "cadastre %s\n", Version)
	return nil
}
