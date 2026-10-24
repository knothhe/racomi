//go:build !darwin && !linux

package desktop

import "errors"

func openAtLoginEnabled() bool { return false }
func setOpenAtLogin(bool) error {
	return errors.New("This setting is available only on macOS and Linux")
}
