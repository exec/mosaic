//go:build !darwin && !linux && !windows

package platform

import "errors"

func openPath(_ string, _ bool) error {
	return errors.New("opening files is not supported on this platform")
}
