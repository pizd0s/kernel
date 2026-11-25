package main

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

var hostClocks = map[uint32]uintptr{
	CLOCK_REALTIME:           unix.CLOCK_REALTIME,
	CLOCK_MONOTONIC:          unix.CLOCK_MONOTONIC,
	CLOCK_PROCESS_CPUTIME_ID: unix.CLOCK_PROCESS_CPUTIME_ID,
	CLOCK_THREAD_CPUTIME_ID:  unix.CLOCK_PROCESS_CPUTIME_ID,
	CLOCK_MONOTONIC_RAW:      unix.CLOCK_MONOTONIC_RAW,
	CLOCK_REALTIME_COARSE:    unix.CLOCK_REALTIME_COARSE,
	CLOCK_MONOTONIC_COARSE:   unix.CLOCK_MONOTONIC_COARSE,
	CLOCK_BOOTTIME:           unix.CLOCK_BOOTTIME,
}

func hostClock(clock uint32) (uintptr, bool) {
	host, ok := hostClocks[clock]

	return host, ok
}

func clockGettime(clock uintptr) time.Duration {
	var t syscall.Timespec
	syscall.Syscall(syscall.SYS_CLOCK_GETTIME, clock, uintptr(unsafe.Pointer(&t)), 0)

	return time.Duration(t.Sec)*time.Second + time.Duration(t.Nsec)
}
