//go:build linux && amd64

package main

import (
	"sync"
	"sync/atomic"
)

const (
	aioRead = iota
	aioWrite
	aioSync
	aioDataSync
)

type AioOp struct {
	cb       uint32
	fd       uint32
	kind     int
	file     Object
	stream   bool
	offset   int64
	n        int
	buf      uint32
	data     []byte
	event    SigEvent
	list     *AioList
	before   []*AioOp
	agent    *Process
	stop     chan struct{}
	stopping sync.Once
	over     chan struct{}
	finished atomic.Bool
	result   int
	err      Errno
	taken    bool
	done     Event
	halt     Event
}

func (op *AioOp) transfer(a *Process) (int, error) {
	switch op.kind {
	case aioRead:
		buf := make([]byte, op.n)
		n, err := a.filePread(op.file, bytesFile(buf), op.n, op.offset)

		if err == ESPIPE {
			n, err = a.fileRead(op.file, bytesFile(buf), op.n, IoMode{})
		}

		op.data = buf[:max(n, 0)]

		return n, err
	case aioWrite:
		if !op.stream {
			n, err := a.filePwrite(op.file, bytesFile(op.data), len(op.data), op.offset)

			if err != ESPIPE {
				return n, err
			}
		}

		return a.fileWrite(op.file, bytesFile(op.data), len(op.data), IoMode{})
	}

	return 0, a.request(op.file, &FileSync{data: op.kind == aioDataSync})
}

func (op *AioOp) stopped() bool {
	select {
	case <-op.stop:
		return true
	default:
		return false
	}
}

func (op *AioOp) run(owner *Process) {
	a := op.agent
	n, err := 0, error(nil)

	for _, b := range op.before {
		select {
		case <-b.over:
		case <-op.stop:
		}
	}

	if op.stopped() {
		err = ECANCELED
	} else {
		n, err = op.transfer(a)
	}

	if err == EINTR {
		err = ECANCELED
	}

	op.result = n

	if err != nil {
		op.result, op.err = -1, errnoOf(err)
	}

	if op.kind == aioWrite {
		op.data = nil
	}

	op.file.unref()
	a.root.unref()
	a.cwd.unref()
	a.setSink(nil)
	op.finished.Store(true)
	close(op.over)
	owner.post(&op.done)
	owner.nudge()
}

func (op *AioOp) abandon() {
	if op.agent == nil {
		return
	}

	op.stopping.Do(func() { close(op.stop) })
	op.agent.post(&op.halt)
}
