package platform

import (
	"errors"
	"os"
	"path/filepath"
)

// OpenPath hands a local file or folder to the OS without invoking a command shell.
func OpenPath(path string, reveal bool) error {
	if path == "" {
		return errors.New("choose a file or folder first")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return errors.New("the file or folder path is invalid")
	}
	if _, err = os.Stat(absolute); err != nil {
		if os.IsNotExist(err) {
			return errors.New("this file or folder no longer exists. It may have been moved or renamed")
		}
		if os.IsPermission(err) {
			return errors.New("Mosaic cannot access this item. Check its folder permissions")
		}
		return errors.New("this file or folder could not be accessed")
	}
	return openPath(absolute, reveal)
}
