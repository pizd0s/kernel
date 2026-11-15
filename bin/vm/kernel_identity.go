//go:build linux && amd64

package main

import (
	"runtime"
	"slices"
	"syscall"
	"time"
)

// Who a process is: its credentials, session and group, priority and
// scheduling. The credentials are the kernel's own (cred), none of the
// host's. A priority is a nice value, a policy SCHED_OTHER, SCHED_BATCH or
// SCHED_IDLE: the kernel keeps them, Go's scheduler does not know them,
// and the real time policies need privilege.

const (
	prioProcess = 0
	prioPgrp    = 1
	prioUser    = 2
	niceMin     = -20
	niceMax     = 19
	schedOther  = 0
	schedFifo   = 1
	schedRr     = 2
	schedBatch  = 3
	schedIdle   = 5
)

// cred is a process's credentials: its real, effective and saved user
// and group ids and its supplementary groups. The first process's are
// those `vm run --user` names, root's without it; a child has its
// parent's. A process whose effective uid is 0 is privileged: it changes
// its ids to any, and the others' checks pass it, as root's do; another
// sets an id only to one of those it has, as Linux's.
type cred struct {
	// uids and gids are the real, effective and saved ids.
	uids, gids [3]uint32
	groups     []uint32
}

func (c *cred) uid() uint32 {
	return c.uids[0]
}

func (c *cred) euid() uint32 {
	return c.uids[1]
}

func (c *cred) gid() uint32 {
	return c.gids[0]
}

func (c *cred) egid() uint32 {
	return c.gids[1]
}

// clone is a copy of its own, a child's.
func (c cred) clone() cred {
	c.groups = slices.Clone(c.groups)

	return c
}

// keep is the -1 of an id a call leaves as it is.
const keep = ^uint32(0)

func (c *cred) privileged() bool {
	return c.euid() == 0
}

// may tells whether an unprivileged process may take id, of real r,
// effective e and saved s.
func may(id, r, e, s uint32) bool {
	return id == keep || id == r || id == e || id == s
}

// inGroup tells whether the process is of group gid: its effective gid or
// one of its supplementary groups.
func (c *cred) inGroup(gid uint32) bool {
	return c.egid() == gid || slices.Contains(c.groups, gid)
}

// setIds is setresuid's and setresgid's change of a process's ids, with
// the kernel's lock: r, e and s, each kept by -1.
func (p *Process) setIds(ids *[3]uint32, r, e, s uint32) error {
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	if !p.cred.privileged() && !(may(r, ids[0], ids[1], ids[2]) && may(e, ids[0], ids[1], ids[2]) && may(s, ids[0], ids[1], ids[2])) {
		return syscall.EPERM
	}

	for i, id := range [3]uint32{r, e, s} {
		if id != keep {
			ids[i] = id
		}
	}

	return nil
}

// setId is setuid's and setgid's: a privileged process's sets all three, an
// unprivileged one's the effective, to the real or the saved id.
func (p *Process) setId(ids *[3]uint32, id uint32) error {
	if p.cred.privileged() {
		return p.setIds(ids, id, id, id)
	}

	if id != ids[0] && id != ids[2] {
		return syscall.EPERM
	}

	return p.setIds(ids, keep, id, keep)
}

// setReIds is setreuid's and setregid's: the real to the real or the
// effective, the effective to any of the three; a change of the real, or of
// the effective to other than the real, makes the saved the new effective.
func (p *Process) setReIds(ids *[3]uint32, r, e uint32) error {
	if !p.cred.privileged() && (r != keep && r != ids[0] && r != ids[1] || !may(e, ids[0], ids[1], ids[2])) {
		return syscall.EPERM
	}

	saved := keep

	if r != keep || e != keep && e != ids[0] {
		saved = e

		if e == keep {
			saved = ids[1]
		}
	}

	return p.setIds(ids, r, e, saved)
}

// process is the process of a pid, the caller for 0.
func (p *Process) process(pid int32) (*Process, error) {
	if pid == 0 || pid == p.pid {
		return p, nil
	}

	p.k.mu.Lock()
	q, ok := p.k.procs[pid]
	p.k.mu.Unlock()

	if !ok || q.zombie {
		return nil, syscall.ESRCH
	}

	return q, nil
}

// prioTargets are the processes getpriority and setpriority name.
func (p *Process) prioTargets(which, who uint32) ([]*Process, error) {
	switch which {
	case prioProcess:
		q, err := p.process(int32(who))

		if err != nil {
			return nil, err
		}

		return []*Process{q}, nil
	case prioPgrp:
		pgid := int32(who)

		if pgid == 0 {
			pgid = p.pgid
		}

		return p.k.processes(func(q *Process) bool { return q.pgid == pgid && !q.zombie }), nil
	case prioUser:
		if who == 0 {
			who = p.cred.uid()
		}

		return p.k.processes(func(q *Process) bool { return !q.zombie && q.cred.uid() == who }), nil
	}

	return nil, syscall.EINVAL
}

func (p *Process) libcSetuid(uid uint32) uint32 {
	return p.result(0, p.setId(&p.cred.uids, uid))
}

func (p *Process) libcSeteuid(uid uint32) uint32 {
	return p.result(0, p.setIds(&p.cred.uids, keep, uid, keep))
}

func (p *Process) libcSetreuid(r, e uint32) uint32 {
	return p.result(0, p.setReIds(&p.cred.uids, r, e))
}

func (p *Process) libcSetresuid(r, e, s uint32) uint32 {
	return p.result(0, p.setIds(&p.cred.uids, r, e, s))
}

func (p *Process) libcSetgid(gid uint32) uint32 {
	return p.result(0, p.setId(&p.cred.gids, gid))
}

func (p *Process) libcSetegid(gid uint32) uint32 {
	return p.result(0, p.setIds(&p.cred.gids, keep, gid, keep))
}

func (p *Process) libcSetregid(r, e uint32) uint32 {
	return p.result(0, p.setReIds(&p.cred.gids, r, e))
}

func (p *Process) libcSetresgid(r, e, s uint32) uint32 {
	return p.result(0, p.setIds(&p.cred.gids, r, e, s))
}

func (p *Process) libcGetresuid(r, e, s uint32) uint32 {
	for i, at := range []uint32{r, e, s} {
		p.put32(at, p.cred.uids[i])
	}

	return p.result(0, nil)
}

func (p *Process) libcGetresgid(r, e, s uint32) uint32 {
	for i, at := range []uint32{r, e, s} {
		p.put32(at, p.cred.gids[i])
	}

	return p.result(0, nil)
}

func (p *Process) libcSetsid() uint32 {
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	for _, q := range p.k.procs {
		if q.pgid == p.pid {
			return p.result(0, syscall.EPERM)
		}
	}

	p.sid, p.pgid = p.pid, p.pid

	return p.result(int(p.pid), nil)
}

func (p *Process) libcGetsid(pid uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	return p.result(int(q.sid), nil)
}

func (p *Process) libcGetpgid(pid uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	return p.result(int(q.pgid), nil)
}

func (p *Process) libcSetpgrp() uint32 {
	return p.libcSetpgid(0, 0)
}

// nice returns the new nice value; lowering it needs privilege.
func (p *Process) libcNice(inc uint32) uint32 {
	p.k.mu.Lock()
	n := min(max(p.nice+int32(inc), niceMin), niceMax)

	if n < p.nice && !p.cred.privileged() {
		p.k.mu.Unlock()

		return p.result(0, syscall.EPERM)
	}

	p.nice = n
	p.k.mu.Unlock()

	return p.result(int(n), nil)
}

// getpriority returns the lowest nice value of the processes named.
func (p *Process) libcGetpriority(which, who uint32) uint32 {
	targets, err := p.prioTargets(which, who)

	if err == nil && len(targets) == 0 {
		err = syscall.ESRCH
	}

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()
	n := int32(niceMax)

	for _, q := range targets {
		n = min(n, q.nice)
	}

	p.k.mu.Unlock()

	return p.result(int(n), nil)
}

func (p *Process) libcSetpriority(which, who, prio uint32) uint32 {
	targets, err := p.prioTargets(which, who)

	if err == nil && len(targets) == 0 {
		err = syscall.ESRCH
	}

	if err != nil {
		return p.result(0, err)
	}

	n := min(max(int32(prio), niceMin), niceMax)
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	for _, q := range targets {
		if n < q.nice && !p.cred.privileged() {
			return p.result(0, syscall.EACCES)
		}
	}

	for _, q := range targets {
		q.nice = n
	}

	return p.result(0, nil)
}

func (p *Process) libcSched_yield() uint32 {
	runtime.Gosched()

	return 0
}

func (p *Process) libcSched_get_priority_max(policy uint32) uint32 {
	switch policy {
	case schedFifo, schedRr:
		return p.result(99, nil)
	case schedOther, schedBatch, schedIdle:
		return p.result(0, nil)
	}

	return p.result(0, syscall.EINVAL)
}

func (p *Process) libcSched_get_priority_min(policy uint32) uint32 {
	switch policy {
	case schedFifo, schedRr:
		return p.result(1, nil)
	case schedOther, schedBatch, schedIdle:
		return p.result(0, nil)
	}

	return p.result(0, syscall.EINVAL)
}

func (p *Process) libcSched_getscheduler(pid uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	return p.result(int(q.policy), nil)
}

func (p *Process) libcSched_getparam(pid, param uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return p.result(0, err)
	}

	p.put32(param, 0)

	return p.result(0, nil)
}

// The priority of the policies the kernel keeps is 0.
func (p *Process) libcSched_setparam(pid, param uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return p.result(0, err)
	}

	if p.u32(param) != 0 {
		return p.result(0, syscall.EINVAL)
	}

	return p.result(0, nil)
}

func (p *Process) libcSched_setscheduler(pid, policy, param uint32) uint32 {
	q, err := p.process(int32(pid))

	if err != nil {
		return p.result(0, err)
	}

	switch policy {
	case schedOther, schedBatch, schedIdle:
		if p.u32(param) != 0 {
			return p.result(0, syscall.EINVAL)
		}

		p.k.mu.Lock()
		q.policy = int32(policy)
		p.k.mu.Unlock()

		return p.result(0, nil)
	case schedFifo, schedRr:
		return p.result(0, syscall.EPERM)
	}

	return p.result(0, syscall.EINVAL)
}

// sched_rr_get_interval is the quantum of Linux's SCHED_RR.
func (p *Process) libcSched_rr_get_interval(pid, ts uint32) uint32 {
	if _, err := p.process(int32(pid)); err != nil {
		return p.result(0, err)
	}

	p.putTimespec(ts, 100*time.Millisecond)

	return p.result(0, nil)
}

// setgroups and initgroups need privilege: initgroups's groups are gid
// and those of the group database that name user a member.
func (p *Process) libcSetgroups(n, list uint32) uint32 {
	if n > ngroupsMax {
		return p.result(0, syscall.EINVAL)
	}

	groups := make([]uint32, n)

	for i := range groups {
		groups[i] = p.u32(list + uint32(4*i))
	}

	return p.result(0, p.setGroups(groups))
}

func (p *Process) libcInitgroups(user, gid uint32) uint32 {
	name := p.str(user)
	groups := []uint32{gid}

	for _, e := range p.groupEntries() {
		if slices.Contains(e.members, name) && !slices.Contains(groups, e.gid) {
			groups = append(groups, e.gid)
		}
	}

	return p.result(0, p.setGroups(groups))
}

// ngroupsMax is NGROUPS_MAX, Linux's.
const ngroupsMax = 65536

func (p *Process) setGroups(groups []uint32) error {
	p.k.mu.Lock()
	defer p.k.mu.Unlock()

	if !p.cred.privileged() {
		return syscall.EPERM
	}

	p.cred.groups = groups

	return nil
}
