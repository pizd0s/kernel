package main

type IpcPerm struct {
	key        int32
	uid, gid   uint32
	cuid, cgid uint32
	mode       uint32
	seq        int32
}

func (m *IpcPerm) owned(c *Cred) bool {
	return c.privileged() || c.euid() == m.uid || c.euid() == m.cuid
}

func (m *IpcPerm) grants(c *Cred, want uint32) bool {
	if c.privileged() {
		return true
	}

	bits := m.mode

	switch {
	case c.euid() == m.uid || c.euid() == m.cuid:
		bits >>= 6
	case c.inGroup(m.gid) || c.inGroup(m.cgid):
		bits >>= 3
	}

	return bits&want == want
}
