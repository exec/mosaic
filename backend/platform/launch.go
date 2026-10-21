package platform

import (
	"encoding/json"
	"io"
	"net"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type secondInstancePayload struct {
	Args             []string `json:"args"`
	WorkingDirectory string   `json:"working_directory"`
}

// NormalizeLaunchArgs handles desktop file URLs and resolves forwarded relative
// paths against the launching process's directory, without changing our cwd.
func NormalizeLaunchArgs(args []string, workingDirectory string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		if u, err := url.Parse(arg); err == nil && u.Scheme == "file" {
			if u.Host == "" || strings.EqualFold(u.Host, "localhost") {
				arg = filepath.FromSlash(u.Path)
				if runtime.GOOS == "windows" && len(arg) > 2 && arg[0] == '\\' && arg[2] == ':' {
					arg = arg[1:]
				}
			} else if runtime.GOOS == "windows" {
				arg = `\\` + u.Host + filepath.FromSlash(u.Path)
			}
		}
		if workingDirectory != "" && !filepath.IsAbs(arg) && strings.EqualFold(filepath.Ext(arg), ".torrent") && !strings.Contains(arg, ":") {
			arg = filepath.Join(workingDirectory, arg)
		}
		out[i] = arg
	}
	return out
}

func handleSecondInstanceConn(conn net.Conn, out chan<- []string) {
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var payload secondInstancePayload
	// Decode a complete JSON value: Windows pipes cannot half-close, so
	// waiting for EOF deadlocks the sender until the read deadline expires.
	if err := json.NewDecoder(io.LimitReader(conn, 64*1024)).Decode(&payload); err != nil {
		return
	}
	select {
	case out <- NormalizeLaunchArgs(payload.Args, payload.WorkingDirectory):
	default:
	}
}

func dispatchSecondInstances(in <-chan []string, onArgs func(args []string)) {
	for args := range in {
		go onArgs(args)
	}
}
