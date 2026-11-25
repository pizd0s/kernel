//go:build ignore

// Command unicode_tables writes lib/libc/unicode.h, the character classes
// and case mappings of libc.wasm's wide characters (lib/libc/wctype.c), of
// Go's unicode package, as the kernel's Go had them:
//
//	go run dev/unicode_tables.go > lib/libc/unicode.h
package main

import (
	"fmt"
	"os"
	"strings"
	"unicode"
)

func noBreakSpace(r rune) bool {
	return r == 0xa0 || r == 0x2007 || r == 0x202f
}

func ranges(name string, in func(rune) bool) string {
	var b strings.Builder
	count := 0

	fmt.Fprintf(&b, "static const struct Range %s[] = {\n", name)

	for r := rune(0); r <= unicode.MaxRune; {
		if !in(r) {
			r++

			continue
		}

		lo := r

		for r <= unicode.MaxRune && in(r) {
			r++
		}

		fmt.Fprintf(&b, "    {0x%x, 0x%x},\n", lo, r-1)
		count++
	}

	fmt.Fprintf(&b, "};\n\n")

	return b.String()
}

func main() {
	w := os.Stdout

	fmt.Fprintf(w, "/*\n * The character classes and case mappings of libc.wasm's wide characters,\n")
	fmt.Fprintf(w, " * of Go's unicode package %s: dev/unicode_tables.go writes them. A class is\n", unicode.Version)
	fmt.Fprintf(w, " * ranges of characters, from the least; a case range maps each character\n")
	fmt.Fprintf(w, " * of it by its deltas to upper and lower case, or, of the delta upperLower,\n")
	fmt.Fprintf(w, " * those of an even offset in it to the next, of an odd one to the one\n")
	fmt.Fprintf(w, " * before.\n */\n#pragma once\n\n#include <stdint.h>\n\n")
	fmt.Fprintf(w, "struct Range {\n    uint32_t lo;\n    uint32_t hi;\n};\n\n")
	fmt.Fprintf(w, "struct CaseRange {\n    uint32_t lo;\n    uint32_t hi;\n    int32_t upper;\n    int32_t lower;\n};\n\n")
	fmt.Fprintf(w, "#define upperLower 0x%x\n\n", unicode.MaxRune+1)
	fmt.Fprint(w, ranges("alphabetic", func(r rune) bool { return unicode.In(r, unicode.L, unicode.Nl, unicode.Other_Alphabetic) }))
	fmt.Fprint(w, ranges("blanks", func(r rune) bool { return unicode.Is(unicode.Zs, r) && !noBreakSpace(r) }))
	fmt.Fprint(w, ranges("spaces", func(r rune) bool { return r != 0 && unicode.Is(unicode.White_Space, r) && !noBreakSpace(r) }))
	fmt.Fprintf(w, "static const struct CaseRange cases[] = {\n")

	for _, c := range unicode.CaseRanges {
		fmt.Fprintf(w, "    {0x%x, 0x%x, %d, %d},\n", c.Lo, c.Hi, c.Delta[unicode.UpperCase], c.Delta[unicode.LowerCase])
	}

	fmt.Fprintf(w, "};\n")
}
