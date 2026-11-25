package main

type Timespec struct {
	Sec  int64
	Nsec int64
}

func nsecToTimespec(nsec int64) Timespec {
	sec := nsec / 1e9

	nsec = nsec % 1e9

	if nsec < 0 {
		nsec += 1e9
		sec--
	}

	return Timespec{Sec: sec, Nsec: nsec}
}
