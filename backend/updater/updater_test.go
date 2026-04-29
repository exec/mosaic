package updater

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeSource struct {
	latestTag string
	latestErr error
}

func (f *fakeSource) DetectLatest(_ context.Context) (string, string, error) {
	if f.latestErr != nil {
		return "", "", f.latestErr
	}
	return f.latestTag, "https://example.com/release", nil
}

func TestCheck_NewerVersion(t *testing.T) {
	u := New(Config{
		CurrentVersion: "v0.7.0",
		Source:         &fakeSource{latestTag: "v0.8.0"},
	})
	info, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Available {
		t.Fatal("expected available=true")
	}
	if info.LatestVersion != "v0.8.0" {
		t.Fatalf("got %q", info.LatestVersion)
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
