//go:build !windows

package agent

import (
	"syscall"
	"time"
)

// procGroupGrace is how long we wait after SIGTERM before escalating to SIGKILL,
// giving children (node workers, MCP servers, hooks) a chance to flush and exit
// cleanly before they're force-killed.
const procGroupGrace = 3 * time.Second

// setProcGroup puts a plain (non-PTY) child into its own process group so the
// whole subtree can be signalled at once via the negative pid. Used for the
// editor's exec.Cmd. NOTE: do NOT call this on a go-pty Cmd — its unix start
// already sets Setsid (a new session implies a new group with PGID == child
// pid), and combining Setsid with Setpgid is rejected by the kernel.
func setProcGroup(attr *syscall.SysProcAttr) {
	attr.Setpgid = true
}

// killProcessGroup terminates an entire process group by sending the signal to
// the negative pid (the group leader's pid IS the PGID, because the child was
// started as a group/session leader). SIGTERM first for a graceful shutdown,
// then SIGKILL after a short grace so orphaned descendants — code-server's node
// workers/extension hosts, claude's MCP servers/hooks — can't leak processes or
// keep loopback ports bound across create/close cycles (FIX 1).
func killProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	// A negative pid targets the whole group led by |pid|.
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func() {
		time.Sleep(procGroupGrace)
		_ = syscall.Kill(-pid, syscall.SIGKILL)
	}()
}
