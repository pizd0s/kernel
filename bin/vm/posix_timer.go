package main

import "time"

type PosixTimer struct {
	id       int32
	clock    uint32
	notify   uint32
	signal   int
	value    uint32
	timer    *time.Timer
	epoch    uint64
	end      time.Time
	interval time.Duration
	overrun  int32
	last     int32
}

func (t *PosixTimer) left() time.Duration {
	if t.timer == nil {
		return 0
	}

	return max(time.Until(t.end), time.Nanosecond)
}
