//go:build !windows

package agent

import "syscall"

// setSocketBroadcast enables SO_BROADCAST so a UDP socket may send to the
// limited broadcast address (WoL magic packet). Unix fd is an int.
func setSocketBroadcast(fd uintptr) error {
	return syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
}
