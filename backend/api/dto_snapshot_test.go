// dto_snapshot_test.go pins the on-the-wire JSON shape of every DTO that the
// frontend hand-mirrors at frontend/src/lib/bindings.ts. Wails generates
// opaque type names for bound methods (api.TorrentDTO is just a name, no
// fields), so silent renames or omitempty flips on the Go side would otherwise
// reach the SPA undetected. This test marshals one stable instance of each
// DTO to JSON and diffs it against a golden in testdata/dto_snapshots/.
//
// What it catches: field renames, json-tag drift, omitempty toggles,
// case changes, and nil-pointer-vs-omitted differences.
//
// What it does NOT catch: semantic changes (e.g. units flipping from kbps to
// bps), or new DTOs that nobody added a snapshot for.
//
// Updating goldens:
//
//	UPDATE_SNAPSHOTS=1 go test ./backend/api/... -run DTOSnapshot
//
// IMPORTANT: any diff in testdata/dto_snapshots/ during PR review almost
// certainly means frontend/src/lib/bindings.ts needs to be updated in the
// SAME PR. The whole point of this test is to surface that coupling at
// review time instead of at runtime.
//
// Pointer fields (TorrentDTO.CategoryID, FilterDTO.CategoryID) are
// snapshotted in BOTH nil and set states because their JSON output differs
// (no omitempty → emits "null"; with omitempty → key is absent). That
// inconsistency is intentional — the frontend already handles `number | null`
// for those fields. We're locking it in, not endorsing it.
package api

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateSnapshots is set via the UPDATE_SNAPSHOTS=1 env var and also exposed
// as a -update-snapshots flag for editor integrations that prefer flags.
var updateSnapshots = flag.Bool("update-snapshots", false, "rewrite DTO snapshot goldens instead of comparing")

func shouldUpdateSnapshots() bool {
	if *updateSnapshots {
		return true
	}
	return os.Getenv("UPDATE_SNAPSHOTS") == "1"
}

// snapshotCase is one fixture: a name (used as the golden filename) and the
// concrete value to marshal. Names use the Go type name; pointer-variant
// cases append a suffix like "_null" or "_set".
type snapshotCase struct {
	name  string
	value any
}

// dtoSnapshotCases returns one stable, fully-populated instance of every DTO
// that crosses the Go↔TS boundary. When a struct has *T fields without
// omitempty, we emit two cases (nil + set) so both JSON shapes get pinned.
func dtoSnapshotCases() []snapshotCase {
	intPtr := func(n int) *int { i := n; return &i }

	tags := []TagDTO{{ID: 7, Name: "linux", Color: "#3366ff"}}
	files := []FileDTO{{
		Index: 0, Path: "dir/file.bin", Size: 1024, BytesDone: 512, Progress: 0.5, Priority: "high",
	}}
	peers := []PeerDTO{{
		IP: "10.0.0.1", Port: 51413, Client: "qBittorrent", Flags: "uTPex",
		Progress: 0.42, DownloadRate: 1024, UploadRate: 2048, Country: "US",
	}}
	trackers := []TrackerDTO{{
		URL: "udp://tracker.example.com:1337", Status: "ok", Seeds: 10, Peers: 20,
		Downloaded: 100, LastAnnounce: 1735689600, NextAnnounce: 1735690200,
	}}

	torrentBase := TorrentDTO{
		ID:            "deadbeef00112233445566778899aabbccddeeff",
		Name:          "Example.Release.1080p",
		Magnet:        "magnet:?xt=urn:btih:deadbeef",
		SavePath:      "/home/user/Downloads",
		TotalBytes:    2_147_483_648,
		BytesDone:     1_073_741_824,
		Progress:      0.5,
		DownloadRate:  524288,
		UploadRate:    65536,
		Peers:         15,
		Seeds:         5,
		Paused:        false,
		Completed:     false,
		AddedAt:       1735689600,
		CategoryID:    nil,
		Tags:          tags,
		QueuePosition: 1,
		ForceStart:    false,
		Queued:        false,
		Verifying:     false,
		FilesMissing:  false,
	}
	torrentWithCategory := torrentBase
	torrentWithCategory.CategoryID = intPtr(42)

	detailBase := DetailDTO{
		ID:          "deadbeef00112233445566778899aabbccddeeff",
		Name:        "Example.Release.1080p",
		Magnet:      "magnet:?xt=urn:btih:deadbeef",
		SavePath:    "/home/user/Downloads",
		TotalBytes:  2_147_483_648,
		BytesDone:   1_073_741_824,
		Progress:    0.5,
		Ratio:       0.75,
		TotalDown:   1_073_741_824,
		TotalUp:     805_306_368,
		Peers:       15,
		Seeds:       5,
		AddedAt:     1735689600,
		CompletedAt: 0, // omitempty → absent
	}
	detailFull := detailBase
	detailFull.CompletedAt = 1735776000
	detailFull.Files = files
	detailFull.PeersList = peers
	detailFull.Trackers = trackers

	filterBase := FilterDTO{
		ID:         3,
		FeedID:     7,
		Regex:      `1080p\.x265`,
		CategoryID: nil,
		SavePath:   "/home/user/Downloads/Shows",
		Enabled:    true,
	}
	filterWithCategory := filterBase
	filterWithCategory.CategoryID = intPtr(42)

	blocklistBase := BlocklistDTO{
		URL:          "https://example.com/blocklist.gz",
		Enabled:      true,
		LastLoadedAt: 1735689600,
		Entries:      12345,
		Error:        "", // omitempty → absent
	}
	blocklistError := blocklistBase
	blocklistError.Error = "fetch failed: 503"

	return []snapshotCase{
		{"CategoryDTO", CategoryDTO{
			ID: 42, Name: "Movies", DefaultSavePath: "/home/user/Downloads/Movies", Color: "#ff8800",
		}},
		{"TagDTO", tags[0]},
		{"TorrentDTO_null_category", torrentBase},
		{"TorrentDTO_with_category", torrentWithCategory},
		{"LimitsDTO", LimitsDTO{
			DownKbps: 5000, UpKbps: 1000, AltDownKbps: 1000, AltUpKbps: 200, AltActive: false,
		}},
		{"QueueLimitsDTO", QueueLimitsDTO{
			MaxActiveDownloads: 3, MaxActiveSeeds: 5,
		}},
		{"PeerLimitsDTO", PeerLimitsDTO{
			ListenPort: 51413, MaxPeersPerTorrent: 80, DHTEnabled: true, EncryptionEnabled: true,
		}},
		{"GlobalStats", GlobalStats{
			TotalTorrents: 10, ActiveTorrents: 3, SeedingTorrents: 2,
			TotalDownloadRate: 1024000, TotalUploadRate: 256000, TotalPeers: 42,
		}},
		{"FileDTO", files[0]},
		{"PeerDTO", peers[0]},
		{"TrackerDTO", trackers[0]},
		{"ScheduleRuleDTO", ScheduleRuleDTO{
			ID: 1, DaysMask: 0b1111100, StartMin: 540, EndMin: 1080,
			DownKbps: 2000, UpKbps: 500, AltOnly: false, Enabled: true,
		}},
		{"BlocklistDTO_no_error", blocklistBase},
		{"BlocklistDTO_with_error", blocklistError},
		{"FeedDTO", FeedDTO{
			ID: 1, URL: "https://example.com/feed.xml", Name: "Example Feed",
			IntervalMin: 60, LastPolled: 1735689600, ETag: `W/"abc123"`, Enabled: true,
		}},
		{"FilterDTO_null_category", filterBase},
		{"FilterDTO_with_category", filterWithCategory},
		{"DetailDTO_overview_only", detailBase},
		{"DetailDTO_full", detailFull},
		{"WebConfigDTO", WebConfigDTO{
			Enabled: true, Port: 8080, BindAll: false, Username: "admin",
			APIKey: "k_0123456789abcdef0123456789abcdef",
		}},
		{"UpdaterConfigDTO", UpdaterConfigDTO{
			Enabled: true, Channel: "stable", LastCheckedAt: 1735689600,
			LastSeenVersion: "0.4.3", InstallSource: "apt",
		}},
		{"UpdateInfoDTO", UpdateInfoDTO{
			Available: true, LatestVersion: "0.4.4",
			AssetURL:       "https://example.com/Mosaic-0.4.4.dmg",
			AssetFilename:  "Mosaic-0.4.4.dmg",
			CheckedAt:      1735689600,
			CurrentVersion: "0.4.3",
		}},
		{"DesktopIntegrationDTO", DesktopIntegrationDTO{
			TrayEnabled: true, CloseToTray: true, StartMinimized: false,
			NotifyOnComplete: true, NotifyOnError: true, NotifyOnUpdate: true,
		}},
	}
}

func TestDTOSnapshot(t *testing.T) {
	dir := filepath.Join("testdata", "dto_snapshots")
	if shouldUpdateSnapshots() {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	for _, tc := range dtoSnapshotCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			enc := json.NewEncoder(&buf)
			enc.SetIndent("", "  ")
			// SetEscapeHTML(false) keeps regex characters like < > & readable
			// in goldens (FilterDTO.Regex etc.).
			enc.SetEscapeHTML(false)
			if err := enc.Encode(tc.value); err != nil {
				t.Fatalf("marshal %s: %v", tc.name, err)
			}
			got := buf.Bytes()

			path := filepath.Join(dir, tc.name+".json")

			if shouldUpdateSnapshots() {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatalf("write golden %s: %v", path, err)
				}
				return
			}

			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden %s: %v\n(rerun with UPDATE_SNAPSHOTS=1 to create it)", path, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("DTO %s drifted from golden %s\n--- want ---\n%s\n--- got ---\n%s\n(rerun with UPDATE_SNAPSHOTS=1 if intentional, and update frontend/src/lib/bindings.ts in the same PR)",
					tc.name, path, string(want), string(got))
			}
		})
	}
}
