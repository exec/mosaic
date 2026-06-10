package updater

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeSource struct {
	latestTag   string
	latestAsset string
	latestErr   error

	// Install observability for tests.
	installCalls int
	installExe   string
	installErr   error
}

func (f *fakeSource) DetectLatest(_ context.Context) (string, string, string, error) {
	if f.latestErr != nil {
		return "", "", "", f.latestErr
	}
	return f.latestTag, "https://example.com/release", f.latestAsset, nil
}

func (f *fakeSource) Install(_ context.Context, exe string) error {
	f.installCalls++
	f.installExe = exe
	return f.installErr
}

func TestCheck_NewerVersion(t *testing.T) {
	u := New(Config{
		CurrentVersion: "v0.7.0",
		Source: &fakeSource{
			latestTag:   "v0.8.0",
			latestAsset: "mosaic_v0.8.0_darwin_arm64.tar.gz",
		},
	})
	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available {
		t.Fatal("expected available=true")
	}
	if info.LatestVersion != "v0.8.0" {
		t.Fatalf("got version %q", info.LatestVersion)
	}
	if info.AssetFilename != "mosaic_v0.8.0_darwin_arm64.tar.gz" {
		t.Fatalf("got asset filename %q", info.AssetFilename)
	}
}

func TestCheck_SameVersion(t *testing.T) {
	u := New(Config{
		CurrentVersion: "v0.7.0",
		Source:         &fakeSource{latestTag: "v0.7.0"},
	})
	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Available {
		t.Fatal("expected available=false")
	}
}

func TestCheck_OlderRemote(t *testing.T) {
	u := New(Config{
		CurrentVersion: "v0.9.0",
		Source:         &fakeSource{latestTag: "v0.7.0"},
	})
	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Available {
		t.Fatal("expected available=false on older remote")
	}
}

func TestCheck_DevBuild(t *testing.T) {
	u := New(Config{
		CurrentVersion: "dev",
		Source:         &fakeSource{latestTag: "v0.7.0"},
	})
	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Available {
		t.Fatal("expected available=false in dev")
	}
}

func TestCheck_NetworkError(t *testing.T) {
	u := New(Config{
		CurrentVersion: "v0.7.0",
		Source:         &fakeSource{latestErr: errors.New("offline")},
	})
	_, err := u.Check(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int // sign of compareVersions(a, b)
	}{
		{"v0.7.0", "v0.7.0", 0},
		{"v0.8.0", "v0.7.0", 1},
		{"v0.7.0", "v0.8.0", -1},
		{"v0.7.10", "v0.7.9", 1},
		{"v1.0", "v1.0.0", 0}, // missing segments are zero
		{"0.8.0", "v0.8.0", 0},
		// Pre-release sorts below the same-numbered release.
		{"v0.8.0-rc1", "v0.8.0", -1},
		{"v0.8.0", "v0.8.0-rc1", 1},
		// Pre-releases of the same core compare lexicographically.
		{"v0.8.0-rc1", "v0.8.0-rc2", -1},
		{"v0.8.0-rc2", "v0.8.0-rc1", 1},
		{"v0.8.0-rc1", "v0.8.0-rc1", 0},
		{"v0.8.0-beta", "v0.8.0-rc1", -1},
		// Pre-release of a HIGHER core version still wins.
		{"v0.9.0-rc1", "v0.8.0", 1},
		{"v0.8.0-rc1", "v0.7.5", 1},
		// Build metadata is ignored for precedence.
		{"v0.8.0+build5", "v0.8.0", 0},
		{"v0.8.0-rc1+build5", "v0.8.0-rc1", 0},
	}
	sign := func(n int) int {
		switch {
		case n < 0:
			return -1
		case n > 0:
			return 1
		}
		return 0
	}
	for _, tc := range cases {
		if got := sign(compareVersions(tc.a, tc.b)); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestCheck_PrereleaseUserOfferedFinal(t *testing.T) {
	u := New(Config{
		CurrentVersion: "v0.8.0-rc1",
		Source:         &fakeSource{latestTag: "v0.8.0"},
	})
	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available {
		t.Fatal("expected rc user to be offered the final release")
	}
}

func TestSchedule_TickRespectsCancel(t *testing.T) {
	u := New(Config{
		CurrentVersion: "v0.7.0",
		Source:         &fakeSource{latestTag: "v0.7.0"},
		Interval:       10 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { u.Schedule(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Schedule did not exit on cancel")
	}
}

func TestInstall_DelegatesToSourceWhenAvailable(t *testing.T) {
	src := &fakeSource{
		latestTag:   "v0.8.0",
		latestAsset: "mosaic_v0.8.0_darwin_arm64.tar.gz",
	}
	u := New(Config{CurrentVersion: "v0.7.0", Source: src})
	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available {
		t.Fatal("expected available=true")
	}

	if err := u.Install(context.Background(), info); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if src.installCalls != 1 {
		t.Fatalf("expected Source.Install called once, got %d", src.installCalls)
	}
	if src.installExe == "" {
		t.Fatal("expected non-empty exe path passed to Source.Install")
	}
}

func TestInstall_TargetsAppImagePathWhenWrapped(t *testing.T) {
	// Inside an AppImage the resolved executable lives on a read-only
	// squashfs mount; Install must target the .AppImage file itself.
	t.Setenv("APPIMAGE", "/home/user/Applications/Mosaic.AppImage")
	src := &fakeSource{
		latestTag:   "v0.8.0",
		latestAsset: "Mosaic-v0.8.0-linux-amd64.AppImage",
	}
	u := New(Config{CurrentVersion: "v0.7.0", Source: src})
	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := u.Install(context.Background(), info); err != nil {
		t.Fatalf("Install returned error: %v", err)
	}
	if src.installExe != "/home/user/Applications/Mosaic.AppImage" {
		t.Fatalf("expected install target to be $APPIMAGE, got %q", src.installExe)
	}
}

func TestInstall_ErrorsWhenNotAvailable(t *testing.T) {
	src := &fakeSource{}
	u := New(Config{CurrentVersion: "v0.7.0", Source: src})
	err := u.Install(context.Background(), Info{Available: false})
	if err == nil {
		t.Fatal("expected error when info.Available is false")
	}
	if src.installCalls != 0 {
		t.Fatalf("Source.Install should not be called when not available, got %d", src.installCalls)
	}
}

func TestInstall_PropagatesSourceError(t *testing.T) {
	src := &fakeSource{
		latestTag:   "v0.8.0",
		latestAsset: "mosaic_v0.8.0_darwin_arm64.tar.gz",
		installErr:  errors.New("checksum mismatch"),
	}
	u := New(Config{CurrentVersion: "v0.7.0", Source: src})
	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = u.Install(context.Background(), info)
	if err == nil {
		t.Fatal("expected install error to propagate")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected source error wrapped, got %v", err)
	}
}
