package main

import (
	"sync"
	"time"
)

type SemSet struct {
	mu           sync.Mutex
	perm         IpcPerm
	sems         []SemValue
	otime, ctime int64
	watchers     Subscribers
	undo         map[*Process][]int32
	removed      bool
}

func newSemSet(perm IpcPerm, n int) *SemSet {
	return &SemSet{perm: perm, sems: make([]SemValue, n), ctime: time.Now().Unix(), undo: map[*Process][]int32{}}
}

func (s *SemSet) apply(p *Process, ops []SemOp) (int, error) {
	values := make([]int32, len(s.sems))

	for i, v := range s.sems {
		values[i] = v.value
	}

	for i, o := range ops {
		v := values[o.num] + int32(o.op)

		switch {
		case o.op > 0 && v > SEMVMX:
			return -1, ERANGE
		case o.op < 0 && v < 0, o.op == 0 && values[o.num] != 0:
			return i, nil
		}

		values[o.num] = v
	}

	for _, o := range ops {
		s.sems[o.num].value, s.sems[o.num].pid = values[o.num], p.pid

		if o.flags&SEM_UNDO != 0 && o.op != 0 {
			adj := s.undo[p]

			if adj == nil {
				adj = make([]int32, len(s.sems))
				s.undo[p] = adj
				p.undoSet(s)
			}

			adj[o.num] -= int32(o.op)
		}
	}

	s.otime = time.Now().Unix()
	s.watchers.tell()

	return -1, nil
}

func (s *SemSet) operate(p *Process, ops []SemOp, end time.Time) error {
	s.mu.Lock()

	defer s.mu.Unlock()

	for {
		if s.removed {
			return EIDRM
		}

		alters := false

		for _, o := range ops {
			alters = alters || o.op != 0
		}

		want := uint32(4)

		if alters {
			want = 2
		}

		if !s.perm.grants(&p.cred, want) {
			return EACCES
		}

		blocked, err := s.apply(p, ops)

		if err != nil || blocked < 0 {
			return err
		}

		o := ops[blocked]

		if o.flags&IPC_NOWAIT != 0 {
			return EAGAIN
		}

		count := &s.sems[o.num].ncnt

		if o.op == 0 {
			count = &s.sems[o.num].zcnt
		}

		*count++

		timedOut, err := p.watch(&s.mu, &s.watchers, false, end, interrupts)

		*count--

		switch {
		case err != nil:
			return err
		case timedOut:
			return EAGAIN
		}
	}
}

func (s *SemSet) set(p *Process, num int, value int32) {
	s.sems[num].value, s.sems[num].pid = value, p.pid

	for _, adj := range s.undo {
		adj[num] = 0
	}

	s.ctime = time.Now().Unix()
	s.watchers.tell()
}

func (s *SemSet) undoExit(p *Process) {
	s.mu.Lock()

	defer s.mu.Unlock()

	adj := s.undo[p]

	delete(s.undo, p)

	if s.removed {
		return
	}

	for i, a := range adj {
		if a != 0 {
			s.sems[i].value, s.sems[i].pid = min(max(s.sems[i].value+a, 0), SEMVMX), p.pid
		}
	}

	s.watchers.tell()
}

func (s *SemSet) remove() {
	s.mu.Lock()
	s.removed = true
	s.undo = map[*Process][]int32{}
	s.watchers.tell()
	s.mu.Unlock()
}

func (s *SemSet) permission() IpcPerm {
	s.mu.Lock()

	defer s.mu.Unlock()

	return s.perm
}
