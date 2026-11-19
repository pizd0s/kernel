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

func strftime(format string, tm TmFields) string {
	var out strings.Builder

	for i := 0; i < len(format); i++ {
		c := format[i]

		if c != '%' || i+1 == len(format) {
			out.WriteByte(c)

			continue
		}

		i++

		pad := byte(0)
		up := false

		for ; i < len(format) && strings.IndexByte("_-0^#", format[i]) >= 0; i++ {
			switch format[i] {
			case '_':
				pad = ' '
			case '-':
				pad = '-'
			case '0':
				pad = '0'
			case '^':
				up = true
			}
		}

		width := 0

		for ; i < len(format) && isDigit(format[i]); i++ {
			width = width*10 + int(format[i]-'0')
		}

		for i < len(format) && (format[i] == 'E' || format[i] == 'O') {
			i++
		}

		if i == len(format) {
			break
		}

		s := conversion(format[i], tm, pad, width)

		if up {
			s = strings.ToUpper(s)
		}

		out.WriteString(s)
	}

	return out.String()
}

func number(v, width int, def, pad byte) string {
	if pad == '-' {
		return strconv.Itoa(v)
	}

	if pad != 0 {
		def = pad
	}

	s := strconv.Itoa(v)

	for len(s) < width {
		s = string(def) + s
	}

	return s
}

func conversion(c byte, tm TmFields, pad byte, width int) string {
	num := func(v, w int, def byte) string {
		return number(v, max(w, width), def, pad)
	}

	hour12 := tm.hour % 12

	if hour12 == 0 {
		hour12 = 12
	}

	isoYear, isoWeek := time.Date(tm.year+1900, time.January, 1+tm.yday, 12, 0, 0, 0, time.UTC).ISOWeek()

	str := func(s string) string {
		for len(s) < width {
			s = " " + s
		}

		return s
	}

	switch c {
	case 'a':
		return str(calendarName(weekdays, tm.wday, true))
	case 'A':
		return str(calendarName(weekdays, tm.wday, false))
	case 'b', 'h':
		return str(calendarName(months, tm.mon, true))
	case 'B':
		return str(calendarName(months, tm.mon, false))
	case 'c':
		return strftime("%a %b %e %H:%M:%S %Y", tm)
	case 'C':
		return num((tm.year+1900)/100, 2, '0')
	case 'd':
		return num(tm.mday, 2, '0')
	case 'D', 'x':
		return strftime("%m/%d/%y", tm)
	case 'e':
		return num(tm.mday, 2, ' ')
	case 'F':
		return strftime("%Y-%m-%d", tm)
	case 'g':
		return num(isoYear%100, 2, '0')
	case 'G':
		return num(isoYear, 1, '0')
	case 'H':
		return num(tm.hour, 2, '0')
	case 'I':
		return num(hour12, 2, '0')
	case 'j':
		return num(tm.yday+1, 3, '0')
	case 'k':
		return num(tm.hour, 2, ' ')
	case 'l':
		return num(hour12, 2, ' ')
	case 'm':
		return num(tm.mon+1, 2, '0')
	case 'M':
		return num(tm.min, 2, '0')
	case 'n':
		return "\n"
	case 'p':
		if tm.hour < 12 {
			return "AM"
		}

		return "PM"
	case 'P':
		if tm.hour < 12 {
			return "am"
		}

		return "pm"
	case 'r':
		return strftime("%I:%M:%S %p", tm)
	case 'R':
		return strftime("%H:%M", tm)
	case 's':
		t := time.Date(tm.year+1900, time.Month(tm.mon+1), tm.mday, tm.hour, tm.min, tm.sec, 0, time.UTC)

		return num(int(t.Unix())-tm.gmtoff, 1, '0')
	case 'S':
		return num(tm.sec, 2, '0')
	case 't':
		return "\t"
	case 'T', 'X':
		return strftime("%H:%M:%S", tm)
	case 'u':
		return num((tm.wday+6)%7+1, 1, '0')
	case 'U':
		return num((tm.yday+7-tm.wday)/7, 2, '0')
	case 'V':
		return num(isoWeek, 2, '0')
	case 'w':
		return num(tm.wday, 1, '0')
	case 'W':
		return num((tm.yday+7-(tm.wday+6)%7)/7, 2, '0')
	case 'y':
		return num((tm.year+1900)%100, 2, '0')
	case 'Y':
		return num(tm.year+1900, 1, '0')
	case 'z':
		off := tm.gmtoff
		sign := '+'

		if off < 0 {
			sign, off = '-', -off
		}

		return fmt.Sprintf("%c%02d%02d", sign, off/3600, off/60%60)
	case 'Z':
		return str(tm.zone)
	case '%':
		return "%"
	}

	return "%" + string(c)
}

func strptime(s []byte, format string, tm *TmFields) (int, bool) {
	at := 0
	century := -1

	space := func() {
		for at < len(s) && (s[at] == ' ' || s[at] >= '\t' && s[at] <= '\r') {
			at++
		}
	}

	num := func(digits, lo, hi int) (int, bool) {
		space()

		sign := 1

		if at < len(s) && (s[at] == '-' || s[at] == '+') && digits > 4 {
			if s[at] == '-' {
				sign = -1
			}

			at++
		}

		start, v := at, 0

		for at < len(s) && at-start < digits && isDigit(s[at]) {
			v = v*10 + int(s[at]-'0')
			at++
		}

		v *= sign

		return v, at > start && v >= lo && v <= hi
	}

	word := func(list []string) (int, bool) {
		space()

		for i, full := range list {
			for _, w := range []string{full, full[:3]} {
				if at+len(w) <= len(s) && strings.EqualFold(string(s[at:at+len(w)]), w) {
					at += len(w)

					return i, true
				}
			}
		}

		return 0, false
	}

	for i := 0; i < len(format); i++ {
		c := format[i]

		if c == ' ' || c >= '\t' && c <= '\r' {
			space()

			continue
		}

		if c != '%' || i+1 == len(format) {
			if at >= len(s) || s[at] != c {
				return 0, false
			}

			at++

			continue
		}

		i++

		for i < len(format) && (format[i] == 'E' || format[i] == 'O') {
			i++
		}

		if i == len(format) {
			return 0, false
		}

		var ok bool
		var v int

		switch format[i] {
		case 'Y':
			v, ok = num(4, 0, 9999)
			tm.year = v - 1900
			century = -1
		case 'y':
			v, ok = num(2, 0, 99)
			tm.year = v

			if v < 69 {
				tm.year += 100
			}
		case 'C':
			v, ok = num(2, 0, 99)
			century = v
		case 'm':
			v, ok = num(2, 1, 12)
			tm.mon = v - 1
		case 'd', 'e':
			v, ok = num(2, 1, 31)
			tm.mday = v
		case 'H', 'k':
			v, ok = num(2, 0, 23)
			tm.hour = v
		case 'I', 'l':
			v, ok = num(2, 1, 12)
			tm.hour = v % 12
		case 'M':
			v, ok = num(2, 0, 59)
			tm.min = v
		case 'S':
			v, ok = num(2, 0, 61)
			tm.sec = v
		case 'j':
			v, ok = num(3, 1, 366)
			tm.yday = v - 1
		case 'b', 'B', 'h':
			v, ok = word(months)
			tm.mon = v
		case 'a', 'A':
			v, ok = word(weekdays)
			tm.wday = v
		case 'p':
			space()
			ok = at+2 <= len(s)

			if ok {
				switch strings.ToUpper(string(s[at : at+2])) {
				case "AM":
				case "PM":
					tm.hour += 12
				default:
					ok = false
				}

				at += 2
			}
		case 's':
			v, ok = num(20, -1<<62, 1<<62)
			*tm = tmOf(time.Unix(int64(v), 0))
		case 'n', 't':
			space()
			ok = true
		case 'D':
			n, good := strptime(s[at:], "%m/%d/%y", tm)

			at, ok = at+n, good
		case 'T':
			n, good := strptime(s[at:], "%H:%M:%S", tm)

			at, ok = at+n, good
		case 'R':
			n, good := strptime(s[at:], "%H:%M", tm)

			at, ok = at+n, good
		case 'F':
			n, good := strptime(s[at:], "%Y-%m-%d", tm)

			at, ok = at+n, good
		case '%':
			ok = at < len(s) && s[at] == '%'
			at++
		}

		if !ok {
			return 0, false
		}
	}

	if century >= 0 {
		tm.year = century*100 - 1900 + (tm.year%100+100)%100
	}

	return at, true
}
