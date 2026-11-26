// Package reclaim explains the space used on a volume: it reconciles the
// used space reported by the system with what the scan saw, and breaks the
// gap down (NTFS metafiles, shadow copies, alternate streams, protected
// folders, unattributed remainder).
package reclaim

import (
	"sort"
	"strings"

	"github.com/maxb1906/cadastre/internal/engine"
	"github.com/maxb1906/cadastre/internal/index"
)

// Component keys.
const (
	Files        = "files"        // main streams of regular files
	ADS          = "ads"          // alternate data streams
	DirIndex     = "dirindex"     // directory indexes ($I30)
	Meta         = "meta"         // NTFS metafiles ($MFT, $LogFile, $UsnJrnl...)
	VSS          = "vss"          // shadow copies diff area
	SysVol       = "sysvol"       // rest of System Volume Information
	Unattributed = "unattributed" // used space nobody accounts for
)

// vssGUID is the suffix of the VSS diff area files in System Volume
// Information.
const vssGUID = "{3808876b-c176-4e48-b7ae-04046e6cc752}"

// Detail is a named part of a component.
type Detail struct {
	Name  string `json:"name"`
	Bytes uint64 `json:"bytes"`
}

// Component is one share of the used space.
type Component struct {
	Key     string   `json:"key"`
	Bytes   uint64   `json:"bytes"`
	Count   uint64   `json:"count"`
	Details []Detail `json:"details,omitempty"`
	// Source: "scan" (seen in the index), "wmi" (reported by the system),
	// "computed" (difference).
	Source string `json:"source"`
}

// VSSInfo is the shadow storage reported by the system, if known.
type VSSInfo struct {
	Known     bool   `json:"known"`
	Used      uint64 `json:"used"`
	Allocated uint64 `json:"allocated"`
	Max       uint64 `json:"max"`
	Copies    int    `json:"copies"`
}

// Reconciliation is the breakdown of the used space of a volume.
type Reconciliation struct {
	Total      uint64      `json:"total"`
	Free       uint64      `json:"free"`
	Used       uint64      `json:"used"`
	Seen       uint64      `json:"seen"`    // everything the scan accounted for
	Missing    int64       `json:"missing"` // Used - visible regular files (what WizTree cannot explain)
	Components []Component `json:"components"`
	// Unattributed may be negative when the scan saw more than the system
	// reports (files created during the scan, compression estimates).
	Unattributed int64    `json:"unattributed"`
	Engine       string   `json:"engine"`
	VSS          VSSInfo  `json:"vss"`
	Denied       uint64   `json:"denied"` // folders a walk could not read
	Anomaly      bool     `json:"anomaly"`
	Notes        []string `json:"notes"` // i18n keys
}

// Get returns the component with key k.
func (r *Reconciliation) Get(k string) Component {
	for _, c := range r.Components {
		if c.Key == k {
			return c
		}
	}
	return Component{Key: k}
}

// Compute reconciles a scan result with the volume usage. vss is the shadow
// storage reported by WMI (Known=false if unavailable).
func Compute(res *engine.Result, vss VSSInfo) *Reconciliation {
	ix := res.Index
	r := &Reconciliation{Total: res.Volume.Total, Free: res.Volume.Free, Engine: res.Engine, VSS: vss}
	if r.Free <= r.Total {
		r.Used = r.Total - r.Free
	}
	files := Component{Key: Files, Source: "scan"}
	ads := Component{Key: ADS, Source: "scan"}
	dirIdx := Component{Key: DirIndex, Source: "scan"}
	meta := Component{Key: Meta, Source: "scan"}
	shadow := Component{Key: VSS, Source: "scan"}
	sysvol := Component{Key: SysVol, Source: "scan"}
	metaDetails := map[string]uint64{}

	svi, hasSVI := ix.Child(ix.Root, "System Volume Information")
	inSVI := func(i uint32) bool { return hasSVI && ix.IsAncestor(svi, i) }

	for i := range ix.Parent {
		u := uint32(i)
		if !ix.Valid(u) {
			continue
		}
		a := ix.Attr[i]
		if a&index.FlagDir != 0 {
			if own := ix.DirOwnAlloc[u]; own > 0 {
				if a&index.FlagMeta != 0 {
					meta.Bytes += own
					metaDetails[metaName(ix, u)] += own
				} else {
					dirIdx.Bytes += own
					dirIdx.Count++
				}
			}
			if a&index.FlagDenied != 0 {
				r.Denied++
			}
			continue
		}
		if a&index.FlagMeta != 0 {
			meta.Bytes += ix.Alloc[i]
			meta.Count++
			metaDetails[metaName(ix, u)] += ix.Alloc[i]
			for _, s := range ix.ADS[u] {
				meta.Bytes += s.Alloc
				metaDetails[metaName(ix, u)+":"+s.Name] += s.Alloc
			}
			continue
		}
		_, adsAlloc := ix.ADSTotals(u)
		if inSVI(u) {
			if strings.Contains(strings.ToLower(ix.Name(u)), vssGUID) {
				shadow.Bytes += ix.Alloc[i] + adsAlloc
				shadow.Count++
			} else {
				sysvol.Bytes += ix.Alloc[i] + adsAlloc
				sysvol.Count++
			}
			continue
		}
		files.Bytes += ix.Alloc[i]
		files.Count++
		if adsAlloc > 0 {
			ads.Bytes += adsAlloc
			ads.Count++
		}
	}
	for name, b := range metaDetails {
		if b > 0 {
			meta.Details = append(meta.Details, Detail{Name: name, Bytes: b})
		}
	}
	sort.Slice(meta.Details, func(a, b int) bool { return meta.Details[a].Bytes > meta.Details[b].Bytes })

	// A walk cannot see the diff area: take the figure WMI reports.
	if shadow.Count == 0 && vss.Known && vss.Allocated > 0 {
		shadow = Component{Key: VSS, Source: "wmi", Bytes: vss.Allocated, Count: uint64(vss.Copies)}
	}
	r.Components = []Component{files, ads, dirIdx, meta, shadow, sysvol}
	for _, c := range r.Components {
		r.Seen += c.Bytes
	}
	r.Missing = int64(r.Used) - int64(files.Bytes)
	r.Unattributed = int64(r.Used) - int64(r.Seen)
	un := Component{Key: Unattributed, Source: "computed"}
	if r.Unattributed > 0 {
		un.Bytes = uint64(r.Unattributed)
	}
	r.Components = append(r.Components, un)

	if res.Engine == engine.EngineWalk {
		r.Notes = append(r.Notes, "reclaim.note.walk")
		if r.Denied > 0 {
			r.Notes = append(r.Notes, "reclaim.note.denied")
		}
	}
	if r.Used == 0 {
		r.Notes = append(r.Notes, "reclaim.note.nousage")
	}
	// Above 1 GiB and 2 % of the used space, an unexplained remainder is
	// abnormal on an MFT scan: files deleted but still open, corruption,
	// quotas...
	if res.Engine == engine.EngineMFT && r.Unattributed > 1<<30 && uint64(r.Unattributed) > r.Used/50 {
		r.Anomaly = true
		r.Notes = append(r.Notes, "reclaim.note.anomaly")
	}
	if vss.Known && shadow.Source == "scan" && vss.Allocated > 0 {
		r.Notes = append(r.Notes, "reclaim.note.vss_wmi")
	}
	return r
}

func metaName(ix *index.Index, i uint32) string {
	p := ix.Path(i)
	return strings.TrimPrefix(p[len(ix.RootPath):], `\`)
}
