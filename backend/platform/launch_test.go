package platform

import (
	"encoding/json"
	"net"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeLaunchArgs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "space #%.torrent")
	uriPath := filepath.ToSlash(path)
	if uriPath[0] != '/' {
		uriPath = "/" + uriPath
	}
	uri := (&url.URL{Scheme: "file", Path: uriPath}).String()
	magnet := "magnet:?xt=urn:btih:abc&dn=file.torrent"
	require.Equal(t, []string{path, filepath.Join(dir, "relative.torrent"), magnet, "--flag"},
		NormalizeLaunchArgs([]string{uri, "relative.torrent", magnet, "--flag"}, dir))
}

func TestSecondInstanceDecodesWithoutEOF(t *testing.T) {
	for _, args := range [][]string{{"relative.torrent"}, {}} {
		server, client := net.Pipe()
		out := make(chan []string, 1)
		go handleSecondInstanceConn(server, out)
		dir := t.TempDir()
		require.NoError(t, json.NewEncoder(client).Encode(secondInstancePayload{Args: args, WorkingDirectory: dir}))
		// Keep the writing side open, as the Windows named-pipe sender does.
		select {
		case got := <-out:
			require.Equal(t, NormalizeLaunchArgs(args, dir), got)
		case <-time.After(time.Second):
			t.Fatal("receiver waited for EOF instead of dispatching the launch")
		}
		require.NoError(t, client.Close())
	}
}
