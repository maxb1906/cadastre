package clean

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maxb1906/cadastre/internal/sysinfo"
)

// Entry is one line of the JSONL journal.
type Entry struct {
	Time    time.Time `json:"ts"`
	Session string    `json:"session"`
	Action  string    `json:"action"` // plan, delete, recycle, rmdir, skip, command, restore_point, end
	Path    string    `json:"path,omitempty"`
	Rule    string    `json:"rule,omitempty"`
	Size    uint64    `json:"size,omitempty"`
	Mode    string    `json:"mode,omitempty"`
	Result  string    `json:"result"` // ok, failed, skipped
	Reason  string    `json:"reason,omitempty"`
	Detail  string    `json:"detail,omitempty"`
}

// Journal appends entries to %ProgramData%\Cadastre\journal\<date>.jsonl,
// synced line by line so that a crash loses nothing.
type Journal struct {
	mu   sync.Mutex
	f    *os.File
	w    *bufio.Writer
	path string
}

// DefaultDir returns the journal directory.
func DefaultDir() string { return filepath.Join(sysinfo.ProgramData(), "Cadastre", "journal") }

// OpenJournal opens (creates) today's journal in dir (DefaultDir if empty),
// falling back to the user's config directory if dir is not writable.
func OpenJournal(dir string) (*Journal, error) {
	if dir == "" {
		dir = DefaultDir()
	}
	name := time.Now().Format("2006-01-02") + ".jsonl"
	try := func(d string) (*Journal, error) {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
		p := filepath.Join(d, name)
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		return &Journal{f: f, w: bufio.NewWriter(f), path: p}, nil
	}
	j, err := try(dir)
	if err == nil {
		return j, nil
	}
	if cfg, e2 := os.UserConfigDir(); e2 == nil {
		if j, e3 := try(filepath.Join(cfg, "Cadastre", "journal")); e3 == nil {
			return j, nil
		}
	}
	return nil, err
}

// Path returns the journal file path.
func (j *Journal) Path() string { return j.path }

// Write appends an entry.
func (j *Journal) Write(e Entry) {
	j.mu.Lock()
	defer j.mu.Unlock()
	b, _ := json.Marshal(e)
	j.w.Write(b)
	j.w.WriteByte('\n')
	j.w.Flush()
	j.f.Sync()
}

// Close closes the journal.
func (j *Journal) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.w.Flush()
	return j.f.Close()
}
