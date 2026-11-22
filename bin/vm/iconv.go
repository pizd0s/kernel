//go:build linux && amd64

package main

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	stepChar = iota
	stepShift
	stepShort
	stepInvalid
)

const (
	putDone = iota
	putShort
	putNone
)

type Iconv struct {
	from, to                  *Codeset
	translit, ignore, discard bool

	order   int
	shiftIn []byte
	lastOut rune

	dec, enc transform.Transformer
}

func newIconv(to, from string) (*Iconv, bool) {
	toName, suffixes, _ := strings.Cut(to, "//")
	fromName, _, _ := strings.Cut(from, "//")
	c := &Iconv{lastOut: -1}

	if suffixes != "" {
		for _, s := range strings.Split(suffixes, "//") {
			switch strings.ToUpper(s) {
			case "TRANSLIT":
				c.translit = true
			case "IGNORE":
				c.ignore = true
			case "NON_IDENTICAL_DISCARD":
				c.discard = true
			case "":
			default:
				return nil, false
			}
		}
	}

	var ok bool

	if c.to, ok = codesetNamed(toName); !ok {
		return nil, false
	}

	if c.from, ok = codesetNamed(fromName); !ok {
		return nil, false
	}

	return c, true
}

func (c *Iconv) copy() *Iconv {
	copied := *c

	copied.shiftIn, copied.dec, copied.enc = bytes.Clone(c.shiftIn), nil, nil

	return &copied
}

func (c *Iconv) decoder() transform.Transformer {
	if c.dec == nil {
		c.dec = c.from.multi.NewDecoder()

		if len(c.shiftIn) > 0 {
			c.dec.Transform(nil, c.shiftIn, false)
		}
	}

	return c.dec
}

func (c *Iconv) encoder() transform.Transformer {
	if c.enc == nil {
		c.enc = c.to.multi.NewEncoder()

		if c.lastOut >= 0 {
			var scratch [16]byte

			c.enc.Transform(scratch[:], []byte(string(c.lastOut)), false)
		}
	}

	return c.enc
}

func encodesAs(set *Codeset, r rune, b []byte) bool {
	var out [16]byte

	n, _, err := set.multi.NewEncoder().Transform(out[:], []byte(string(r)), true)

	return err == nil && bytes.Equal(out[:n], b)
}

func unit16(b []byte, big bool) rune {
	if big {
		return rune(b[0])<<8 | rune(b[1])
	}

	return rune(b[1])<<8 | rune(b[0])
}

func unit32(b []byte, big bool) rune {
	if big {
		return rune(b[0])<<24 | rune(b[1])<<16 | rune(b[2])<<8 | rune(b[3])
	}

	return rune(b[3])<<24 | rune(b[2])<<16 | rune(b[1])<<8 | rune(b[0])
}

func (c *Iconv) bigInput() bool {
	if c.from.marked {
		return c.order != 2
	}

	return c.from.big
}

func (c *Iconv) mark(src []byte, size int) (int, bool) {
	if !c.from.marked || c.order != 0 {
		return 0, false
	}

	if len(src) < size {
		return 0, true
	}

	if r := unit32(append(make([]byte, 4-size), src[:size]...), true); r == 0xfeff {
		c.order = 1

		return size, true
	}

	if size == 2 && src[0] == 0xff && src[1] == 0xfe || size == 4 && unit32(src, false) == 0xfeff {
		c.order = 2

		return size, true
	}

	c.order = 1

	return 0, false
}

func (c *Iconv) next(src []byte) (rune, int, int) {
	from := c.from

	switch {
	case from.ascii:
		if src[0] >= 0x80 {
			return 0, 1, stepInvalid
		}

		return rune(src[0]), 1, stepChar
	case from.single != nil:
		r := from.single.DecodeByte(src[0])

		if r == utf8.RuneError {
			return 0, 1, stepInvalid
		}

		return r, 1, stepChar
	case from.form == utf8Form:
		if !utf8.FullRune(src) {
			return 0, 0, stepShort
		}

		r, n := utf8.DecodeRune(src)

		if r == utf8.RuneError && n == 1 {
			return 0, 1, stepInvalid
		}

		return r, n, stepChar
	case from.form == utf16Form || from.form == ucs2Form:
		if n, decided := c.mark(src, 2); decided {
			if n == 0 {
				return 0, 0, stepShort
			}

			return 0, n, stepShift
		}

		if len(src) < 2 {
			return 0, 0, stepShort
		}

		r := unit16(src, c.bigInput())

		switch {
		case r < 0xd800 || r > 0xdfff:
			return r, 2, stepChar
		case from.form == ucs2Form || r > 0xdbff:
			return 0, 2, stepInvalid
		case len(src) < 4:
			return 0, 0, stepShort
		}

		low := unit16(src[2:], c.bigInput())

		if low < 0xdc00 || low > 0xdfff {
			return 0, 2, stepInvalid
		}

		return 0x10000 + (r-0xd800)<<10 + (low - 0xdc00), 4, stepChar
	case from.form == utf32Form:
		if n, decided := c.mark(src, 4); decided {
			if n == 0 {
				return 0, 0, stepShort
			}

			return 0, n, stepShift
		}

		if len(src) < 4 {
			return 0, 0, stepShort
		}

		r := unit32(src, c.bigInput())

		if r < 0 || r > unicode.MaxRune || r >= 0xd800 && r <= 0xdfff {
			return 0, 4, stepInvalid
		}

		return r, 4, stepChar
	}

	return c.nextMulti(src)
}

func (c *Iconv) nextMulti(src []byte) (rune, int, int) {
	d := c.decoder()

	var out [utf8.UTFMax]byte

	_, n, err := d.Transform(out[:0], src, false)

	if n > 0 {
		c.shiftIn = append(c.shiftIn[:0], src[:n]...)

		return 0, n, stepShift
	}

	if err == transform.ErrShortSrc {
		return 0, 0, stepShort
	}

	for k := 1; k <= utf8.UTFMax; k++ {
		written, n, err := d.Transform(out[:k], src, false)

		if written > 0 {
			r, _ := utf8.DecodeRune(out[:written])

			if r == utf8.RuneError && !encodesAs(c.from, r, src[:n]) {
				return 0, n, stepInvalid
			}

			return r, n, stepChar
		}

		switch err {
		case transform.ErrShortSrc:
			return 0, 0, stepShort
		case transform.ErrShortDst:
		default:
			return 0, max(n, 1), stepInvalid
		}
	}

	return 0, 1, stepInvalid
}

func (c *Iconv) put(r rune, dst []byte) (int, int) {
	to := c.to
	size := 0

	switch {
	case to.ascii:
		if r >= 0x80 {
			return 0, putNone
		}

		size = 1
	case to.single != nil:
		if _, ok := to.single.EncodeRune(r); !ok {
			return 0, putNone
		}

		size = 1
	case to.form == utf8Form:
		size = utf8.RuneLen(r)
	case to.form == ucs2Form && r > 0xffff:
		return 0, putNone
	case to.form == utf16Form || to.form == ucs2Form:
		size = 2

		if r > 0xffff {
			size = 4
		}
	case to.form == utf32Form:
		size = 4
	default:
		return c.putMulti(r, dst)
	}

	if len(dst) < size {
		return 0, putShort
	}

	switch {
	case to.ascii:
		dst[0] = byte(r)
	case to.single != nil:
		dst[0], _ = to.single.EncodeRune(r)
	case to.form == utf8Form:
		utf8.EncodeRune(dst, r)
	case size == 4 && to.form == utf16Form:
		r -= 0x10000
		putUnit16(dst, 0xd800+r>>10, to.big)
		putUnit16(dst[2:], 0xdc00+r&0x3ff, to.big)
	case size == 2:
		putUnit16(dst, r, to.big)
	default:
		putUnit32(dst, r, to.big)
	}

	return size, putDone
}

func putUnit16(dst []byte, r rune, big bool) {
	if big {
		dst[0], dst[1] = byte(r>>8), byte(r)
	} else {
		dst[0], dst[1] = byte(r), byte(r>>8)
	}
}

func putUnit32(dst []byte, r rune, big bool) {
	if big {
		dst[0], dst[1], dst[2], dst[3] = byte(r>>24), byte(r>>16), byte(r>>8), byte(r)
	} else {
		dst[0], dst[1], dst[2], dst[3] = byte(r), byte(r>>8), byte(r>>16), byte(r>>24)
	}
}

func (c *Iconv) putMulti(r rune, dst []byte) (int, int) {
	n, _, err := c.encoder().Transform(dst, []byte(string(r)), false)

	if err == nil {
		c.lastOut = r

		return n, putDone
	}

	c.enc = nil

	if err == transform.ErrShortDst {
		return 0, putShort
	}

	return 0, putNone
}

func (c *Iconv) transliterate(r rune, dst []byte) (int, int) {
	var plain []rune

	for _, d := range norm.NFKD.String(string(r)) {
		if !unicode.Is(unicode.Mn, d) {
			plain = append(plain, d)
		}
	}

	if len(plain) == 0 || len(plain) == 1 && plain[0] == r {
		return 0, putNone
	}

	last, out := c.lastOut, 0

	for _, d := range plain {
		n, status := c.put(d, dst[out:])

		if status != putDone {
			c.lastOut, c.enc = last, nil

			return 0, status
		}

		out += n
	}

	return out, putDone
}

func (c *Iconv) nonIdentical(r rune, dst []byte) (int, int) {
	if c.translit {
		if n, status := c.transliterate(r, dst); status != putNone {
			return n, status
		}
	}

	if c.ignore || c.discard {
		return 0, putDone
	}

	return c.put('?', dst)
}

func (c *Iconv) convert(src, dst []byte) (int, int, int, Errno) {
	in, out, count := 0, 0, 0

	for in < len(src) {
		r, n, kind := c.next(src[in:])

		switch kind {
		case stepShort:
			return in, out, count, EINVAL
		case stepShift:
			in += n

			continue
		case stepInvalid:
			if !c.ignore {
				return in, out, count, EILSEQ
			}

			in += n

			continue
		}

		m, status := c.put(r, dst[out:])

		if status == putNone {
			if m, status = c.nonIdentical(r, dst[out:]); status == putDone {
				count++
			}
		}

		if status == putShort {
			return in, out, count, E2BIG
		}

		in, out = in+n, out+m
	}

	return in, out, count, 0
}

func (c *Iconv) reset(dst []byte, write bool) (int, Errno) {
	n := 0

	if write && c.to.stateful && c.lastOut >= 0 {
		written, _, err := c.encoder().Transform(dst, nil, true)

		if err != nil {
			c.enc = nil

			return 0, E2BIG
		}

		n = written
	}

	c.order, c.shiftIn, c.lastOut, c.dec, c.enc = 0, nil, -1, nil, nil

	return n, 0
}
