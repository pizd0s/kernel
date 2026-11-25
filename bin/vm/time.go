package main

import (
	"encoding/binary"
	"strconv"
	"strings"
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

func posixZone(tz string) *time.Location {
	i := 0

	if strings.HasPrefix(tz, "<") {
		i = strings.IndexByte(tz, '>') + 1

		if i == 0 {
			return time.UTC
		}
	} else {
		for i < len(tz) && (tz[i] >= 'A' && tz[i] <= 'Z' || tz[i] >= 'a' && tz[i] <= 'z') {
			i++
		}
	}

	std := strings.Trim(tz[:i], "<>")
	rest := tz[i:]
	sign := 1

	if rest != "" && (rest[0] == '+' || rest[0] == '-') {
		if rest[0] == '-' {
			sign = -1
		}

		rest = rest[1:]
	}

	offset := 0

	for k, unit := range []int{3600, 60, 1} {
		j := 0

		for j < len(rest) && isDigit(rest[j]) {
			j++
		}

		if j == 0 {
			if k == 0 {
				return time.UTC
			}

			break
		}

		n, _ := strconv.Atoi(rest[:j])

		offset += n * unit
		rest = rest[j:]

		if !strings.HasPrefix(rest, ":") {
			break
		}

		rest = rest[1:]
	}

	if rest != "" {
		if loc, err := footerZone(tz); err == nil {
			return loc
		}
	}

	return time.FixedZone(std, -sign*offset)
}

func footerZone(tz string) (*time.Location, error) {
	var data []byte

	for range 2 {
		data = append(data, "TZif2"...)
		data = append(data, make([]byte, 15)...)

		for _, n := range []uint32{0, 0, 0, 0, 1, 4} {
			data = binary.BigEndian.AppendUint32(data, n)
		}

		data = append(data, 0, 0, 0, 0, 0, 0)
		data = append(data, "UTC\x00"...)
	}

	return time.LoadLocationFromTZData(tz, append(append(append(data, '\n'), tz...), '\n'))
}

type TmFields struct {
	sec, min, hour, mday, mon, year, wday, yday, isdst, gmtoff int
	zone                                                       string
}

func tmOf(t time.Time) TmFields {
	name, offset := t.Zone()
	tm := TmFields{t.Second(), t.Minute(), t.Hour(), t.Day(), int(t.Month()) - 1, t.Year() - 1900, int(t.Weekday()), t.YearDay() - 1, 0, offset, name}

	if t.IsDST() {
		tm.isdst = 1
	}

	return tm
}
