//go:build windows

package platform

import (
	"encoding/json"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// EarlyForwardLaunchArgs runs a Wails-compatible single-instance check
// BEFORE any heavy/exclusive init (anacrolix port bind, SQLite DB open).
// Returns true iff a running Mosaic was detected and os.Args[1:] was
// forwarded via WM_COPYDATA — the caller MUST exit immediately when this
// returns true, before touching any process-exclusive resource.
//
// Why this exists:
//
// When the user double-clicks a .torrent in File Explorer with Mosaic set
// as the default handler, Windows spawns a second mosaic.exe with the
// path as os.Args[1]. The second process must:
//
//  1. Detect the running instance,
//  2. Forward the file path to it,
//  3. Exit cleanly.
//
// Wails has SingleInstanceLock for exactly this, but Wails's check runs
// inside wails.Run — by then, our main.go has already opened the SQLite
// DB and constructed the anacrolix backend, which tries to bind a fixed
// listen port. The bind fails because the running instance holds it,
// log.Fatal kills the process, and the file path never reaches the
// SingleInstanceLock dispatch. The user sees nothing happen.
//
// We replicate Wails's exact wire format (mutex name, hidden window class,
// WM_COPYDATA dwData=1542, JSON SecondInstanceData) and run the check at
// the very top of main(), so a second instance exits before binding any
// port. The first instance still uses Wails's normal SingleInstanceLock
// for the running side; our mutex name is intentionally distinct so the
// two checks don't fight.

const wailsCopyDataMarker = 1542

type wailsSecondInstanceData struct {
	Args             []string `json:"Args"`
	WorkingDirectory string   `json:"WorkingDirectory"`
}

type copyDataStruct struct {
	dwData uintptr
	cbData uint32
	lpData uintptr
}

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	procFindWindowExW  = user32.NewProc("FindWindowExW")
	procSendMessageW   = user32.NewProc("SendMessageW")
	wmCopyData    uint = 0x004A
	// HWND_MESSAGE = -3 as a pseudo-handle. Required as the parent argument
	// to FindWindowExW to locate a message-only window (one created with
	// CreateWindowEx using HWND_MESSAGE as parent — exactly what Wails uses
	// for its second-instance receiver). Plain FindWindowW only scans
	// top-level windows and CANNOT find message-only windows; that's why
	// every prior attempt to forward args via FindWindowW returned 0.
	hwndMessage = ^uintptr(2)
)

func EarlyForwardLaunchArgs(uniqueId string) bool {
	if len(os.Args) <= 1 {
		// No args to forward; let the normal path handle "is there a running instance"
		// via Wails's own check (in which case the second process exits without
		// having done anything observable to the user).
		return false
	}

	// Distinct mutex name from Wails's so the running instance's Wails-side
	// SingleInstanceLock setup doesn't think this name is taken by us.
	earlyName := "mosaic-early-singleinstance-" + uniqueId
	_, createErr := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(earlyName))
	if createErr != windows.ERROR_ALREADY_EXISTS {
		// First instance: hold the mutex for the lifetime of the process.
		// (createErr==nil means we just created it.)
		return false
	}

	// Another Mosaic is running. Find Wails's hidden message-only window.
	// Wails creates it with class+window names "wails-app-<id>-sic" / "-siw".
	className := "wails-app-" + uniqueId + "-sic"
	windowName := "wails-app-" + uniqueId + "-siw"
	classW, _ := syscall.UTF16PtrFromString(className)
	windowW, _ := syscall.UTF16PtrFromString(windowName)

	// Brief retry: the running instance may still be initializing Wails when
	// File Explorer launches us. Wait up to ~1s for the window to appear.
	// FindWindowExW(HWND_MESSAGE, NULL, class, window) is the correct API
	// for locating message-only windows; plain FindWindowW only scans
	// top-level windows and silently returns 0 here.
	var hwnd uintptr
	for i := 0; i < 50; i++ {
		r, _, _ := procFindWindowExW.Call(hwndMessage, 0, uintptr(unsafe.Pointer(classW)), uintptr(unsafe.Pointer(windowW)))
		if r != 0 {
			hwnd = r
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if hwnd == 0 {
		// Running instance's window never appeared — fall back to the normal
		// path. The second process will still exit later via Wails's own
		// SingleInstanceLock check, but the file may be lost.
		return false
	}

	cwd, _ := os.Getwd()
	data := wailsSecondInstanceData{Args: os.Args[1:], WorkingDirectory: cwd}
	serialized, err := json.Marshal(data)
	if err != nil {
		return false
	}

	utf16, err := windows.UTF16FromString(string(serialized))
	if err != nil {
		return false
	}
	cd := copyDataStruct{
		dwData: wailsCopyDataMarker,
		cbData: uint32(len(utf16)*2 + 1),
		lpData: uintptr(unsafe.Pointer(&utf16[0])),
	}
	procSendMessageW.Call(hwnd, uintptr(wmCopyData), 0, uintptr(unsafe.Pointer(&cd)))
	return true
}
