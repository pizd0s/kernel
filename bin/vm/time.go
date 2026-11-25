//go:build linux && amd64

package main

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	weekdays = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	months   = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
)

var hostClocks = map[uint32]uintptr{
	CLOCK_REALTIME:           hostClockRealtime,
	CLOCK_MONOTONIC:          hostClockMonotonic,
	CLOCK_PROCESS_CPUTIME_ID: hostClockProcessCputime,
	CLOCK_THREAD_CPUTIME_ID:  hostClockProcessCputime,
	CLOCK_MONOTONIC_RAW:      hostClockMonotonicRaw,
	CLOCK_REALTIME_COARSE:    hostClockRealtimeCoarse,
	CLOCK_MONOTONIC_COARSE:   hostClockMonotonicCoarse,
	CLOCK_BOOTTIME:           hostClockBoottime,
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

func asctime(tm TmFields) (string, bool) {
	s := fmt.Sprintf("%.3s %.3s%3d %.2d:%.2d:%.2d %d\n", calendarName(weekdays, tm.wday, true), calendarName(months, tm.mon, true),
		tm.mday, tm.hour, tm.min, tm.sec, tm.year+1900)

	return s, len(s) < 26
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

func calendarName(list []string, i int, short bool) string {
	if i < 0 || i >= len(list) {
		return "?"
	}

	if short {
		return list[i][:3]
	}

	return list[i]
}
