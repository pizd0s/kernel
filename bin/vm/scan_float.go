//go:build linux && amd64

package main

import (
	"math"
	"math/big"
	"strconv"
)

func floatSpace(c byte) bool {
	return c == ' ' || c >= '\t' && c <= '\r'
}

func floatDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func floatHex(c byte) bool {
	return floatDigit(c) || c|0x20 >= 'a' && c|0x20 <= 'f'
}

func floatNamed(s []byte, name string) bool {
	if len(s) < len(name) {
		return false
	}

	for i := range len(name) {
		if s[i]|0x20 != name[i] {
			return false
		}
	}

	return true
}

func floatSign(negative bool) float64 {
	if negative {
		return -1
	}

	return 1
}

func floatDigits(s []byte, at int, digit func(byte) bool) (int, int, bool) {
	count, nonzero := 0, false

	for at < len(s) && digit(s[at]) {
		nonzero = nonzero || s[at] != '0'
		at, count = at+1, count+1
	}

	if at < len(s) && s[at] == '.' {
		at++

		for at < len(s) && digit(s[at]) {
			nonzero = nonzero || s[at] != '0'
			at, count = at+1, count+1
		}
	}

	return at, count, nonzero
}

func floatExponent(s []byte, at int, letter byte) int {
	if at >= len(s) || s[at]|0x20 != letter {
		return at
	}

	k := at + 1

	if k < len(s) && (s[k] == '+' || s[k] == '-') {
		k++
	}

	if k >= len(s) || !floatDigit(s[k]) {
		return at
	}

	for k < len(s) && floatDigit(s[k]) {
		k++
	}

	return k
}

func floatUnderflows(token string, v float64, nonzero bool, bits int) bool {
	least := 0x1p-1022

	if bits == 32 {
		least = 0x1p-126
	}

	if !nonzero || math.Abs(v) >= least {
		return false
	}

	if v == 0 {
		return true
	}

	exact, ok := new(big.Rat).SetString(token)

	return !ok || exact.Cmp(new(big.Rat).SetFloat64(v)) != 0
}

func scanFloat(s []byte, bits int) (float64, int, bool) {
	at := 0

	for at < len(s) && floatSpace(s[at]) {
		at++
	}

	start, negative := at, false

	if at < len(s) && (s[at] == '+' || s[at] == '-') {
		negative = s[at] == '-'
		at++
	}

	rest := s[at:]

	switch {
	case floatNamed(rest, "infinity"):
		return math.Copysign(math.Inf(1), floatSign(negative)), at + 8, false
	case floatNamed(rest, "inf"):
		return math.Copysign(math.Inf(1), floatSign(negative)), at + 3, false
	case floatNamed(rest, "nan"):
		end := at + 3

		if end < len(s) && s[end] == '(' {
			k := end + 1

			for k < len(s) && (floatDigit(s[k]) || s[k] == '_' || s[k]|0x20 >= 'a' && s[k]|0x20 <= 'z') {
				k++
			}

			if k < len(s) && s[k] == ')' {
				end = k + 1
			}
		}

		return math.Copysign(math.NaN(), floatSign(negative)), end, false
	}

	var token string

	end, count, nonzero := 0, 0, false

	if len(rest) > 1 && rest[0] == '0' && rest[1]|0x20 == 'x' {
		end, count, nonzero = floatDigits(s, at+2, floatHex)

		if count > 0 {
			exponent := floatExponent(s, end, 'p')

			token = string(s[start:end]) + "p0"

			if exponent > end {
				token = string(s[start:exponent])
			}

			end = exponent
		}
	}

	if count == 0 {
		end, count, nonzero = floatDigits(s, at, floatDigit)

		if count == 0 {
			return 0, 0, false
		}

		end = floatExponent(s, end, 'e')
		token = string(s[start:end])
	}

	v, err := strconv.ParseFloat(token, bits)

	return v, end, err != nil || floatUnderflows(token, v, nonzero, bits)
}

func scanWideFloat(s []uint32, bits int) (float64, int, bool) {
	at := 0

	for at < len(s) && iswClass(wcSpace, s[at]) {
		at++
	}

	narrow := make([]byte, len(s)-at)

	for i, c := range s[at:] {
		narrow[i] = 0xff

		if c < 0x80 {
			narrow[i] = byte(c)
		}
	}

	v, n, erange := scanFloat(narrow, bits)

	if n == 0 {
		return v, 0, false
	}

	return v, at + n, erange
}
