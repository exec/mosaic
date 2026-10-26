package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenPathRejectsMissingItem(t *testing.T) {
	for _, path := range []string{"", filepath.Join(t.TempDir(), strings.Repeat("long", 40)+".mkv")} {
		for _, reveal := range []bool{false, true} {
			err := OpenPath(path, reveal)
			if err == nil || strings.Contains(err.Error(), ".mkv") {
				t.Fatalf("expected concise missing-file error, got %v", err)
			}
		}
	}
}

// Opt in only on a machine with an interactive desktop; this opens its default text app and file manager.
func TestNativeOpenAndReveal(t *testing.T) {
	if os.Getenv("MOSAIC_TEST_NATIVE_OPEN") != "1" {
		t.Skip("requires a desktop session")
	}
	root, err := os.MkdirTemp("", "Mosaic native open ")
	if err != nil {
		t.Fatal(err)
	}
	// Leave this small fixture available while asynchronously launched apps read it.
	path := filepath.Join(root, "Mosaic café #1 % comma, test.txt")
	if err := os.WriteFile(path, []byte("Mosaic native open/reveal smoke test.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("fixture: %s", path)
	for _, reveal := range []bool{false, true} {
		if err := OpenPath(path, reveal); err != nil {
			t.Errorf("reveal=%v: %v", reveal, err)
		}
	}
	if err := OpenPath(root, false); err != nil {
		t.Errorf("folder: %v", err)
	}
}
