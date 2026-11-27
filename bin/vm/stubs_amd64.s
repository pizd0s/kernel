#include "textflag.h"
#include "funcdata.h"

// The stubs the code calls (symbols.go). Their arguments come in registers
// as Go's ABIInternal places them, and the code keeps the memory's bound
// in R13 and its base in R15 across them; the instance's block lies below
// the base, the module's distance, the word just below the base its
// address. g.instance holds the Instance of Go's (instance.go), which does
// not move: its first words are the memory's base, the bound and the
// block's address, which a move rewrites. R14 is g. Go never preempts
// assembly. Offsets: g.stack.hi 8, g.stackguard0 16, g.instance 472
// (lib/goruntime); the Instance's base 0, bound 8, block 16; the block's
// stack limit 24, jump frame 48, base 120 (abi.go). The stack moves by PUSH, POP
// and ADJSP, which the assembler puts in the stack pointer table: Go
// unwinds through these frames when it grows the stack. A function that
// writes the stack pointer cannot be unwound through, so none of these
// calls anything after that.

// vmMorestack is called by a stack check that failed, with what the check
// compared pushed. The preemption the check may have seen can be done by
// now: the stub compares that once more with g.stackguard0 and grows the
// stack only when it is still not above. Past the instance's stack limit
// it traps instead. Every register comes back as it was.
TEXT ·vmMorestack(SB), NOSPLIT|NOFRAME, $0-0
	NO_LOCAL_POINTERS
	PUSHQ	BP
	MOVQ	SP, BP
	PUSHQ	AX
	PUSHQ	BX
	PUSHQ	CX
	PUSHQ	DX
	PUSHQ	SI
	PUSHQ	DI
	PUSHQ	R8
	PUSHQ	R9
	PUSHQ	R10
	PUSHQ	R11
	PUSHQ	R12
	PUSHQ	R13
	PUSHQ	R15
	ADJSP	$120
	MOVSD	X0, 0(SP)
	MOVSD	X1, 8(SP)
	MOVSD	X2, 16(SP)
	MOVSD	X3, 24(SP)
	MOVSD	X4, 32(SP)
	MOVSD	X5, 40(SP)
	MOVSD	X6, 48(SP)
	MOVSD	X7, 56(SP)
	MOVSD	X8, 64(SP)
	MOVSD	X9, 72(SP)
	MOVSD	X10, 80(SP)
	MOVSD	X11, 88(SP)
	MOVSD	X12, 96(SP)
	MOVSD	X13, 104(SP)
	MOVSD	X14, 112(SP)
	MOVQ	16(BP), AX
	CMPQ	AX, 16(R14)
	JA	fits
	MOVQ	8(R14), AX
	SUBQ	SP, AX
	MOVQ	-8(R15), BX
	CMPQ	AX, 24(BX)
	JBE	grow
	// Past the limit: the trap. The assembler's stack pointer table follows
	// the text, so this comes before the registers come back, and the
	// adjustment after the trap, never run, keeps the table right.
	ADJSP	$16
	MOVL	$9, 0(SP)
	CALL	·helperTrap(SB)
	UD2
	ADJSP	$-16
grow:
	CALL	runtime·morestack_noctxt(SB)
fits:
	MOVSD	0(SP), X0
	MOVSD	8(SP), X1
	MOVSD	16(SP), X2
	MOVSD	24(SP), X3
	MOVSD	32(SP), X4
	MOVSD	40(SP), X5
	MOVSD	48(SP), X6
	MOVSD	56(SP), X7
	MOVSD	64(SP), X8
	MOVSD	72(SP), X9
	MOVSD	80(SP), X10
	MOVSD	88(SP), X11
	MOVSD	96(SP), X12
	MOVSD	104(SP), X13
	MOVSD	112(SP), X14
	ADJSP	$-120
	POPQ	R15
	POPQ	R13
	POPQ	R12
	POPQ	R11
	POPQ	R10
	POPQ	R9
	POPQ	R8
	POPQ	DI
	POPQ	SI
	POPQ	DX
	POPQ	CX
	POPQ	BX
	POPQ	AX
	POPQ	BP
	RET

// vmInterruptCall is the call Go's runtime injects where it interrupts the
// code (lib/goruntime), and the call a return from an import makes while
// the instance's interrupt word is set. It calls helperInterrupt, which
// finds the instance in g.instance. A handler may move the memory, and the
// block with it. Where the code may be interrupted, only R15 and R13 hold
// what a move changes: R13 the bound, R15 the base, but just after a call
// of Go, where they are read again from the Instance (objCodeReloadAfterGo),
// R15 holding the Instance meanwhile, which no address in the memory
// equals. The stub comes back with every register and flag as it was, but
// R13, the new bound, and R15 where it was the old base: the new base.
TEXT ·vmInterruptCall(SB), NOSPLIT|NOFRAME, $0-0
	NO_LOCAL_POINTERS
	PUSHQ	BP
	MOVQ	SP, BP
	PUSHFQ
	PUSHQ	AX
	PUSHQ	BX
	PUSHQ	CX
	PUSHQ	DX
	PUSHQ	SI
	PUSHQ	DI
	PUSHQ	R8
	PUSHQ	R9
	PUSHQ	R10
	PUSHQ	R11
	PUSHQ	R12
	PUSHQ	R13
	ADJSP	$144
	MOVSD	X0, 16(SP)
	MOVSD	X1, 24(SP)
	MOVSD	X2, 32(SP)
	MOVSD	X3, 40(SP)
	MOVSD	X4, 48(SP)
	MOVSD	X5, 56(SP)
	MOVSD	X6, 64(SP)
	MOVSD	X7, 72(SP)
	MOVSD	X8, 80(SP)
	MOVSD	X9, 88(SP)
	MOVSD	X10, 96(SP)
	MOVSD	X11, 104(SP)
	MOVSD	X12, 112(SP)
	MOVSD	X13, 120(SP)
	MOVSD	X14, 128(SP)
	MOVQ	R15, 8(SP)
	MOVQ	472(R14), AX
	MOVQ	0(AX), BX
	MOVQ	BX, 0(SP)
	CALL	·helperInterrupt(SB)
	MOVQ	472(R14), AX
	MOVQ	8(AX), CX
	MOVQ	CX, 144(SP)
	MOVQ	8(SP), R15
	CMPQ	R15, 0(SP)
	JNE	based
	MOVQ	0(AX), R15
based:
	MOVSD	16(SP), X0
	MOVSD	24(SP), X1
	MOVSD	32(SP), X2
	MOVSD	40(SP), X3
	MOVSD	48(SP), X4
	MOVSD	56(SP), X5
	MOVSD	64(SP), X6
	MOVSD	72(SP), X7
	MOVSD	80(SP), X8
	MOVSD	88(SP), X9
	MOVSD	96(SP), X10
	MOVSD	104(SP), X11
	MOVSD	112(SP), X12
	MOVSD	120(SP), X13
	MOVSD	128(SP), X14
	ADJSP	$-144
	POPQ	R13
	POPQ	R12
	POPQ	R11
	POPQ	R10
	POPQ	R9
	POPQ	R8
	POPQ	DI
	POPQ	SI
	POPQ	DX
	POPQ	CX
	POPQ	BX
	POPQ	AX
	POPFQ
	POPQ	BP
	RET

// vmStackGrow is called by a frame of the memory's stack that does not
// fit below the stack pointer, with the stack pointer and the bytes the
// frame takes pushed: it puts in place of the stack pointer the one the
// frame fits below (Process.stackGrow). Every register comes back as it
// was, but the bound and the base, which follow a move of the memory as
// in vmInterruptCall.
TEXT ·vmStackGrow(SB), NOSPLIT|NOFRAME, $0-0
	NO_LOCAL_POINTERS
	PUSHQ	BP
	MOVQ	SP, BP
	PUSHFQ
	PUSHQ	AX
	PUSHQ	BX
	PUSHQ	CX
	PUSHQ	DX
	PUSHQ	SI
	PUSHQ	DI
	PUSHQ	R8
	PUSHQ	R9
	PUSHQ	R10
	PUSHQ	R11
	PUSHQ	R12
	PUSHQ	R13
	ADJSP	$160
	MOVSD	X0, 40(SP)
	MOVSD	X1, 48(SP)
	MOVSD	X2, 56(SP)
	MOVSD	X3, 64(SP)
	MOVSD	X4, 72(SP)
	MOVSD	X5, 80(SP)
	MOVSD	X6, 88(SP)
	MOVSD	X7, 96(SP)
	MOVSD	X8, 104(SP)
	MOVSD	X9, 112(SP)
	MOVSD	X10, 120(SP)
	MOVSD	X11, 128(SP)
	MOVSD	X12, 136(SP)
	MOVSD	X13, 144(SP)
	MOVSD	X14, 152(SP)
	MOVQ	R15, 24(SP)
	MOVQ	472(R14), AX
	MOVQ	0(AX), BX
	MOVQ	BX, 32(SP)
	MOVL	16(BP), AX
	MOVQ	AX, 0(SP)
	MOVQ	24(BP), AX
	MOVQ	AX, 8(SP)
	CALL	·helperStackGrow(SB)
	MOVL	16(SP), AX
	MOVQ	AX, 16(BP)
	MOVQ	472(R14), AX
	MOVQ	8(AX), CX
	MOVQ	CX, 160(SP)
	MOVQ	24(SP), R15
	CMPQ	R15, 32(SP)
	JNE	based
	MOVQ	0(AX), R15
based:
	MOVSD	40(SP), X0
	MOVSD	48(SP), X1
	MOVSD	56(SP), X2
	MOVSD	64(SP), X3
	MOVSD	72(SP), X4
	MOVSD	80(SP), X5
	MOVSD	88(SP), X6
	MOVSD	96(SP), X7
	MOVSD	104(SP), X8
	MOVSD	112(SP), X9
	MOVSD	120(SP), X10
	MOVSD	128(SP), X11
	MOVSD	136(SP), X12
	MOVSD	144(SP), X13
	MOVSD	152(SP), X14
	ADJSP	$-160
	POPQ	R13
	POPQ	R12
	POPQ	R11
	POPQ	R10
	POPQ	R9
	POPQ	R8
	POPQ	DI
	POPQ	SI
	POPQ	DX
	POPQ	CX
	POPQ	BX
	POPQ	AX
	POPFQ
	POPQ	BP
	RET

// vmLand is what longjmp calls, func(jump uintptr, val uint32, inst
// uintptr) uint32 with AX, BX and CX: inst is the Instance; jump is a Jump
// (jump.go), its frame,
// then the code of the caller's function, [start, end). It returns from
// the call that wrote the frame again, with val, when the caller's RBP is
// on the chain of frame pointers from its own caller, the frame below
// returns into that code and the token is the frame's: the invocation is
// the one that made the call. Else it returns 0. The switch is a ret.
TEXT ·vmLand(SB), NOSPLIT|NOFRAME, $0-0
	NO_LOCAL_POINTERS
	MOVQ	8(R14), DX
	MOVQ	DX, R9
	MOVQ	16(CX), R10
	SUBQ	120(R10), R9
	MOVQ	R9, SI
	SUBQ	16(AX), SI
	MOVQ	BP, DI
walk:
	MOVQ	(DI), R8
	CMPQ	R8, SI
	JEQ	found
	CMPQ	R8, DI
	JBE	fail
	CMPQ	R8, DX
	JAE	fail
	MOVQ	R8, DI
	JMP	walk
found:
	MOVQ	8(DI), R8
	CMPQ	R8, 72(AX)
	JB	fail
	CMPQ	R8, 80(AX)
	JAE	fail
	MOVQ	-8(SI), R8
	CMPQ	R8, 64(AX)
	JNE	fail
	MOVQ	CX, 472(R14)
	MOVQ	8(CX), R13
	MOVQ	(CX), R15
	MOVQ	R9, R8
	SUBQ	8(AX), R8
	SUBQ	$8, R8
	MOVQ	0(AX), DX
	MOVQ	DX, (R8)
	MOVQ	SI, BP
	// val leaves BX before the callee-saved registers, BX among them, come
	// back from the frame, which AX still addresses.
	MOVL	BX, DX
	MOVQ	24(AX), BX
	MOVQ	32(AX), R9
	MOVQ	40(AX), R10
	MOVQ	48(AX), R11
	MOVQ	56(AX), R12
	MOVL	DX, AX
	MOVQ	R8, SP
	RET
fail:
	XORL	AX, AX
	RET

// vmFrames is what fork calls first, func(request uintptr) with AX, on a
// FramesRequest (instance.go): it walks the frame pointers from the jump
// frame's caller up while each returns into a function of the module, to
// the frame that returns into the entry thunk or the resume stub's
// continuation, and copies those frames, from the caller's stack pointer
// to that return address, when they fit the buffer. In the copy each saved
// RBP is an offset from its start, the top one 0. A leaf that only reads
// the stack.
TEXT ·vmFrames(SB), NOSPLIT|NOFRAME, $0-0
	NO_LOCAL_POINTERS
	MOVQ	8(R14), DX
	MOVQ	0(AX), R10
	MOVQ	DX, R9
	SUBQ	120(R10), R9
	MOVQ	R9, SI
	SUBQ	8(AX), SI
	MOVQ	R9, DI
	SUBQ	16(AX), DI
walk:
	MOVQ	8(DI), R8
	CMPQ	R8, 40(AX)
	JB	notentry
	CMPQ	R8, 48(AX)
	JB	top
notentry:
	CMPQ	R8, 56(AX)
	JB	notresume
	CMPQ	R8, 64(AX)
	JB	top
notresume:
	CMPQ	R8, 24(AX)
	JB	fail
	CMPQ	R8, 32(AX)
	JAE	fail
	MOVQ	(DI), R8
	CMPQ	R8, DI
	JBE	fail
	CMPQ	R8, DX
	JAE	fail
	MOVQ	R8, DI
	JMP	walk
top:
	LEAQ	16(DI), CX
	SUBQ	SI, CX
	MOVQ	CX, 88(AX)
	MOVQ	DI, R11
	SUBQ	SI, R11
	MOVQ	R11, 96(AX)
	CMPQ	CX, 80(AX)
	JA	done
	MOVQ	SI, BX
	MOVQ	72(AX), R10
	MOVQ	R10, DI
	REP; MOVSB
	MOVQ	8(AX), R8
	SUBQ	16(AX), R8
relink:
	CMPQ	R8, R11
	JEQ	last
	MOVQ	(R10)(R8*1), CX
	SUBQ	BX, CX
	MOVQ	CX, (R10)(R8*1)
	MOVQ	CX, R8
	JMP	relink
last:
	MOVQ	$0, (R10)(R11*1)
done:
	RET
fail:
	MOVQ	$0, 88(AX)
	RET

// vmResume is what a child of fork enters by, func(request uintptr)
// uint64 with AX, on a ResumeRequest (instance.go): it grows the stack until
// the frames fit, then vmResumePlace, with a frame of its own, places the
// frames below its stack pointer, links them to its frame and returns from
// the call of the jump frame with 0, the instance's base set so that the
// parent's offsets hold. The top frame returns into vmResumeReturn's
// continuation, after a push never executed that gives it the frame of
// the saved RBP, and that returns to Go with what the frame left in AX.
// Writing below its stack pointer, vmResumePlace must not be preempted,
// which assembly is not.
TEXT ·vmResume(SB), NOSPLIT|NOFRAME, $0-0
	NO_LOCAL_POINTERS
start:
	MOVQ	0(AX), R13
	MOVQ	R13, 472(R14)
	MOVQ	(R13), R15
	LEAQ	-512(SP), DX
	SUBQ	16(AX), DX
	CMPQ	DX, 16(R14)
	JBE	grow
	JMP	·vmResumePlace(SB)
grow:
	ADJSP	$8
	MOVQ	DX, 0(SP)
	CALL	·vmMorestack(SB)
	ADJSP	$-8
	JMP	start

TEXT ·vmResumePlace(SB), NOSPLIT|NOFRAME, $0-0
	NO_LOCAL_POINTERS
	ADJSP	$8
	MOVQ	BP, 0(SP)
	MOVQ	SP, BP
	MOVQ	SP, DI
	SUBQ	16(AX), DI
	MOVQ	DI, DX
	MOVQ	8(AX), SI
	MOVQ	16(AX), CX
	REP; MOVSB
	MOVQ	32(AX), R8
	MOVQ	24(AX), SI
relink:
	CMPQ	R8, SI
	JEQ	last
	MOVQ	(DX)(R8*1), CX
	LEAQ	(DX)(CX*1), DI
	MOVQ	DI, (DX)(R8*1)
	MOVQ	CX, R8
	JMP	relink
last:
	MOVQ	BP, (DX)(SI*1)
	LEAQ	·vmResumeReturn+1(SB), DI
	LEAQ	(DX)(SI*1), CX
	MOVQ	DI, 8(CX)
	MOVQ	8(R14), DI
	SUBQ	DX, DI
	SUBQ	40(AX), DI
	MOVQ	16(R13), CX
	MOVQ	DI, 120(CX)
	MOVQ	8(R13), R13
	MOVQ	56(AX), BX
	MOVQ	64(AX), R9
	MOVQ	72(AX), R10
	MOVQ	80(AX), R11
	MOVQ	88(AX), R12
	MOVQ	48(AX), DI
	MOVQ	DX, BP
	ADDQ	32(AX), BP
	XORL	AX, AX
	// The return from the call of the jump frame: the frames begin at the
	// base, where its stack pointer is after the return.
	MOVQ	DX, SP
	JMP	DI

TEXT ·vmResumeReturn(SB), NOSPLIT|NOFRAME, $0-0
	NO_LOCAL_POINTERS
	PUSHQ	BP
	POPQ	BP
	RET

// func strCall0(fn uintptr) uintptr calls fn, a function of lib/str, with no
// arguments in the convention between the module's functions: it keeps BX,
// R9-R12, R13-R15 and BP and returns in AX. A frame of its own makes this
// function check the stack, so what fn uses below it is above the guard.
TEXT ·strCall0(SB), 0, $8-16
	MOVQ	fn+0(FP), BX
	CALL	BX
	MOVQ	AX, ret+8(FP)
	RET

// func strCall3(fn, a, b, c uintptr) uintptr calls fn as strCall0 calls a
// function, with a, b and c in AX, CX and DX.
TEXT ·strCall3(SB), 0, $8-40
	MOVQ	a+8(FP), AX
	MOVQ	b+16(FP), CX
	MOVQ	c+24(FP), DX
	MOVQ	fn+0(FP), BX
	CALL	BX
	MOVQ	AX, ret+32(FP)
	RET

// func strMove(fn uintptr, dst, src unsafe.Pointer, n uintptr) calls lib/str's
// memmove, fn, as strCall0 calls a function, with dst, src and n in AX, CX
// and DX: the stand puts its inputs into a module's memory with it.
TEXT ·strMove(SB), 0, $8-32
	MOVQ	dst+8(FP), AX
	MOVQ	src+16(FP), CX
	MOVQ	n+24(FP), DX
	MOVQ	fn+0(FP), BX
	CALL	BX
	RET

// vmAsanRange is what an asan object calls, as it calls a function of
// lib/str, with the host address of a range of the shadow map in AX and
// its length in CX: it returns in AX how many bytes from the start are
// shadowOK, the length when all are. A word at a time, then the bytes
// left. It writes AX, CX, DX and SI only, and no stack.
TEXT ·vmAsanRange(SB), NOSPLIT|NOFRAME, $0-0
	MOVQ	AX, DX
	XORL	AX, AX
words:
	LEAQ	8(AX), SI
	CMPQ	SI, CX
	JA	bytes
	CMPQ	(DX)(AX*1), $-1
	JNE	bytes
	MOVQ	SI, AX
	JMP	words
bytes:
	CMPQ	AX, CX
	JAE	done
	CMPB	(DX)(AX*1), $-1
	JNE	done
	INCQ	AX
	JMP	bytes
done:
	RET

// The addresses of the stubs (stubs.go).
TEXT ·morestackStub(SB), NOSPLIT, $0-8
	LEAQ	·vmMorestack(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·asanRangeStub(SB), NOSPLIT, $0-8
	LEAQ	·vmAsanRange(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·interruptStub(SB), NOSPLIT, $0-8
	LEAQ	·vmInterruptCall(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·stackGrowStub(SB), NOSPLIT, $0-8
	LEAQ	·vmStackGrow(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·landStub(SB), NOSPLIT, $0-8
	LEAQ	·vmLand(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·framesStub(SB), NOSPLIT, $0-8
	LEAQ	·vmFrames(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·resumeStub(SB), NOSPLIT, $0-8
	LEAQ	·vmResume(SB), AX
	MOVQ	AX, ret+0(FP)
	RET

TEXT ·resumeReturnStub(SB), NOSPLIT, $0-8
	LEAQ	·vmResumeReturn(SB), AX
	MOVQ	AX, ret+0(FP)
	RET
