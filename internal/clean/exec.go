package clean

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Prepare checks every item of the plan against the disk (the mandatory
// dry-run): items that changed since the scan move to Skipped, and the file
// number of each remaining item is captured when the scan did not record
// it (walk engine, snapshot). This also is the rescan of the items required
// before cleaning from a reopened snapshot.
func Prepare(p *Plan, fs FS) {
	check := func(items []Item) []Item {
		kept := items[:0]
		for _, it := range items {
			info, err := fs.Stat(it.Path)
			if err == nil {
				err = Check(info, expectation(it))
			}
			if err != nil {
				p.Skipped = append(p.Skipped, Skip{Path: it.Path, Rule: it.Rule, Reason: reasonOf(err), Detail: detail(err), Alloc: it.Alloc})
				continue
			}
			if info.Cloud {
				p.Skipped = append(p.Skipped, Skip{Path: it.Path, Rule: it.Rule, Reason: ReasonCloud, Alloc: it.Alloc})
				continue
			}
			if it.FileID == 0 {
				it.FileID = info.FileID
			}
			kept = append(kept, it)
		}
		return kept
	}
	p.Items = check(p.Items)
	for k := range p.Official {
		p.Official[k].Items = check(p.Official[k].Items)
		p.Official[k].settle()
	}
	p.Files, p.Bytes = 0, 0
	for _, it := range p.Items {
		if !it.Dir {
			p.Files++
			p.Bytes += it.Alloc
		}
	}
	for _, o := range p.Official {
		p.Bytes += o.Alloc
	}
	p.Prepared = true
}

func expectation(it Item) Expectation {
	return Expectation{Size: it.Size, MTime: it.MTime, FileID: it.FileID, Dir: it.Dir, Reparse: it.Reparse}
}

func detail(err error) string {
	var known = []error{ErrGone, ErrChanged, ErrIDChanged, ErrTypeChanged, ErrLocked, ErrDenied, ErrNotEmpty}
	for _, k := range known {
		if errors.Is(err, k) {
			return ""
		}
	}
	if err == nil {
		return ""
	}
	return err.Error()
}

// Options of an execution.
type Options struct {
	// ConfirmOrange is the explicit confirmation required by orange items;
	// without it they are skipped.
	ConfirmOrange bool
	// RestorePoint creates a restore point before official system actions.
	RestorePoint bool
	// Running is checked again right before each rule's items.
	Running func() map[string]bool
	// Progress is called after each item.
	Progress func(Progress)
	// Runner runs official commands (default: RunCommand).
	Runner func(ctx context.Context, args []string) (string, error)
	// RestorePointer creates the restore point (default: CreateRestorePoint).
	RestorePointer func(ctx context.Context) (string, error)
}

// Progress of an execution.
type Progress struct {
	Done  int    `json:"done"`
	Total int    `json:"total"`
	Freed uint64 `json:"freed"`
	Path  string `json:"path"`
}

// Result of an execution.
type Result struct {
	Done     int      `json:"done"`
	Failed   int      `json:"failed"`
	Skipped  int      `json:"skipped"`
	Freed    uint64   `json:"freed"`
	Canceled bool     `json:"canceled"`
	Journal  string   `json:"journal"`
	Removed  []uint32 `json:"-"` // index entries deleted
	Rescan   []string `json:"-"` // folders touched by official actions
	Entries  []Entry  `json:"entries"`
}

// ErrNotPrepared is returned when executing a plan without its dry-run.
var ErrNotPrepared = errors.New("clean: the plan must be simulated (prepared) before execution")

const recycleBatch = 64

// Execute runs a prepared plan. Each file is revalidated right before its
// deletion; the execution can be canceled between two items.
func Execute(ctx context.Context, p *Plan, fs FS, j *Journal, opt Options) (*Result, error) {
	if !p.Prepared {
		return nil, ErrNotPrepared
	}
	if opt.Runner == nil {
		opt.Runner = RunCommand
	}
	if opt.RestorePointer == nil {
		opt.RestorePointer = CreateRestorePoint
	}
	r := &Result{}
	if j != nil {
		r.Journal = j.Path()
	}
	log := func(e Entry) {
		e.Session = p.ID
		if e.Time.IsZero() {
			e.Time = time.Now()
		}
		if j != nil {
			j.Write(e)
		}
		if len(r.Entries) < 5000 {
			r.Entries = append(r.Entries, e)
		}
	}
	log(Entry{Action: "plan", Path: p.Root, Mode: p.Mode, Result: "ok",
		Detail: strings.Join([]string{"items=" + itoa(len(p.Items)), "official=" + itoa(len(p.Official)), "engine=" + p.Engine}, " ")})

	total := len(p.Items)
	for _, o := range p.Official {
		total += len(o.Items) + len(o.Commands)
	}
	done := 0
	progress := func(path string) {
		done++
		if opt.Progress != nil {
			opt.Progress(Progress{Done: done, Total: total, Freed: r.Freed, Path: path})
		}
	}

	items := p.Items
	if !opt.ConfirmOrange {
		kept := items[:0:0]
		for _, it := range items {
			if it.Risk == "orange" {
				log(Entry{Action: "skip", Path: it.Path, Rule: it.Rule, Size: it.Alloc, Result: "skipped", Reason: "orange_unconfirmed"})
				r.Skipped++
				continue
			}
			kept = append(kept, it)
		}
		items = kept
	}
	if err := deleteItems(ctx, items, p.Mode, fs, opt, r, log, progress); err != nil {
		return r, err
	}

	// Official actions.
	restoreDone := false
	for _, o := range p.Official {
		if ctx.Err() != nil {
			r.Canceled = true
			break
		}
		if o.Risk == "orange" && !opt.ConfirmOrange {
			log(Entry{Action: "skip", Path: strings.Join(o.Targets, "; "), Rule: o.Rule, Result: "skipped", Reason: "orange_unconfirmed"})
			r.Skipped++
			continue
		}
		if o.RestorePoint && opt.RestorePoint && !restoreDone {
			out, err := opt.RestorePointer(ctx)
			restoreDone = true
			log(Entry{Action: "restore_point", Result: resultOf(err), Detail: trim(out, err)})
		}
		before := r.Freed
		for _, cmd := range o.Commands {
			if ctx.Err() != nil {
				r.Canceled = true
				break
			}
			if cmd == nil {
				if err := deleteItems(ctx, o.Items, ModePermanent, fs, opt, r, log, progress); err != nil {
					return r, err
				}
				continue
			}
			out, err := opt.Runner(ctx, cmd)
			log(Entry{Action: "command", Rule: o.Rule, Path: strings.Join(o.Targets, "; "), Mode: "official",
				Result: resultOf(err), Detail: strings.Join(cmd, " ") + "\n" + trim(out, err)})
			if err != nil {
				r.Failed++
			} else {
				r.Done++
			}
			progress(strings.Join(cmd, " "))
		}
		_ = before
		r.Rescan = append(r.Rescan, o.Targets...)
	}
	log(Entry{Action: "end", Path: p.Root, Result: "ok", Size: r.Freed,
		Detail: "done=" + itoa(r.Done) + " failed=" + itoa(r.Failed) + " skipped=" + itoa(r.Skipped)})
	return r, nil
}

func deleteItems(ctx context.Context, items []Item, mode string, fs FS, opt Options, r *Result, log func(Entry), progress func(string)) error {
	var batch []Item
	flush := func() {
		if len(batch) == 0 {
			return
		}
		paths := make([]string, len(batch))
		for k, it := range batch {
			paths[k] = it.Path
		}
		errs := fs.Recycle(paths)
		for _, it := range batch {
			if err := errs[it.Path]; err != nil {
				log(Entry{Action: "recycle", Path: it.Path, Rule: it.Rule, Size: it.Alloc, Mode: ModeRecycle, Result: "failed", Reason: reasonOf(err), Detail: detail(err)})
				r.Failed++
			} else {
				log(Entry{Action: "recycle", Path: it.Path, Rule: it.Rule, Size: it.Alloc, Mode: ModeRecycle, Result: "ok"})
				r.Done++
				r.Freed += it.Alloc
				r.Removed = append(r.Removed, it.Entry)
			}
			progress(it.Path)
		}
		batch = batch[:0]
	}
	var running map[string]bool
	if opt.Running != nil {
		running = opt.Running()
	}
	for _, it := range items {
		if ctx.Err() != nil {
			flush()
			r.Canceled = true
			log(Entry{Action: "skip", Path: it.Path, Rule: it.Rule, Result: "skipped", Reason: ReasonCanceled})
			return nil
		}
		// Defense in depth: the blacklist is checked again at execution.
		if blockedPath(it.Path) {
			log(Entry{Action: "skip", Path: it.Path, Rule: it.Rule, Result: "skipped", Reason: ReasonProtected})
			r.Skipped++
			progress(it.Path)
			continue
		}
		if name := firstRunning(it.CloseFirst, running); name != "" {
			log(Entry{Action: "skip", Path: it.Path, Rule: it.Rule, Result: "skipped", Reason: ReasonProcess, Detail: name})
			r.Skipped++
			progress(it.Path)
			continue
		}
		exp := expectation(it)
		// Folders (after their files) are removed only if empty, and
		// always permanently: an empty folder is nothing to restore.
		if it.Dir || mode == ModePermanent {
			flush()
			err := fs.DeleteVerified(it.Path, exp)
			action := "delete"
			if it.Dir && !it.Reparse {
				action = "rmdir"
			}
			if err != nil {
				res := "failed"
				if errors.Is(err, ErrNotEmpty) || errors.Is(err, ErrChanged) || errors.Is(err, ErrIDChanged) || errors.Is(err, ErrGone) || errors.Is(err, ErrTypeChanged) {
					res = "skipped"
					r.Skipped++
				} else {
					r.Failed++
				}
				log(Entry{Action: action, Path: it.Path, Rule: it.Rule, Size: it.Alloc, Mode: ModePermanent, Result: res, Reason: reasonOf(err), Detail: detail(err)})
			} else {
				log(Entry{Action: action, Path: it.Path, Rule: it.Rule, Size: it.Alloc, Mode: ModePermanent, Result: "ok"})
				r.Done++
				r.Freed += it.Alloc
				r.Removed = append(r.Removed, it.Entry)
			}
			progress(it.Path)
			continue
		}
		// Recycle bin: revalidate right before, in small batches.
		info, err := fs.Stat(it.Path)
		if err == nil {
			err = Check(info, exp)
		}
		if err != nil {
			log(Entry{Action: "recycle", Path: it.Path, Rule: it.Rule, Size: it.Alloc, Mode: ModeRecycle, Result: "skipped", Reason: reasonOf(err), Detail: detail(err)})
			r.Skipped++
			progress(it.Path)
			continue
		}
		batch = append(batch, it)
		if len(batch) >= recycleBatch {
			flush()
		}
	}
	flush()
	return nil
}

func firstRunning(names []string, running map[string]bool) string {
	for _, n := range names {
		if running[strings.ToLower(n)] {
			return n
		}
	}
	return ""
}

// blockedPath re-applies the parts of the blacklist that never depend on a
// rule, from the absolute path.
func blockedPath(p string) bool {
	segs := strings.FieldsFunc(p, func(r rune) bool { return r == '\\' || r == '/' })
	if len(p) >= 2 && p[1] == ':' && len(segs) > 0 {
		segs = segs[1:]
	} else {
		return false // not a drive path (tests, shares): checked at planning
	}
	blocked, _ := Blocked(segs, true) // most permissive form: anything still blocked is never touched
	return blocked
}

func resultOf(err error) string {
	if err != nil {
		return "failed"
	}
	return "ok"
}

func trim(out string, err error) string {
	if err != nil {
		out = strings.TrimSpace(out + "\n" + err.Error())
	}
	if len(out) > 16<<10 {
		out = out[:16<<10] + "…"
	}
	return strings.TrimSpace(out)
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
