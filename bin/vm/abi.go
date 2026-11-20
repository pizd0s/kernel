package main

const (
	offMem              = 0
	offMemSize          = 8
	offMemBound         = 16
	offStackLimit       = 24
	offHost             = 32
	offTokens           = 40
	offJump             = 48
	offBase             = offJump + 8*jumpWords
	offInterrupt        = offBase + 8
	offCell             = offInterrupt + 8
	headerSize          = offCell + 8
	gInstance           = 472
	jumpPC              = 0
	jumpSP              = 1
	jumpBP              = 2
	jumpRegs            = 3
	jumpToken           = 8
	jumpWords           = 9
	jumpSaved           = 5
	boundSlack          = 1024
	profileCounterBytes = 8
	tableEntries        = 0
	tableSize           = 8
	tableMax            = 12
	tableBytes          = 16
	descBytes           = 16
)
