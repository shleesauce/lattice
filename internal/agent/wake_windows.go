//go:build windows

package agent

import "syscall"

// setSocketBroadcast enables SO_BROADCAST so a UDP socket may send to the
// limited broadcast address (WoL magic packet). Windows fd is a Handle.
func setSocketBroadcast(fd uintptr) error {
	return syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
