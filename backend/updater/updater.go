package updater

import (
	"context"
	"fmt"
	"sync"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

// DefaultInterval is how often Schedule runs Check after the initial startup
// check. Tunable via Config.Interval.
const DefaultInterval = 24 * time.Hour

// Info is the result of a single Check call. Available + LatestVersion +
// AssetURL + AssetFilename are zero-valued when no upgrade is offered.
type Info struct {
	Available     bool      `json:"available"`
	LatestVersion string    `json:"latest_version"`
	AssetURL      string    `json:"asset_url"`
	AssetFilename string    `json:"asset_filename"`
	CheckedAt     time.Time `json:"checked_at"`
}

// Config is the constructor input.
type Config struct {
	CurrentVersion string
	Source         Source
	Interval       time.Duration // 0 → DefaultInterval
	OnAvailable    func(Info)    // optional; fired from Check when Info.Available
}

// Updater performs version checks and binary swaps. Safe for concurrent use.
type Updater struct {
	cfg  Config
	mu   sync.Mutex
	last Info
}

func New(c Config) *Updater {
	if c.Interval == 0 {
		c.Interval = DefaultInterval
	}
	return &Updater{cfg: c}
}

// Last returns a snapshot of the most recent Check result. Zero-valued before
// the first Check.
func (u *Updater) Last() Info {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.last
}

// Check polls the configured Source. Dev/empty CurrentVersion always returns a
// non-available Info with a nil error so the caller can still record CheckedAt.
func (u *Updater) Check(ctx context.Context) (Info, error) {
	info := Info{CheckedAt: time.Now()}
	if u.cfg.CurrentVersion == "dev" || u.cfg.CurrentVersion == "" {
		u.set(info)
		return info, nil
	}
	tag, asset, assetName, err := u.cfg.Source.DetectLatest(ctx)
	if err != nil {
		return Info{}, err
	}
	if tag == "" {
		u.set(info)
		return info, nil
	}
	if compareVersions(tag, u.cfg.CurrentVersion) > 0 {
		info.Available = true
		info.LatestVersion = tag
		info.AssetURL = asset
		info.AssetFilename = assetName
		if u.cfg.OnAvailable != nil {
			u.cfg.OnAvailable(info)
		}
	}
	u.set(info)
	return info, nil
}

func (u *Updater) set(i Info) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.last = i
}

// Schedule runs an initial Check synchronously (errors swallowed) and then
// re-checks every Interval until ctx is cancelled.
func (u *Updater) Schedule(ctx context.Context) {
	_, _ = u.Check(ctx)
	t := time.NewTicker(u.cfg.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = u.Check(ctx)
		}
	}
}

// Install downloads and applies the asset at info.AssetURL, replacing the
// running binary. Caller arranges the relaunch prompt. The AssetFilename's
// extension drives go-selfupdate's archive decompressor selection.
func (u *Updater) Install(ctx context.Context, info Info) error {
	if !info.Available {
		return fmt.Errorf("no update available")
	}
	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return fmt.Errorf("executable path: %w", err)
	}
	return selfupdate.UpdateTo(ctx, info.AssetURL, info.AssetFilename, exe)
}

// compareVersions returns negative / zero / positive for a < / == / > b
// using simple semver-ish numeric segment compare. Tolerant of "v" prefix.
func compareVersions(a, b string) int {
	pa := parseSegments(a)
	pb := parseSegments(b)
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var ai, bi int
		if i < len(pa) {
			ai = pa[i]
		}
		if i < len(pb) {
			bi = pb[i]
		}
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}

func parseSegments(v string) []int {
	if len(v) > 0 && (v[0] == 'v' || v[0] == 'V') {
		v = v[1:]
	}
	var out []int
	cur := 0
	have := false
	for _, c := range v {
		if c >= '0' && c <= '9' {
			cur = cur*10 + int(c-'0')
			have = true
			continue
		}
		if have {
			out = append(out, cur)
			cur = 0
			have = false
		}
		if c != '.' {
			break // stop at first non-dot non-digit (e.g. "-rc1")
		}
	}
	if have {
		out = append(out, cur)
	}
	return out
}
