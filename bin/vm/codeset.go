//go:build linux && amd64

package main

import (
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

var unicodeCodesets = map[string]*Codeset{
	"":            {form: utf8Form},
	"char":        {form: utf8Form},
	"utf8":        {form: utf8Form},
	"utf16":       {form: utf16Form, big: true, marked: true},
	"utf16be":     {form: utf16Form, big: true},
	"utf16le":     {form: utf16Form},
	"ucs2":        {form: ucs2Form, big: true},
	"ucs2be":      {form: ucs2Form, big: true},
	"ucs2le":      {form: ucs2Form},
	"utf32":       {form: utf32Form, big: true, marked: true},
	"utf32be":     {form: utf32Form, big: true},
	"utf32le":     {form: utf32Form},
	"ucs4":        {form: utf32Form, big: true},
	"ucs4be":      {form: utf32Form, big: true},
	"ucs4le":      {form: utf32Form},
	"wchart":      {form: utf32Form},
	"ascii":       {ascii: true},
	"usascii":     {ascii: true},
	"ansix341968": {ascii: true},
	"iso646us":    {ascii: true},
}

var singleCodesets = map[string]*charmap.Charmap{
	"iso88591":     charmap.ISO8859_1,
	"latin1":       charmap.ISO8859_1,
	"l1":           charmap.ISO8859_1,
	"iso88592":     charmap.ISO8859_2,
	"latin2":       charmap.ISO8859_2,
	"iso88593":     charmap.ISO8859_3,
	"latin3":       charmap.ISO8859_3,
	"iso88594":     charmap.ISO8859_4,
	"latin4":       charmap.ISO8859_4,
	"iso88595":     charmap.ISO8859_5,
	"cyrillic":     charmap.ISO8859_5,
	"iso88596":     charmap.ISO8859_6,
	"arabic":       charmap.ISO8859_6,
	"iso88597":     charmap.ISO8859_7,
	"greek":        charmap.ISO8859_7,
	"iso88598":     charmap.ISO8859_8,
	"hebrew":       charmap.ISO8859_8,
	"iso88599":     charmap.ISO8859_9,
	"latin5":       charmap.ISO8859_9,
	"iso885910":    charmap.ISO8859_10,
	"latin6":       charmap.ISO8859_10,
	"iso885913":    charmap.ISO8859_13,
	"latin7":       charmap.ISO8859_13,
	"iso885914":    charmap.ISO8859_14,
	"latin8":       charmap.ISO8859_14,
	"iso885915":    charmap.ISO8859_15,
	"latin9":       charmap.ISO8859_15,
	"iso885916":    charmap.ISO8859_16,
	"latin10":      charmap.ISO8859_16,
	"cp874":        charmap.Windows874,
	"windows874":   charmap.Windows874,
	"cp1250":       charmap.Windows1250,
	"windows1250":  charmap.Windows1250,
	"cp1251":       charmap.Windows1251,
	"windows1251":  charmap.Windows1251,
	"cp1252":       charmap.Windows1252,
	"windows1252":  charmap.Windows1252,
	"cp1253":       charmap.Windows1253,
	"windows1253":  charmap.Windows1253,
	"cp1254":       charmap.Windows1254,
	"windows1254":  charmap.Windows1254,
	"cp1255":       charmap.Windows1255,
	"windows1255":  charmap.Windows1255,
	"cp1256":       charmap.Windows1256,
	"windows1256":  charmap.Windows1256,
	"cp1257":       charmap.Windows1257,
	"windows1257":  charmap.Windows1257,
	"cp1258":       charmap.Windows1258,
	"windows1258":  charmap.Windows1258,
	"koi8r":        charmap.KOI8R,
	"koi8u":        charmap.KOI8U,
	"cp437":        charmap.CodePage437,
	"ibm437":       charmap.CodePage437,
	"437":          charmap.CodePage437,
	"cp850":        charmap.CodePage850,
	"ibm850":       charmap.CodePage850,
	"850":          charmap.CodePage850,
	"cp852":        charmap.CodePage852,
	"ibm852":       charmap.CodePage852,
	"cp855":        charmap.CodePage855,
	"ibm855":       charmap.CodePage855,
	"cp858":        charmap.CodePage858,
	"ibm858":       charmap.CodePage858,
	"cp860":        charmap.CodePage860,
	"ibm860":       charmap.CodePage860,
	"cp862":        charmap.CodePage862,
	"ibm862":       charmap.CodePage862,
	"cp863":        charmap.CodePage863,
	"ibm863":       charmap.CodePage863,
	"cp865":        charmap.CodePage865,
	"ibm865":       charmap.CodePage865,
	"cp866":        charmap.CodePage866,
	"ibm866":       charmap.CodePage866,
	"866":          charmap.CodePage866,
	"macintosh":    charmap.Macintosh,
	"macroman":     charmap.Macintosh,
	"mac":          charmap.Macintosh,
	"maccyrillic":  charmap.MacintoshCyrillic,
	"xmaccyrillic": charmap.MacintoshCyrillic,
}

var multiCodesets = map[string]encoding.Encoding{
	"shiftjis":   japanese.ShiftJIS,
	"sjis":       japanese.ShiftJIS,
	"cp932":      japanese.ShiftJIS,
	"mskanji":    japanese.ShiftJIS,
	"windows31j": japanese.ShiftJIS,
	"eucjp":      japanese.EUCJP,
	"iso2022jp":  japanese.ISO2022JP,
	"euckr":      korean.EUCKR,
	"cp949":      korean.EUCKR,
	"ksc5601":    korean.EUCKR,
	"uhc":        korean.EUCKR,
	"gbk":        simplifiedchinese.GBK,
	"cp936":      simplifiedchinese.GBK,
	"gb2312":     simplifiedchinese.GBK,
	"euccn":      simplifiedchinese.GBK,
	"gb18030":    simplifiedchinese.GB18030,
	"hzgb2312":   simplifiedchinese.HZGB2312,
	"hz":         simplifiedchinese.HZGB2312,
	"big5":       traditionalchinese.Big5,
	"big5hkscs":  traditionalchinese.Big5,
	"cp950":      traditionalchinese.Big5,
}

const (
	notUnicode = iota
	utf8Form
	utf16Form
	ucs2Form
	utf32Form
)

type Codeset struct {
	form     int
	big      bool
	marked   bool
	ascii    bool
	single   *charmap.Charmap
	multi    encoding.Encoding
	stateful bool
}

func codesetKey(name string) string {
	var b strings.Builder

	for _, c := range strings.ToLower(name) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}

	return b.String()
}

func codesetNamed(name string) (*Codeset, bool) {
	key := codesetKey(name)

	if c, ok := unicodeCodesets[key]; ok {
		return c, true
	}

	if m, ok := singleCodesets[key]; ok {
		return &Codeset{single: m}, true
	}

	if e, ok := multiCodesets[key]; ok {
		return &Codeset{multi: e, stateful: e == japanese.ISO2022JP || e == simplifiedchinese.HZGB2312}, true
	}

	return nil, false
}
