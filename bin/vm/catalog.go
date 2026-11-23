//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/binary"
	"sort"
	"strings"
)

const (
	catalogMagic  = 0xff88ff89
	catalogHeader = 20
	catalogEntry  = 12
	catalogMax    = 1 << 24
	nlsDefault    = "/usr/share/nls/%L/%N.cat:/usr/share/nls/%N/%L"
)

func catalogValid(b []byte) bool {
	be := binary.BigEndian

	if len(b) < catalogHeader || be.Uint32(b) != catalogMagic || uint64(be.Uint32(b[8:]))+catalogHeader != uint64(len(b)) {
		return false
	}

	sets := uint64(be.Uint32(b[4:]))
	msgs, text := catalogHeader+uint64(be.Uint32(b[12:])), catalogHeader+uint64(be.Uint32(b[16:]))

	if catalogHeader+sets*catalogEntry > msgs || msgs > text || text > uint64(len(b)) {
		return false
	}

	count := (text - msgs) / catalogEntry

	for i := range sets {
		s := b[catalogHeader+catalogEntry*i:]

		if uint64(be.Uint32(s[8:]))+uint64(be.Uint32(s[4:])) > count {
			return false
		}
	}

	for i := range count {
		at := text + uint64(be.Uint32(b[msgs+catalogEntry*i+8:]))

		if at >= uint64(len(b)) || bytes.IndexByte(b[at:], 0) < 0 {
			return false
		}
	}

	return true
}

func catalogFind(b []byte, set, msg int32) (int, bool) {
	be := binary.BigEndian

	entry := func(at, i int) (int32, []byte, bool) {
		if at < 0 || i < 0 || at+catalogEntry*(i+1) > len(b) {
			return 0, nil, false
		}

		e := b[at+catalogEntry*i:]

		return int32(be.Uint32(e)), e, true
	}

	sets := int(be.Uint32(b[4:]))
	msgs, text := catalogHeader+int(be.Uint32(b[12:])), catalogHeader+int(be.Uint32(b[16:]))

	i := sort.Search(sets, func(i int) bool {
		n, _, ok := entry(catalogHeader, i)

		return !ok || n >= set
	})

	n, s, ok := entry(catalogHeader, i)

	if !ok || n != set {
		return 0, false
	}

	count, at := int(be.Uint32(s[4:])), msgs+catalogEntry*int(be.Uint32(s[8:]))

	k := sort.Search(count, func(k int) bool {
		n, _, ok := entry(at, k)

		return !ok || n >= msg
	})

	n, m, ok := entry(at, k)

	if !ok || n != msg || k >= count {
		return 0, false
	}

	off := text + int(be.Uint32(m[8:]))

	if off < text || off >= len(b) || bytes.IndexByte(b[off:], 0) < 0 {
		return 0, false
	}

	return off, true
}

func nlsExpand(template, name, lang string) (string, bool) {
	if template == "" {
		return name, true
	}

	var b strings.Builder

	for i := 0; i < len(template); i++ {
		if template[i] != '%' {
			b.WriteByte(template[i])

			continue
		}

		if i++; i == len(template) {
			return "", false
		}

		switch template[i] {
		case 'N':
			b.WriteString(name)
		case 'L':
			b.WriteString(lang)
		case 'l':
			if k := strings.IndexAny(lang, "_.@"); k >= 0 {
				b.WriteString(lang[:k])
			} else {
				b.WriteString(lang)
			}
		case 't':
			if _, t, ok := strings.Cut(lang, "_"); ok {
				if k := strings.IndexAny(t, ".@"); k >= 0 {
					t = t[:k]
				}

				b.WriteString(t)
			}
		case 'c':
			b.WriteString("UTF-8")
		case '%':
			b.WriteByte('%')
		default:
			return "", false
		}
	}

	return b.String(), true
}
