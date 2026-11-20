//go:build linux && amd64

package main

import (
	"math"
	"slices"
	"sync"
	"syscall"
	"time"
)

const (
	poolGrain     = 4096
	defaultMemory = 16 << 30
)

type Pool struct {
	mem []byte

	mu         sync.Mutex
	changed    sync.Cond
	extents    []*Extent
	compacting bool
	waiting    []*Process
}

func newPool(size uint64) *Pool {
	pl := &Pool{mem: mmap(int(size&^(poolGrain-1)), syscall.PROT_READ|syscall.PROT_WRITE)}

	pl.changed.L = &pl.mu

	return pl
}

func grain(n uint64) uint64 {
	return (n + poolGrain - 1) &^ (poolGrain - 1)
}

func (pl *Pool) take(by *Process, owner *Instance, used uint64) (*Extent, error) {
	pl.mu.Lock()

	defer pl.mu.Unlock()

	if by.chaosCompacts() {
		pl.shake(by, nil)
	}

	for {
		if pl.compacting {
			pl.await(by)

			continue
		}

		if at, _, ok := pl.place(nil, used, used); ok {
			x := &Extent{at: at, used: used, capacity: used, to: noMove, owner: owner}

			pl.extents = append(pl.extents, x)
			pl.order()

			return x, nil
		}

		if !pl.compact(by, nil, used) {
			return nil, ENOMEM
		}
	}
}

func (pl *Pool) grow(by *Process, x *Extent, used, most uint64) (at uint64, moving bool, err error) {
	pl.mu.Lock()

	defer pl.mu.Unlock()

	if by.chaosCompacts() {
		pl.shake(by, x)
	}

	for {
		if used <= x.capacity {
			x.used = used

			return x.at, false, nil
		}

		if pl.compacting {
			pl.await(by)

			continue
		}

		if at, capacity, ok := pl.place(x, used, min(max(used, 2*x.capacity), most)); ok {
			if at == x.at {
				x.used, x.capacity = used, capacity

				return at, false, nil
			}

			x.to, x.toCapacity = at, capacity

			return at, true, nil
		}

		if !pl.compact(by, x, used) {
			return 0, false, ENOMEM
		}
	}
}

func (pl *Pool) elsewhere(x *Extent) (uint64, bool) {
	pl.mu.Lock()

	defer pl.mu.Unlock()

	if pl.compacting || x.to != noMove {
		return 0, false
	}

	best := -1
	gaps := pl.gaps(nil)

	for k, g := range gaps {
		if g[1]-g[0] >= x.capacity && (best < 0 || g[1]-g[0] < gaps[best][1]-gaps[best][0]) {
			best = k
		}
	}

	if best < 0 {
		return 0, false
	}

	x.to, x.toCapacity = gaps[best][0], x.capacity

	return x.to, true
}

func (pl *Pool) target(x *Extent) uint64 {
	pl.mu.Lock()

	defer pl.mu.Unlock()

	return x.to
}

func (pl *Pool) settle(x *Extent, used uint64) {
	pl.mu.Lock()

	defer pl.mu.Unlock()

	x.at, x.capacity, x.used, x.to = x.to, x.toCapacity, used, noMove
	pl.order()
	pl.changed.Broadcast()
}

func (pl *Pool) give(x *Extent) {
	pl.mu.Lock()

	defer pl.mu.Unlock()

	pl.extents = slices.DeleteFunc(pl.extents, func(y *Extent) bool { return y == x })
	x.gone = true
	pl.changed.Broadcast()
}

func (pl *Pool) runner(i *Instance, q *Process) *Process {
	pl.mu.Lock()

	defer pl.mu.Unlock()

	was := i.proc

	i.proc = q

	if q == nil && i.extent.to != noMove {
		i.extent.to = noMove
		pl.changed.Broadcast()
	}

	return was
}

func byPlace(a, b *Extent) int {
	switch {
	case a.at < b.at:
		return -1
	case a.at > b.at:
		return 1
	}

	return 0
}

func (pl *Pool) order() {
	slices.SortFunc(pl.extents, byPlace)
}

func (pl *Pool) view(skip *Extent) []*Extent {
	v := make([]*Extent, 0, len(pl.extents)+1)

	for _, x := range pl.extents {
		if x == skip {
			continue
		}

		v = append(v, x)

		if x.to != noMove {
			v = append(v, &Extent{at: x.to, used: x.toCapacity, capacity: x.toCapacity, to: noMove})
		}
	}

	slices.SortFunc(v, byPlace)

	return v
}

func (pl *Pool) gaps(skip *Extent) [][2]uint64 {
	var gaps [][2]uint64

	reached := uint64(0)

	for _, x := range pl.view(skip) {
		if x.at > reached {
			gaps = append(gaps, [2]uint64{reached, x.at})
		}

		reached = max(reached, x.end())
	}

	if top := uint64(len(pl.mem)); top > reached {
		gaps = append(gaps, [2]uint64{reached, top})
	}

	return gaps
}

func (pl *Pool) spare(skip *Extent) uint64 {
	n := uint64(0)

	for _, g := range pl.gaps(skip) {
		n += g[1] - g[0]
	}

	return n
}

func (pl *Pool) fit(x *Extent, need, room uint64) (at, capacity uint64, ok bool) {
	gaps := pl.gaps(x)

	if x != nil {
		for _, g := range gaps {
			if g[0] <= x.at && x.at < g[1] && g[1]-x.at >= need {
				return x.at, min(room, g[1]-x.at), true
			}
		}
	}

	for _, want := range [2]uint64{room, need} {
		best := -1

		for k, g := range gaps {
			if g[1]-g[0] >= want && (best < 0 || g[1]-g[0] < gaps[best][1]-gaps[best][0]) {
				best = k
			}
		}

		if best >= 0 {
			return gaps[best][0], want, true
		}
	}

	return 0, 0, false
}

func (pl *Pool) place(x *Extent, need, room uint64) (at, capacity uint64, ok bool) {
	if at, capacity, ok = pl.fit(x, need, room); ok {
		return at, capacity, ok
	}

	for _, y := range pl.extents {
		if y != x && y.to == noMove {
			y.capacity = y.used
		}
	}

	return pl.fit(x, need, room)
}

func (pl *Pool) movable(y, x *Extent) bool {
	return y == x || y.owner != nil && y.owner.proc != nil && y.to == noMove
}

func (pl *Pool) window(x *Extent, need uint64) ([]*Extent, []uint64) {
	v := pl.view(nil)
	gap := make([]uint64, len(v)+1)
	reached := uint64(0)

	for k, y := range v {
		gap[k] = max(y.at, reached) - reached
		reached = max(reached, y.end())
	}

	gap[len(v)] = uint64(len(pl.mem)) - reached

	first, last, middle, cost := -1, -1, -1, uint64(math.MaxUint64)

	for a := range v {
		free, before, down, below, above, upper, j := gap[a], gap[a], uint64(0), false, uint64(0), uint64(0), -1

		for b := a; b < len(v) && pl.movable(v[b], x); b++ {
			if b > a {
				before += gap[b]
			}

			switch {
			case v[b] == x:
				j, below = b, before > 0
			case j < 0:
				if before > 0 {
					down += v[b].used
				}
			default:
				if gap[b] > 0 && b-1 > j {
					upper = above
				}

				above += v[b].used
			}

			free += gap[b+1]

			c, fits := down, free >= need

			if j < 0 && x != nil {
				c += x.used
			}

			if j >= 0 {
				fits = x.capacity+free >= need

				if below {
					c += x.used
				}

				if gap[b+1] > 0 {
					c += above
				} else {
					c += upper
				}
			}

			if fits && c < cost {
				first, last, middle, cost = a, b, j, c
			}
		}
	}

	if first < 0 {
		return nil, nil
	}

	var moves []*Extent
	var places []uint64

	at, lower := v[first].at-gap[first], last

	if middle >= 0 {
		lower = middle
	}

	for _, y := range v[first : lower+1] {
		if y.at != at {
			moves, places = append(moves, y), append(places, at)
		}

		at += y.used
	}

	if middle >= 0 {
		limit := uint64(len(pl.mem))

		if last+1 < len(v) {
			limit = v[last+1].at
		}

		for k := last; k > middle; k-- {
			limit -= v[k].used

			if v[k].at != limit {
				moves, places = append(moves, v[k]), append(places, limit)
			}
		}
	}

	return moves, places
}

func (pl *Pool) packing(x *Extent) ([]*Extent, []uint64) {
	var moves []*Extent
	var places []uint64

	reached := uint64(0)

	for _, y := range pl.view(nil) {
		if y == x || !pl.movable(y, x) || y.at == reached {
			reached = max(reached, y.end())

			continue
		}

		moves = append(moves, y)
		places = append(places, reached)
		reached += y.used
	}

	return moves, places
}

func (pl *Pool) vacant(y *Extent, at uint64) bool {
	for _, g := range pl.gaps(y) {
		if g[0] <= at && at+y.used <= g[1] {
			return true
		}
	}

	return false
}

func (pl *Pool) compact(by *Process, x *Extent, need uint64) bool {
	if pl.spare(x) < need {
		return false
	}

	pl.compacting = true

	defer pl.settled()

	for {
		moves, places := pl.window(x, need)

		if len(moves) == 0 {
			return false
		}

		if pl.slide(by, x, moves, places) {
			return true
		}
	}
}

func (pl *Pool) shake(by *Process, x *Extent) {
	if pl.compacting {
		return
	}

	pl.compacting = true

	defer pl.settled()

	for {
		moves, places := pl.packing(x)

		if len(moves) == 0 || pl.slide(by, x, moves, places) {
			return
		}
	}
}

func (pl *Pool) slide(by *Process, x *Extent, moves []*Extent, places []uint64) bool {
	for k, y := range moves {
		at := places[k]

		if y.gone || y.to != noMove || !pl.movable(y, x) || !pl.vacant(y, at) {
			return false
		}

		if y.at == at {
			continue
		}

		y.to, y.toCapacity = at, y.used

		q := y.owner.proc

		pl.mu.Unlock()

		if y == x || q == by {
			y.owner.relocate()
		} else {
			q.post(&y.owner.relocation)
			q.nudge()
		}

		pl.mu.Lock()

		for y.to != noMove && !y.gone {
			pl.changed.Wait()
		}

		if y.gone || y.at != at {
			return false
		}
	}

	return true
}

func (pl *Pool) settled() {
	pl.compacting = false

	waiting := pl.waiting

	pl.waiting = nil
	pl.changed.Broadcast()
	pl.mu.Unlock()

	for _, w := range waiting {
		w.post(&w.own.pool)
	}

	pl.mu.Lock()
}

func (pl *Pool) await(by *Process) {
	if by == nil {
		pl.changed.Wait()

		return
	}

	pl.waiting = append(pl.waiting, by)
	pl.mu.Unlock()

	by.wait(time.Time{}, func(*Event) (bool, error) {
		pl.mu.Lock()

		defer pl.mu.Unlock()

		return !pl.compacting, nil
	})

	pl.mu.Lock()
}
