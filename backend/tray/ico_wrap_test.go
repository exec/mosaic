//go:build !darwin

package tray

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

// TestWrapPNGAsICO_RoundTripsHeader pins the wire format of the
// generated ICO container — width/height advisory zeros, single image
// entry, payload offset = 22, payload size matches the input PNG, and
// the payload itself is the unmodified PNG bytes. Windows's NotifyIcon
// loader silently rejects malformed ICOs, so a structural break here
// would re-introduce the original "tray entry but no icon" bug
// without any other test catching it.
func TestWrapPNGAsICO_RoundTripsHeader(t *testing.T) {
	if len(iconIdleBytes) == 0 {
		t.Fatal("iconIdleBytes embed is empty; was //go:embed broken?")
	}
	// Sanity check the embedded payload IS a PNG before we wrap it.
	if _, err := png.Decode(bytes.NewReader(iconIdleBytes)); err != nil {
		t.Fatalf("embedded idle icon is not a valid PNG: %v", err)
	}

	ico := wrapPNGAsICO(iconIdleBytes)
	if len(ico) != 22+len(iconIdleBytes) {
		t.Fatalf("ICO size: got %d, want %d", len(ico), 22+len(iconIdleBytes))
	}

	// ICONDIR
	if ico[0] != 0 || ico[1] != 0 {
		t.Errorf("ICONDIR.reserved: got %d %d, want 0 0", ico[0], ico[1])
	}
	if ico[2] != 1 || ico[3] != 0 {
		t.Errorf("ICONDIR.type: got %d %d, want 1 0 (icon)", ico[2], ico[3])
	}
	if ico[4] != 1 || ico[5] != 0 {
		t.Errorf("ICONDIR.count: got %d %d, want 1 0", ico[4], ico[5])
	}

	// ICONDIRENTRY: width/height advisory 0 (= read from PNG IHDR);
	// planes=1, bitCount=32; size + offset.
	if ico[10] != 1 || ico[11] != 0 {
		t.Errorf("entry.planes: got %d %d, want 1 0", ico[10], ico[11])
	}
	if ico[12] != 32 || ico[13] != 0 {
		t.Errorf("entry.bitCount: got %d %d, want 32 0", ico[12], ico[13])
	}
	gotSize := binary.LittleEndian.Uint32(ico[14:18])
	if gotSize != uint32(len(iconIdleBytes)) {
		t.Errorf("entry.size: got %d, want %d", gotSize, len(iconIdleBytes))
	}
	gotOffset := binary.LittleEndian.Uint32(ico[18:22])
	if gotOffset != 22 {
		t.Errorf("entry.offset: got %d, want 22", gotOffset)
	}

	// Payload bytes are the original PNG verbatim.
	if !bytes.Equal(ico[22:], iconIdleBytes) {
		t.Error("payload does not match input PNG bytes")
	}
}
