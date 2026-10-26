package platform

import (
	"errors"

	"golang.org/x/sys/windows"
)

func openPath(path string, reveal bool) error {
	target, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return errors.New("the file path contains an invalid character")
	}
	var params *uint16
	if reveal {
		// Explorer requires the path quoted after /select, rather than quoting the entire switch.
		params, err = windows.UTF16PtrFromString(`/select,"` + path + `"`)
		if err != nil {
			return errors.New("the file path contains an invalid character")
		}
		target = windows.StringToUTF16Ptr("explorer.exe")
	}
	if err := windows.ShellExecute(0, nil, target, params, nil, windows.SW_SHOWNORMAL); err != nil {
		if reveal {
			return errors.New("File Explorer could not reveal this file. Check that the download folder is available")
		}
		return errors.New("Windows could not open this item. Choose a default app for this file type, or open its download folder")
	}
	return nil
}
