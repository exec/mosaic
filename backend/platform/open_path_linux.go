package platform

import (
	"context"
	"errors"
	"net/url"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"
)

func openPath(path string, reveal bool) error {
	if reveal {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		// GNOME/KDE can select the file; other desktops fall back to its parent folder.
		if bus, err := dbus.ConnectSessionBus(); err == nil {
			uri := (&url.URL{Scheme: "file", Path: path}).String()
			err = bus.Object("org.freedesktop.FileManager1", "/org/freedesktop/FileManager1").CallWithContext(ctx, "org.freedesktop.FileManager1.ShowItems", 0, []string{uri}, "").Err
			bus.Close()
			if err == nil {
				return nil
			}
		}
		path = filepath.Dir(path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	if err := exec.CommandContext(ctx, "xdg-open", uri).Run(); err != nil {
		return errors.New("the desktop could not open this item. Check its default application and that a desktop session is available")
	}
	return nil
}
