//go:build linux && amd64

package main

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	regExtended = 1
	regIcase    = 2
	regNewline  = 4
	regNosub    = 8
	regNotbol   = 1
	regNoteol   = 2
	regStartend = 4
	regNomatch  = 1
	regBadpat   = 2
	regEcollate = 3
	regEescape  = 5
	regEsubreg  = 6
	regEbrack   = 7
	regEparen   = 8
	regEbrace   = 9
	regBadbr    = 10
	regErange   = 11
	regBadrpt   = 13
)

type PosixRegex struct {
	pattern []byte
	flags   uint32
	nsub    int

	variants [4]*regexp.Regexp
}

func (r *PosixRegex) variant(eflags uint32) *regexp.Regexp {
	v := eflags & (regNotbol | regNoteol)

	if r.variants[v] == nil {
		src, _, _ := translateRegex(r.pattern, r.flags, v&regNotbol != 0, v&regNoteol != 0)

		if r.flags&regNewline != 0 && v != 0 {
			src = "(" + src + ")"

			if v&regNotbol != 0 {
				src = "(?s:.)" + src
			}

			if v&regNoteol != 0 {
				src += "(?s:.)"
			}
		}

		re := regexp.MustCompile(src)

		re.Longest()
		r.variants[v] = re
	}

	return r.variants[v]
}

type RegexWriter struct {
	out      strings.Builder
	atom     int
	repeated bool
	groups   []int
	nsub     int
}

func (w *RegexWriter) atomic(s string) {
	w.atom = w.out.Len()
	w.repeated = false
	w.out.WriteString(s)
}

func (w *RegexWriter) repeat(op string) {
	if w.repeated {
		s := w.out.String()

		w.out.Reset()
		w.out.WriteString(s[:w.atom] + "(?:" + s[w.atom:] + ")")
	}

	w.out.WriteString(op)
	w.repeated = true
}

func quoteByte(c byte) string {
	return regexp.QuoteMeta(string([]byte{c}))
}

func translateRegex(pat []byte, flags uint32, bolNever, eolNever bool) (string, int, uint32) {
	ere := flags&regExtended != 0
	newline := flags&regNewline != 0
	w := &RegexWriter{}

	switch {
	case newline:
		w.out.WriteString("(?m)")
	default:
		w.out.WriteString("(?s)")
	}

	if flags&regIcase != 0 {
		w.out.WriteString("(?i)")
	}

	w.atom = w.out.Len()

	start := true
	never := `[^\x00-\x{10FFFF}]`

	for i := 0; i < len(pat); i++ {
		c := pat[i]
		wasStart := start

		start = false

		switch {
		case c == '\\':
			if i+1 == len(pat) {
				return "", 0, regEescape
			}

			i++

			e := pat[i]

			switch {
			case !ere && e == '(':
				w.groups = append(w.groups, w.out.Len())
				w.nsub++
				w.out.WriteString("(")
				start = true
			case !ere && e == ')':
				if len(w.groups) == 0 {
					return "", 0, regEparen
				}

				w.atom = w.groups[len(w.groups)-1]
				w.groups = w.groups[:len(w.groups)-1]
				w.out.WriteString(")")
				w.repeated = false
			case !ere && e == '|':
				w.out.WriteString("|")
				start = true
			case !ere && e == '{':
				end := bytes.Index(pat[i:], []byte(`\}`))

				if end < 0 {
					return "", 0, regEbrace
				}

				if wasStart {
					return "", 0, regBadrpt
				}

				interval, ok := parseInterval(string(pat[i+1 : i+end]))

				if !ok {
					return "", 0, regBadbr
				}

				w.repeat(interval)
				i += end + 1
			case !ere && (e == '+' || e == '?'):
				if wasStart {
					w.atomic(quoteByte(e))
				} else {
					w.repeat(string(e))
				}
			case e >= '1' && e <= '9':
				return "", 0, regEsubreg
			case e == '<' || e == '>':
				w.atomic(`\b`)
			case e == 'b' || e == 'B' || e == 'w' || e == 'W' || e == 's' || e == 'S':
				w.atomic(`\` + string(e))
			case e == '`':
				w.atomic(`\A`)
			case e == '\'':
				w.atomic(`\z`)
			case e == 'n':
				w.atomic(`\n`)
			case e == 't':
				w.atomic(`\t`)
			default:
				w.atomic(quoteByte(e))
			}
		case c == '[':
			class, n, code := translateBracket(pat[i:], newline)

			if code != 0 {
				return "", 0, code
			}

			w.atomic(class)
			i += n - 1
		case c == '*' || ere && (c == '+' || c == '?'):
			if wasStart {
				w.atomic(quoteByte(c))
			} else {
				w.repeat(string(c))
			}
		case ere && c == '{':
			end := bytes.IndexByte(pat[i:], '}')
			interval, ok := "", false

			if end > 0 {
				interval, ok = parseInterval(string(pat[i+1 : i+end]))
			}

			if !ok || wasStart {
				w.atomic(`\{`)
			} else {
				w.repeat(interval)
				i += end
			}
		case ere && c == '(':
			w.groups = append(w.groups, w.out.Len())
			w.nsub++
			w.out.WriteString("(")
			start = true
		case ere && c == ')':
			if len(w.groups) == 0 {
				w.atomic(`\)`)

				continue
			}

			w.atom = w.groups[len(w.groups)-1]
			w.groups = w.groups[:len(w.groups)-1]
			w.out.WriteString(")")
			w.repeated = false
		case ere && c == '|':
			w.out.WriteString("|")
			start = true
		case c == '^':
			switch {
			case !ere && !wasStart:
				w.atomic(`\^`)
			case bolNever && !newline:
				w.atomic(never)
				start = true
			default:
				w.atomic("^")
				start = true
			}
		case c == '$':
			last := i+1 == len(pat) || !ere && i+2 < len(pat) && pat[i+1] == '\\' && (pat[i+2] == ')' || pat[i+2] == '|')

			switch {
			case !ere && !last:
				w.atomic(`\$`)
			case eolNever && !newline:
				w.atomic(never)
			default:
				w.atomic("$")
			}
		case c == '.':
			w.atomic(".")
		default:
			w.atomic(quoteByte(c))
		}
	}

	if len(w.groups) > 0 {
		return "", 0, regEparen
	}

	return w.out.String(), w.nsub, 0
}

func parseInterval(s string) (string, bool) {
	lo, hi, comma := s, "", false

	if at := strings.IndexByte(s, ','); at >= 0 {
		lo, hi, comma = s[:at], s[at+1:], true
	}

	m, err := strconv.Atoi(lo)

	if err != nil || m > 1000 {
		return "", false
	}

	if !comma {
		return "{" + lo + "}", true
	}

	if hi == "" {
		return "{" + lo + ",}", true
	}

	n, err := strconv.Atoi(hi)

	if err != nil || n < m || n > 1000 {
		return "", false
	}

	return "{" + lo + "," + hi + "}", true
}

func translateBracket(pat []byte, newline bool) (string, int, uint32) {
	var out strings.Builder

	i := 1

	out.WriteString("[")

	if i < len(pat) && pat[i] == '^' {
		out.WriteString("^")
		i++

		if newline {
			out.WriteString(`\n`)
		}
	}

	first := true

	for {
		if i >= len(pat) {
			return "", 0, regEbrack
		}

		c := pat[i]

		if c == ']' && !first {
			break
		}

		first = false

		if c == '[' && i+1 < len(pat) && (pat[i+1] == ':' || pat[i+1] == '=' || pat[i+1] == '.') {
			kind := pat[i+1]
			end := bytes.Index(pat[i+2:], []byte{kind, ']'})

			if end < 0 {
				return "", 0, regEbrack
			}

			body := pat[i+2 : i+2+end]

			if kind == ':' {
				if !validClass(string(body)) {
					return "", 0, 4
				}

				out.WriteString("[:" + string(body) + ":]")
			} else {
				if !utf8.Valid(body) || utf8.RuneCount(body) != 1 {
					return "", 0, regEcollate
				}

				for _, b := range body {
					out.WriteString(classByte(b))
				}
			}

			i += end + 4

			continue
		}

		out.WriteString(classByte(c))
		i++

		if i+1 < len(pat) && pat[i] == '-' && pat[i+1] != ']' {
			out.WriteString("-" + classByte(pat[i+1]))
			i += 2
		}
	}

	out.WriteString("]")

	return out.String(), i + 1, 0
}

func classByte(c byte) string {
	if c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80 {
		return string([]byte{c})
	}

	return `\` + string([]byte{c})
}

func validClass(name string) bool {
	switch name {
	case "alnum", "alpha", "blank", "cntrl", "digit", "graph", "lower", "print", "punct", "space", "upper", "xdigit":
		return true
	}

	return false
}
