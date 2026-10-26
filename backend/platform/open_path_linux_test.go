package platform

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestRevealFallsBackToParentURI(t *testing.T) {
	root := t.TempDir()
	capture := filepath.Join(root, "argument")
	t.Setenv("PATH", root)
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path="+filepath.Join(root, "missing-bus"))
	t.Setenv("MOSAIC_TEST_CAPTURE", capture)
	if err := os.WriteFile(filepath.Join(root, "xdg-open"), []byte("#!/bin/sh\nprintf '%s' \"$1\" > \"$MOSAIC_TEST_CAPTURE\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "Café # % folder")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "movie.txt")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := OpenPath(path, true); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	want := (&url.URL{Scheme: "file", Path: folder}).String()
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
