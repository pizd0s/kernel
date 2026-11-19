//go:build linux && amd64

package main

// setjmp and longjmp, as an embedder builds them: the import that returns
// twice takes the jump frame its adapter wrote (abi.go, goabi.go) into a
// Jump; longjmp returns from that call again through the land stub
// (stubs_amd64.s), which checks that the frame is still on the stack. The
// Go frames between the stub's caller and the frame are dropped, not
// unwound: they must hold no defers and no locks.

// Jump is a frame to return to: the jump frame as the adapter wrote it,
// then the code of its function, the land stub's to read.
type Jump struct {
	frame [jumpWords]uint64
	code  [2]uint64
}

// sp is the frame's stack pointer as an offset below the stack's top: a
// frame deeper in the stack has a larger one.
func (j *Jump) sp() uint64 {
	return j.frame[jumpSP]
}
