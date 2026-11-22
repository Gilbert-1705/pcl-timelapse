//go:build unix

package main

import "syscall"

// renderNice is the niceness ffmpeg renders with in the low profile, so the
// server and the printer connection stay responsive on a small computer.
const renderNice = 10

// lowerPriority makes the process nicer. A failure only costs speed and is
// not reported.
func lowerPriority(pid int) {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, renderNice)
}
