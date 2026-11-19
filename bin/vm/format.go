package main

const (
	I32               ValType = 0x7f
	I64               ValType = 0x7e
	F32               ValType = 0x7d
	F64               ValType = 0x7c
	V128              ValType = 0x7b
	FuncRef           ValType = 0x70
	ExternRef         ValType = 0x6f
	externFunc                = 0
	externTable               = 1
	externMemory              = 2
	externGlobal              = 3
	trapUnreachable           = 1
	trapMemoryOOB             = 2
	trapIntOverflow           = 3
	trapDivByZero             = 4
	trapConversion            = 5
	trapIndirectType          = 6
	trapUndefinedElem         = 7
	trapUninitElem            = 8
	trapStack                 = 9
	trapTableOOB              = 10
	trapNullRef               = 11
	trapAsan                  = 12
	asanLoad                  = 0
	asanStore                 = 1
	asanRange                 = 2
)

type ValType uint8

type FuncType struct {
	params  []ValType
	results []ValType
}

type SpEntry struct {
	pc    int32
	delta int32
}

func alignTo(x, n int32) int32 {
	return (x + n - 1) &^ (n - 1)
}
