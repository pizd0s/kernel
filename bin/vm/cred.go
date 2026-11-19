//go:build linux && amd64

package main

import (
	"slices"
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
