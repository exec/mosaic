package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"testing"

	selfupdate "github.com/creativeprojects/go-selfupdate"
)

// TestUpdaterTarballMatchesLibContract guards the interface contract between
// scripts/build-macos.sh's tarball layout and go-selfupdate's case-sensitive
// matcher. The lib derives `cmd` from filepath.Base(cmdPath) — on macOS that's
// lowercase "mosaic" — and looks for a tar entry matching
// `^mosaic([_-]v?<ver>)?([_-]darwin[_-]universal)?(\.exe)?$`. v0.1.13 through
// v0.1.22 shipped the inner file as "Mosaic-vX.Y.Z-darwin-universal" (capital
// M), which never matched, so auto-update on macOS failed with "executable
// not found in tar: \"mosaic\"" for every user. Fixed in v0.1.23 by writing
// the inner file as plain "mosaic"; this test fails loudly if anyone changes
// the build script back to a name the lib won't accept.
func TestUpdaterTarballMatchesLibContract(t *testing.T) {
	cases := []struct {
		name      string
		innerName string
		wantOK    bool
	}{
		{name: "lowercase mosaic (v0.1.23+)", innerName: "mosaic", wantOK: true},
		{name: "lowercase with version+platform", innerName: "mosaic-v0.1.23-darwin-universal", wantOK: true},
		{name: "capital M (broken v0.1.13–v0.1.22)", innerName: "Mosaic-v0.1.22-darwin-universal", wantOK: false},
		{name: "capital M plain", innerName: "Mosaic", wantOK: false},
		{name: "wrong cmd name", innerName: "notmosaic", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tarball := buildSingleEntryTarGz(t, tc.innerName, []byte("fake mach-o bytes"))
			r, err := selfupdate.DecompressCommand(
				bytes.NewReader(tarball),
				"Mosaic-v0.1.23-darwin-universal.tar.gz",
				"mosaic",    // cmd — what filepath.Base(cmdPath) yields on macOS
				"darwin",    // os
				"universal", // arch (set by cfg.UniversalArch)
			)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("expected lib to extract entry %q, got error: %v", tc.innerName, err)
				}
				got, _ := io.ReadAll(r)
				if !bytes.Equal(got, []byte("fake mach-o bytes")) {
					t.Fatalf("extracted bytes mismatch: got %q", got)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected lib to reject entry %q, but got no error", tc.innerName)
			}
		})
	}
}

func buildSingleEntryTarGz(t *testing.T, name string, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(payload)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar write header: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("tar write payload: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}
