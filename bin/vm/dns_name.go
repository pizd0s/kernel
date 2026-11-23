//go:build linux && amd64

package main

import (
	"fmt"
	"strings"
)

func dnsWire(name string) ([][]byte, bool) {
	if name == "." {
		return nil, true
	}

	var labels [][]byte
	var label []byte

	total := 1

	end := func() bool {
		if len(label) == 0 || len(label) > NS_MAXLABEL {
			return false
		}

		labels = append(labels, label)
		total += len(label) + 1
		label = nil

		return total <= NS_MAXCDNAME
	}

	for i := 0; i < len(name); i++ {
		c := name[i]

		switch {
		case c == '\\' && i+3 < len(name) && dnsDigits(name[i+1:i+4]):
			v := int(name[i+1]-'0')*100 + int(name[i+2]-'0')*10 + int(name[i+3]-'0')

			if v > 255 {
				return nil, false
			}

			label = append(label, byte(v))
			i += 3
		case c == '\\':
			if i+1 == len(name) {
				return nil, false
			}

			label = append(label, name[i+1])
			i++
		case c == '.':
			if !end() {
				return nil, false
			}
		default:
			label = append(label, c)
		}
	}

	if len(label) > 0 && !end() {
		return nil, false
	}

	return labels, true
}

func dnsDigits(s string) bool {
	return strings.Trim(s, "0123456789") == ""
}

func dnsLabels(msg []byte, at int) ([][]byte, int, bool) {
	var labels [][]byte

	used, total := 0, 1

	for jumps, start := 0, at; ; {
		if at < 0 || at >= len(msg) {
			return nil, 0, false
		}

		n := int(msg[at])

		switch {
		case n == 0:
			if used == 0 {
				used = at + 1 - start
			}

			return labels, used, true
		case n&NS_CMPRSFLGS == NS_CMPRSFLGS:
			if at+1 >= len(msg) || jumps > len(msg) {
				return nil, 0, false
			}

			if used == 0 {
				used = at + 2 - start
			}

			jumps++
			at = (n&^NS_CMPRSFLGS)<<8 | int(msg[at+1])
		case n&NS_CMPRSFLGS != 0 || at+1+n > len(msg):
			return nil, 0, false
		default:
			if total += n + 1; total > NS_MAXCDNAME {
				return nil, 0, false
			}

			labels = append(labels, msg[at+1:at+1+n])
			at += 1 + n
		}
	}
}

func dnsSkip(msg []byte, at int) (int, bool) {
	for at < len(msg) {
		n := int(msg[at])

		switch {
		case n == 0:
			return at + 1, true
		case n&NS_CMPRSFLGS == NS_CMPRSFLGS:
			return at + 2, at+2 <= len(msg)
		case n&NS_CMPRSFLGS != 0:
			return 0, false
		}

		at += 1 + n
	}

	return 0, false
}

func dnsSkipRecords(msg []byte, at int, question bool, count int) (int, bool) {
	for range count {
		end, ok := dnsSkip(msg, at)

		if !ok {
			return 0, false
		}

		at = end + NS_QFIXEDSZ

		if !question {
			if at = end + NS_RRFIXEDSZ; at > len(msg) {
				return 0, false
			}

			at += int(msg[at-2])<<8 | int(msg[at-1])
		}

		if at > len(msg) {
			return 0, false
		}
	}

	return at, true
}

func dnsText(labels [][]byte, root string) string {
	if len(labels) == 0 {
		return root
	}

	var b strings.Builder

	for i, label := range labels {
		if i > 0 {
			b.WriteByte('.')
		}

		for _, c := range label {
			switch {
			case strings.IndexByte(".;\\()\"@$", c) >= 0:
				b.WriteByte('\\')
				b.WriteByte(c)
			case c <= ' ' || c >= 0x7f:
				fmt.Fprintf(&b, "\\%03d", c)
			default:
				b.WriteByte(c)
			}
		}
	}

	return b.String()
}

func dnsSame(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		x, y := a[i], b[i]

		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}

		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}

		if x != y {
			return false
		}
	}

	return true
}

func dnsFind(msg []byte, names []int, labels [][]byte) int {
	for _, at := range names {
		for jumps := 0; at >= 0 && at < len(msg) && jumps <= len(msg); {
			n := int(msg[at])

			if n&NS_CMPRSFLGS == NS_CMPRSFLGS {
				if at+1 >= len(msg) {
					break
				}

				jumps++
				at = (n&^NS_CMPRSFLGS)<<8 | int(msg[at+1])

				continue
			}

			if n == 0 || n&NS_CMPRSFLGS != 0 {
				break
			}

			if found, _, ok := dnsLabels(msg, at); ok && len(found) == len(labels) {
				same := true

				for i := range labels {
					same = same && dnsSame(found[i], labels[i])
				}

				if same {
					return at
				}
			}

			at += 1 + n
		}
	}

	return -1
}
