//go:build windows

package agent

import (
	"os/exec"
	"strconv"
	"syscall"

	"golang.org/x/sys/windows"
)

// setProcGroup starts the child in a new process group so its descendants are
// grouped under it. The tree teardown itself is done by taskkill /T (below),
// which walks the parent/child chain regardless of group, so this flag is mainly
// belt-and-suspenders. Used for the editor's exec.Cmd; harmless on a go-pty Cmd.
func setProcGroup(attr *syscall.SysProcAttr) {
	attr.CreationFlags |= windows.CREATE_NEW_PROCESS_GROUP
}

// killProcessGroup force-kills the process and its entire descendant tree.
// Windows has no signal-the-negative-pid equivalent, so we shell out to taskkill
// with /T (terminate the tree) and /F (force) — the proven way to ensure
// code-server's node workers/extension hosts and claude's MCP servers/hooks
// don't leak or keep loopback ports bound across create/close cycles (FIX 1).
func killProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	cmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
}
