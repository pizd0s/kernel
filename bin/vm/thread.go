//go:build linux && amd64

package main

const (
	defaultThreadStack    = 131072
	defaultThreadGuard    = 8192
	defaultThreadStackMax = 8 << 20
	defaultThreadGuardMax = 1 << 20
)

type Thread struct {
	values      [PTHREAD_KEYS_MAX]uint32
	cancelState uint32
	cancelType  uint32
	cancelled   bool
	detached    bool
}
