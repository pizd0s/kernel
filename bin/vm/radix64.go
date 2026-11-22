//go:build linux && amd64

package main

import "strings"

const radix64Digits = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func radix64Value(s []byte) uint32 {
	var v uint32

	for i, c := range s {
		d := strings.IndexByte(radix64Digits, c)

		if d < 0 {
			break
		}

		v |= uint32(d) << (6 * i)
	}

	return v
}

func radix64Text(v uint32) string {
	var b []byte

	for ; v != 0 && len(b) < 6; v >>= 6 {
		b = append(b, radix64Digits[v&63])
	}

	return string(b)
}
