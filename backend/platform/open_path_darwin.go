package platform

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

func openPath(path string, reveal bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{"--", path}
	if reveal {
		args = append([]string{"-R"}, args...)
	}
	if err := exec.CommandContext(ctx, "/usr/bin/open", args...).Run(); err != nil {
		if reveal {
			return errors.New("Finder could not reveal this file. Check that the download folder is available")
		}
		return errors.New("macOS could not open this item. Choose a default app in Finder’s Get Info, or open its download folder")
	}
	return nil
}
