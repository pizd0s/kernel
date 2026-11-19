//go:build linux && amd64

package main

const (
	sizeofAddrinfo    = 32
	sizeofDirent      = 280
	sizeofEpollEvent  = 16
	sizeofFdSet       = 128
	sizeofFlock       = 32
	sizeofGlob        = 36
	sizeofGroup       = 16
	sizeofHostent     = 20
	sizeofIfNameindex = 8
	sizeofInAddr      = 4
	sizeofIovec       = 8
	sizeofItimerval   = 32
	sizeofJmpBuf      = 80
	sizeofLconv       = 56
	sizeofMsghdr      = 28
	sizeofNetent      = 16
	sizeofPasswd      = 28
	sizeofPollfd      = 8
	sizeofProtoent    = 12
	sizeofRegex       = 32
	sizeofRegmatch    = 8
	sizeofRlimit      = 16
	sizeofRusage      = 88
	sizeofSchedParam  = 4
	sizeofServent     = 16
	sizeofSigaction   = 24
	sizeofSiginfo     = 36
	sizeofSigset      = 8
	sizeofStack       = 12
	sizeofStat        = 112
	sizeofStatfs      = 88
	sizeofStatvfs     = 72
	sizeofSysinfo     = 312
	sizeofTermios     = 60
	sizeofTimespec    = 16
	sizeofTimeval     = 16
	sizeofTimezone    = 8
	sizeofTm          = 44
	sizeofTms         = 32
	sizeofUtimbuf     = 16
	sizeofUtsname     = 390
	sizeofWinsize     = 8
)

func fcntlSize(cmd uint32) uint32 {
	switch cmd {
	case fGetlk, fSetlk, fSetlkw:
		return sizeofFlock
	}

	return 0
}

func ioctlSize(req uint32) uint32 {
	switch req {
	case tcgets, tcsets, tcsets + 1, tcsets + 2:
		return sizeofTermios
	case tiocgwinsz, tiocswinsz:
		return sizeofWinsize
	}

	return 0
}
