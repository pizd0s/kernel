// Seats, where a goroutine of vm's kernel (bin/vm) sleeps until another
// calls it: one word, 0 when nobody sits and nobody called, seatCalled
// when a call came before the sitter, else the sitter's g. A sitter parks
// and a call readies it as a channel's would (the next to run on the
// caller's P), without a channel's lock, sudog or copy. A sitter that
// parks runs its P's next goroutine, the one it readied last, at once
// on its M, without the search of schedule, when nothing schedule does
// first is due.

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

	gp := getg()

	if gp.lockedm != 0 || gp.bubble != nil {
		gopark(seatPark, unsafe.Pointer(s), waitReasonSeat, traceBlockSync, 1)

		return
	}

	mp := acquirem()
	mp.waitlock = unsafe.Pointer(s)
	gp.waitreason = waitReasonSeat
	mp.waitTraceBlockReason = traceBlockSync
	mp.waitTraceSkip = 1
	releasem(mp)
	mcall(seatSit)
}

// seatSit is park_m with seatPark that goes on with seatNext.
func seatSit(gp *g) {
	mp := getg().m
	seat := mp.waitlock
	mp.waitlock = nil
	trace := traceAcquire()

	if trace.ok() {
		trace.GoPark(mp.waitTraceBlockReason, mp.waitTraceSkip)
	}

	casgstatus(gp, _Grunning, _Gwaiting)

	if trace.ok() {
		traceRelease(trace)
	}

	dropg()

	if !seatPark(gp, seat) {
		trace := traceAcquire()
		casgstatus(gp, _Gwaiting, _Grunnable)

		if trace.ok() {
			trace.GoUnpark(gp, 2)
			traceRelease(trace)
		}

		execute(gp, true)
	}

	if next := seatNext(mp.p.ptr()); next != nil {
		execute(next, true)
	}

	schedule()
}

// seatNext takes pp's next goroutine when schedule would run it at once:
// nothing of findRunnable's before its run queue is due (a stop of the
// world, a safe point function, a preemption, a timer, the tracer, GC
// workers, the global queue's turn, finalizers, cleanups) and it is not
// locked to a thread.
func seatNext(pp *p) *g {
	if sched.gcwaiting.Load() || pp.runSafePointFn != 0 || pp.preempt || gcBlackenEnabled != 0 ||
		traceEnabled() || traceShuttingDown() || pp.schedtick%61 == 0 && !sched.runq.empty() ||
		fingStatus.Load()&(fingWait|fingWake) == fingWait|fingWake || gcCleanups.needsWake() {
		return nil
	}

	if when := pp.timers.wakeTime(); when != 0 && when <= nanotime() {
		return nil
	}

	next := pp.runnext

	if next == 0 || next.ptr().lockedm != 0 || !pp.runnext.cas(next, 0) {
		return nil
	}

	return next.ptr()
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
