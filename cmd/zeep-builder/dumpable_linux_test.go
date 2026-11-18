//go:build linux

package main

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

func TestHardenProcessMakesDaemonNonDumpable(t *testing.T) {
	if err := hardenProcess(); err != nil {
		t.Fatalf("hardenProcess: %v", err)
	}
	dumpable, err := isDumpable()
	if err != nil {
		t.Fatal(err)
	}
	if dumpable {
		t.Fatal("process is still dumpable after hardenProcess")
	}

	// A same-uid child without CAP_SYS_PTRACE must not read our environ.
	// Skipped as root (or with CAP_SYS_PTRACE), where the kernel allows it.
	if os.Geteuid() == 0 {
		t.Skip("running as root: the /proc permission check is bypassed")
	}
	cat, err := exec.LookPath("cat")
	if err != nil {
		t.Skip("cat not available")
	}
	out, err := exec.Command(cat, "/proc/"+strconv.Itoa(os.Getpid())+"/environ").CombinedOutput()
	if err == nil {
		t.Fatalf("child read the daemon's environ (%d bytes)", len(out))
	}
}
