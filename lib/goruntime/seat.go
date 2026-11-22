// Seats, where a goroutine of vm's kernel (bin/vm) sleeps until another
// calls it: one word, 0 when nobody sits and nobody called, seatCalled
// when a call came before the sitter, else the sitter's g. A sitter parks
// and a call readies it as a channel's would (the next to run on the
// caller's P), without a channel's lock, sudog or copy.

package runtime

import (
	"internal/runtime/atomic"
	"unsafe"
)

const seatCalled = 1

// A Seat is where one goroutine at a time sleeps.
type Seat struct {
	v atomic.Uintptr
}

// Sit parks the calling goroutine until a Ring of s, or returns at once,
// taking the call, when one came before.
func (s *Seat) Sit() {
	if s.v.CompareAndSwap(seatCalled, 0) {
		return
	}

	gopark(seatPark, unsafe.Pointer(s), waitReasonSeat, traceBlockSync, 1)
}

// seatPark puts the parking gp on its seat, or takes the call that came
// first and resumes it.
func seatPark(gp *g, seat unsafe.Pointer) bool {
	s := (*Seat)(seat)

	for {
		if s.v.CompareAndSwap(0, uintptr(unsafe.Pointer(gp))) {
			return true
		}

		if s.v.CompareAndSwap(seatCalled, 0) {
			return false
		}
	}
}

// Ring readies the goroutine sitting on s, or leaves the call for the next
// Sit.
func (s *Seat) Ring() {
	for {
		switch v := s.v.Load(); v {
		case seatCalled:
			return
		case 0:
			if s.v.CompareAndSwap(0, seatCalled) {
				return
			}
		default:
			if s.v.CompareAndSwap(v, 0) {
				goready((*g)(unsafe.Pointer(v)), 1)

				return
			}
		}
	}
}
