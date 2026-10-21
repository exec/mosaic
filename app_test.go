package main

import "testing"

func TestLaunchForwardingBeforeBackendStartup(t *testing.T) {
	app := NewApp(nil, nil)
	app.ShowWindow()
	app.HandleLaunchArgs([]string{"example.torrent"})
	if len(app.pendingArgs) != 1 || app.pendingArgs[0][0] != "example.torrent" {
		t.Fatal("launch arguments must wait for startup before accessing the backend")
	}
}
