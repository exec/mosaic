//go:build darwin

package tray

import (
	"testing"
)

// TestDarwin_iconBytesEmbedded sanity-checks that the go:embed directives
// resolved real PNG payloads at compile time. We don't actually feed them
// to AppKit here (that would require an NSApplication and the menu bar),
// just verify the bytes are present and look like PNGs.
func TestDarwin_iconBytesEmbedded(t *testing.T) {
	cases := []struct {
		name string
		buf  []byte
	}{
		{"idle", iconIdleBytes},
		{"active", iconActiveBytes},
		{"error", iconErrorBytes},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if len(c.buf) == 0 {
				t.Fatalf("%s icon bytes empty; go:embed did not resolve", c.name)
			}
			// PNG magic: 0x89 'P' 'N' 'G' 0x0D 0x0A 0x1A 0x0A.
			if len(c.buf) < 8 || c.buf[0] != 0x89 || c.buf[1] != 'P' || c.buf[2] != 'N' || c.buf[3] != 'G' {
				t.Fatalf("%s icon does not look like a PNG (first bytes: % x)", c.name, c.buf[:min(8, len(c.buf))])
			}
		})
	}
}

// TestDarwin_iconBytesForState exercises the state-to-bytes selector
// without touching AppKit.
func TestDarwin_iconBytesForState(t *testing.T) {
	tr := New(Callbacks{})
	d, ok := tr.impl.(*darwinImpl)
	if !ok {
		t.Fatalf("expected *darwinImpl, got %T", tr.impl)
	}

	tr.iconState.Store(int32(IconIdle))
	if got := d.iconBytesForState(); len(got) == 0 {
		t.Fatal("idle returned empty bytes")
	}
	tr.iconState.Store(int32(IconActive))
	if got := d.iconBytesForState(); len(got) == 0 {
		t.Fatal("active returned empty bytes")
	}
	tr.iconState.Store(int32(IconError))
	if got := d.iconBytesForState(); len(got) == 0 {
		t.Fatal("error returned empty bytes")
	}
}

// TestDarwin_constructWithoutStart confirms we can build the *Tray struct,
// flip state, and call Stop() without ever invoking Start (which would
// allocate an NSStatusItem and require a real menu bar / NSApplication).
func TestDarwin_constructWithoutStart(t *testing.T) {
	tr := New(Callbacks{
		OnShow:           func() {},
		OnPauseAll:       func() {},
		OnResumeAll:      func() {},
		OnToggleAltSpeed: func() {},
		OnOpenSettings:   func() {},
		OnQuit:           func() {},
	})
	if tr == nil {
		t.Fatal("New returned nil")
	}
	tr.SetIconState(IconActive)
	tr.SetPaused(true)
	tr.SetAltSpeedActive(true)
	// Stop is safe before Start — it should observe started=false via the
	// running flag and do nothing.
	tr.Stop()
}
