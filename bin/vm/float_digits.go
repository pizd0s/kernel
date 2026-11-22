//go:build linux && amd64

package main

import (
	"math"
	"strconv"
	"strings"
)

func digitsOfExponential(text string) (string, int) {
	mantissa, exponent, _ := strings.Cut(text, "e")
	power, _ := strconv.Atoi(exponent)

	return strings.TrimRight(strings.Replace(mantissa, ".", "", 1), "0"), power + 1
}

func shortestDigits(v float64) (string, int) {
	return digitsOfExponential(strconv.FormatFloat(v, 'e', -1, 64))
}

func significantDigits(v float64, n int) (string, int) {
	return digitsOfExponential(strconv.FormatFloat(v, 'e', max(n, 1)-1, 64))
}

func roundedDigits(digits string, point, keep int) (string, int) {
	if keep >= len(digits) {
		return digits, point
	}

	if keep < 0 || keep == 0 && (digits[0] < '5' || digits[0] == '5' && len(digits) == 1) {
		return "", point - keep
	}

	if keep == 0 {
		return "1", point + 1
	}

	rest := digits[keep:]
	kept := []byte(digits[:keep])

	if rest[0] > '5' || rest[0] == '5' && (len(rest) > 1 || kept[keep-1]&1 == 1) {
		i := keep - 1

		for i >= 0 && kept[i] == '9' {
			kept[i] = '0'
			i--
		}

		if i < 0 {
			return "1", point + 1
		}

		kept[i]++
	}

	return strings.TrimRight(string(kept), "0"), point
}

func fixedDigits(v float64, n int) (string, int) {
	if n < 0 {
		digits, point := digitsOfExponential(strconv.FormatFloat(v, 'e', 767, 64))

		return roundedDigits(digits, point, point+n)
	}

	whole, fraction, _ := strings.Cut(strconv.FormatFloat(v, 'f', n, 64), ".")
	all := whole + fraction
	significant := strings.TrimLeft(all, "0")
	digits := strings.TrimRight(significant, "0")

	if digits == "" {
		return "", -n
	}

	return digits, len(whole) - (len(all) - len(significant))
}

func decimalDigits(v float64, mode, n int) (string, int) {
	if mode > 5 && mode <= 9 {
		mode -= 4
	}

	switch mode {
	case 2:
		return significantDigits(v, n)
	case 3:
		return fixedDigits(v, n)
	case 4:
		if digits, point := shortestDigits(v); len(digits) <= max(n, 1) {
			return digits, point
		}

		return significantDigits(v, n)
	case 5:
		if digits, point := shortestDigits(v); len(digits)-point <= n {
			return digits, point
		}

		return fixedDigits(v, n)
	}

	return shortestDigits(v)
}

func shortFloatText(v float64, n int) string {
	sign := ""

	if math.Signbit(v) {
		sign, v = "-", -v
	}

	switch {
	case math.IsNaN(v):
		return sign + "nan"
	case math.IsInf(v, 0):
		return sign + "inf"
	case v == 0:
		return sign + "0"
	}

	if n < 0 {
		n = 6
	}

	n = min(max(n, 1), 17)

	digits, point := significantDigits(v, n)

	if point-1 < -4 || point-1 >= n {
		mantissa := digits[:1]

		if len(digits) > 1 {
			mantissa += "." + digits[1:]
		}

		power, minus := point-1, "+"

		if power < 0 {
			power, minus = -power, "-"
		}

		exponent := strconv.Itoa(power)

		if power < 10 {
			exponent = "0" + exponent
		}

		return sign + mantissa + "e" + minus + exponent
	}

	switch {
	case point <= 0:
		return sign + "0." + strings.Repeat("0", -point) + digits
	case point >= len(digits):
		return sign + digits + strings.Repeat("0", point-len(digits))
	}

	return sign + digits[:point] + "." + digits[point:]
}

func hexDigits(v float64, n int) ([]byte, int) {
	bits := math.Float64bits(v)
	exponent := int(bits >> 52 & 0x7ff)
	mantissa := bits & (1<<52 - 1)

	if exponent == 0 {
		exponent = 1

		for mantissa < 1<<52 {
			mantissa <<= 1
			exponent--
		}
	} else {
		mantissa |= 1 << 52
	}

	digits := make([]byte, 14)

	for i := range digits {
		digits[i] = byte(mantissa >> (52 - 4*i) & 0xf)
	}

	if n == 0 {
		n = 1
	}

	if n < 0 {
		n = len(digits)

		for n > 1 && digits[n-1] == 0 {
			n--
		}
	}

	if n < len(digits) {
		rest := digits[n:]
		above := rest[0] > 8 || rest[0] == 8 && strings.Trim(string(rest[1:]), "\x00") != ""

		if above || rest[0] == 8 && digits[n-1]&1 == 1 {
			i := n - 1

			for digits[i] == 0xf {
				digits[i] = 0
				i--
			}

			digits[i]++
		}

		digits = digits[:n]
	}

	for len(digits) < n {
		digits = append(digits, 0)
	}

	return digits, exponent - 1022
}
