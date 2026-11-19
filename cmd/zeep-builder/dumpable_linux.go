//go:build linux

package main

import (
	"fmt"
	"syscall"
)

// hardenProcess marks the daemon non-dumpable (prctl PR_SET_DUMPABLE 0).
//
// Build steps run code the agent wrote (the Vite config, npm scripts) as the
// same uid as the daemon. A same-uid process can normally read
// /proc/<daemon pid>/environ, which still holds BUILDER_SHARED_SECRET, and
// /proc/<pid>/mem. For a non-dumpable process the kernel requires
// CAP_SYS_PTRACE for those files (and for ptrace attach), and the container
// drops every capability, so the children can no longer read the secret from
// the daemon. Children are unaffected: execve resets the flag for them.
//
// This is a mitigation, not session isolation: sessions still share one uid
// and one volume (see docs/docs/ai-frontend-builder.md, Isolation model).
func hardenProcess() error {
	if _, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_SET_DUMPABLE, 0, 0); errno != 0 {
		return fmt.Errorf("prctl(PR_SET_DUMPABLE, 0): %w", errno)
	}
	return nil
}

// isDumpable reports the current PR_GET_DUMPABLE value (for tests).
func isDumpable() (bool, error) {
	r, _, errno := syscall.RawSyscall(syscall.SYS_PRCTL, syscall.PR_GET_DUMPABLE, 0, 0)
	if errno != 0 {
		return false, errno
	}
	return r != 0, nil
}
