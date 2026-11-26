package rules

import (
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/maxb1906/cadastre/internal/index"
)

// segment kinds
const (
	segLiteral = iota
	segGlob
	segAny // **
)

type segment struct {
	kind int
	text string // lowercased
}

// pattern is a compiled path pattern. It is matched with a small NFA whose
// states are positions in the segment list, so that "**" works at any place
// without backtracking blowups.
type pattern struct {
	src  string
	segs []segment
}

const maxSegments = 63

func compilePattern(p string) (pattern, error) {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '\\' || r == '/' })
	if len(parts) == 0 {
		return pattern{}, errors.New("empty pattern")
	}
	if len(parts) > maxSegments {
		return pattern{}, errors.New("pattern too long")
	}
	pt := pattern{src: p}
	for _, s := range parts {
		switch {
		case s == "**":
			pt.segs = append(pt.segs, segment{kind: segAny})
		case s == "." || s == "..":
			return pattern{}, errors.New("relative segments not allowed")
		case strings.ContainsAny(s, "*?"):
			if strings.Contains(s, "**") {
				return pattern{}, errors.New(`"**" must be a whole segment`)
			}
			pt.segs = append(pt.segs, segment{kind: segGlob, text: strings.ToLower(s)})
		default:
			pt.segs = append(pt.segs, segment{kind: segLiteral, text: strings.ToLower(s)})
		}
	}
	return pt, nil
}

// states is a bit set of NFA positions; bit len(segs) is the final state.
type states uint64

func (pt *pattern) final() states { return 1 << uint(len(pt.segs)) }

// closure adds the epsilon transitions: a "**" may match zero segments.
func (pt *pattern) closure(s states) states {
	for k := 0; k < len(pt.segs); k++ {
		if s&(1<<uint(k)) != 0 && pt.segs[k].kind == segAny {
			s |= 1 << uint(k+1)
		}
	}
	return s
}

// step consumes one path segment (lowercase name).
func (pt *pattern) step(s states, name []byte) states {
	var next states
	for k := 0; k < len(pt.segs); k++ {
		if s&(1<<uint(k)) == 0 {
			continue
		}
		seg := pt.segs[k]
		switch seg.kind {
		case segAny:
			next |= 1 << uint(k)
		case segLiteral:
			if equalFoldBytes(name, seg.text) {
				next |= 1 << uint(k+1)
			}
		case segGlob:
			if globMatch(seg.text, name) {
				next |= 1 << uint(k+1)
			}
		}
	}
	return pt.closure(next)
}

// start returns the NFA states after consuming the segments of prefix (the
// position of the scanned root inside its volume, for folder scans).
func (pt *pattern) start(prefix []string) states {
	s := pt.closure(1)
	for _, p := range prefix {
		s = pt.step(s, []byte(p))
		if s == 0 {
			return 0
		}
	}
	return s
}

// match calls emit for every entry of ix matching the pattern. A matched
// directory is not descended into (its subtree belongs to the match).
// Reparse points, metafiles and virtual nodes are never descended into.
func (pt *pattern) match(ix *index.Index, prefix []string, emit func(i uint32)) {
	s0 := pt.start(prefix)
	if s0 == 0 {
		return
	}
	if s0&pt.final() != 0 {
		emit(ix.Root)
		return
	}
	type frame struct {
		dir uint32
		st  states
	}
	stack := []frame{{ix.Root, s0}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, k := range ix.Kids(f.dir) {
			if !ix.Valid(k) || ix.NameLen[k] == 0 {
				continue
			}
			ns := pt.step(f.st, ix.NameBytes(k))
			if ns == 0 {
				continue
			}
			if ns&pt.final() != 0 {
				emit(k)
				continue
			}
			if ix.IsDir(k) && ix.Attr[k]&(index.AttrReparsePoint|index.FlagMeta|index.FlagVirtual) == 0 {
				stack = append(stack, frame{k, ns})
			}
		}
	}
}

// globMatch matches a lowercase pattern with * and ? against a name,
// case-insensitively.
func globMatch(pat string, name []byte) bool {
	// Iterative wildcard matching with single backtrack point.
	var lower [256]byte
	n := name
	if len(name) <= len(lower) {
		ascii := true
		for k, c := range name {
			if c >= 0x80 {
				ascii = false
				break
			}
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			lower[k] = c
		}
		if ascii {
			n = lower[:len(name)]
		} else {
			n = []byte(strings.ToLower(string(name)))
		}
	} else {
		n = []byte(strings.ToLower(string(name)))
	}
	p, s := 0, 0
	star, mark := -1, 0
	for s < len(n) {
		switch {
		case p < len(pat) && pat[p] == '?':
			_, size := utf8.DecodeRune(n[s:])
			p++
			s += size
		case p < len(pat) && pat[p] == '*':
			star, mark = p, s
			p++
		case p < len(pat) && pat[p] == n[s]:
			p++
			s++
		case star >= 0:
			p = star + 1
			mark++
			s = mark
		default:
			return false
		}
	}
	for p < len(pat) && pat[p] == '*' {
		p++
	}
	return p == len(pat)
}

// matchesAny reports whether name matches one of the lowercase globs.
func matchesAny(globs []string, name []byte) bool {
	for _, g := range globs {
		if globMatch(g, name) {
			return true
		}
	}
	return false
}

func equalFoldBytes(b []byte, lower string) bool {
	if len(b) != len(lower) {
		return strings.EqualFold(string(b), lower)
	}
	for k := 0; k < len(b); k++ {
		c := b[k]
		if c >= 0x80 {
			return strings.EqualFold(string(b), lower)
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lower[k] {
			return false
		}
	}
	return true
}
