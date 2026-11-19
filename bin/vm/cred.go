//go:build linux && amd64

package main

import (
	"slices"
)

const keep = ^uint32(0)

type Cred struct {
	uids, gids [3]uint32
	groups     []uint32
}

func (c *Cred) uid() uint32 {
	return c.uids[0]
}

func (c *Cred) euid() uint32 {
	return c.uids[1]
}

func (c *Cred) gid() uint32 {
	return c.gids[0]
}

func (c *Cred) egid() uint32 {
	return c.gids[1]
}

func (c Cred) clone() Cred {
	c.groups = slices.Clone(c.groups)

	return c
}

func (c *Cred) privileged() bool {
	return c.euid() == 0
}

func may(id, r, e, s uint32) bool {
	return id == keep || id == r || id == e || id == s
}

func (c *Cred) inGroup(gid uint32) bool {
	return c.egid() == gid || slices.Contains(c.groups, gid)
}
