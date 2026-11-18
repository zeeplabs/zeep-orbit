//go:build !linux

package main

// hardenProcess is a no-op outside Linux. The builder runs on Linux in
// production; other platforms (macOS) are for development only and have no
// /proc/<pid>/environ for build steps to read.
func hardenProcess() error { return nil }
