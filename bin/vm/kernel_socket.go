//go:build linux && amd64

package main

import (
	"syscall"
)

// The socket calls: every socket is the kernel's, of AF_UNIX, a pair's end
// (kernel_endpoint.go) or a socket of a name of the VFS's (kernel_unix.go);
// the system has no network. The address structures are Linux's.

const (
	afUnix         = 1
	afInet         = 2
	afInet6        = 10
	sockNonblock   = 0o4000
	sockCloexec    = 0o2000000
	msgDontwait    = 0x40
	msgNosignal    = 0x4000
	msgCtrunc      = 0x8
	msgCmsgCloexec = 0x40000000
	solSocket      = 1
	soAcceptconn   = 30
	scmRights      = 1
	sunPathMax     = 108
	sockaddrMax    = 128
)

func unixAddress(path []byte) []byte {
	if len(path) == 0 {
		return []byte{afUnix, 0}
	}

	return append(append([]byte{afUnix, 0}, path...), 0)
}

// putAddress writes an address the way accept and getsockname do: as much
// as the buffer at at holds, and the whole length at lenAt.
func (p *Process) putAddress(addr []byte, at, lenAt uint32) {
	if at == 0 || lenAt == 0 {
		return
	}

	room := p.u32(lenAt)
	copy(p.span(at, min(room, uint32(len(addr)))), addr)
	p.put32(lenAt, uint32(len(addr)))
}

// Messages. A guest's msghdr is seven words; its control messages are
// headed by three words and aligned to four bytes, the host's by a long
// and two words and aligned to eight.
const (
	guestCmsgHeader = 12
	hostCmsgHeader  = 16
)

func guestAlign(n uint32) uint32 {
	return (n + 3) &^ 3
}

// socket makes a socket of the kernel's, of AF_UNIX (kernel_unix.go): the
// system has no network, so no other domain.
func (p *Process) libcSocket(domain, kind, protocol uint32) uint32 {
	if domain != afUnix {
		return p.result(0, syscall.EAFNOSUPPORT)
	}

	if kind&^(sockTypeMask|sockNonblock|sockCloexec) != 0 {
		return p.result(0, syscall.EINVAL)
	}

	if protocol != 0 {
		return p.result(0, syscall.EPROTONOSUPPORT)
	}

	nonblock, cloexec := kind&sockNonblock != 0, kind&sockCloexec != 0

	switch kind & sockTypeMask {
	case sockStream, sockSeqpacket:
		return p.result(p.install(newUnixSocket(int(kind&sockTypeMask), nonblock), 0, cloexec))
	case sockDgram, sockRaw:
		return p.result(p.install(newUnixDgram(nonblock), 0, cloexec))
	}

	return p.result(0, syscall.ESOCKTNOSUPPORT)
}

// socketpair makes a pair of the kernel's (kernel_endpoint.go): of
// SOCK_STREAM, SOCK_DGRAM, which SOCK_RAW is to AF_UNIX as Linux has it,
// or SOCK_SEQPACKET.
func (p *Process) libcSocketpair(domain, kind, protocol, at uint32) uint32 {
	if domain != afUnix {
		return p.result(0, syscall.EOPNOTSUPP)
	}

	if kind&^(sockTypeMask|sockNonblock|sockCloexec) != 0 {
		return p.result(0, syscall.EINVAL)
	}

	if protocol != 0 {
		return p.result(0, syscall.EPROTONOSUPPORT)
	}

	base := int(kind & sockTypeMask)

	if base == sockCall {
		return p.result(0, p.callPair(kind, at))
	}

	switch base {
	case sockStream, sockDgram, sockSeqpacket:
	case sockRaw:
		base = sockDgram
	default:
		return p.result(0, syscall.ESOCKTNOSUPPORT)
	}

	a, b := newPairOf(base, kind&sockNonblock != 0)
	cloexec := kind&sockCloexec != 0
	x, err := p.install(a, 0, cloexec)

	if err != nil {
		b.unref()

		return p.result(0, err)
	}

	y, err := p.install(b, 0, cloexec)

	if err != nil {
		p.closeFd(x)

		return p.result(0, err)
	}

	p.put32(at, uint32(x))
	p.put32(at+4, uint32(y))

	return p.result(0, nil)
}

// The bits of a socket's type that are its kind; SOCK_RAW.
const (
	sockTypeMask = 0xf
	sockRaw      = 3
	// SOCK_CALL, a call pair's (kernel_call.go): lib/c's own, of
	// sys/socket.h.
	sockCall = 7
)

func (p *Process) libcBind(fd, addr, n uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.bind(p, addr, n)
	}

	return p.result(0, err)
}

func (p *Process) libcConnect(fd, addr, n uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.connect(p, addr, n)
	}

	return p.result(0, err)
}

func (p *Process) libcListen(fd, backlog uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.listen(int(int32(backlog)))
	}

	return p.result(0, err)
}

func (p *Process) libcAccept(fd, addr, lenAt uint32) uint32 {
	return p.socketAccept(fd, addr, lenAt, 0)
}

func (p *Process) libcAccept4(fd, addr, lenAt, flags uint32) uint32 {
	return p.socketAccept(fd, addr, lenAt, flags)
}

func (p *Process) libcGetsockname(fd, addr, lenAt uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	name, err := s.sockname(p)

	if err != nil {
		return p.result(0, err)
	}

	p.putAddress(name, addr, lenAt)

	return p.result(0, nil)
}

func (p *Process) libcGetpeername(fd, addr, lenAt uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	name, err := s.peername(p)

	if err != nil {
		return p.result(0, err)
	}

	p.putAddress(name, addr, lenAt)

	return p.result(0, nil)
}

func (p *Process) libcGetsockopt(fd, level, name, at, lenAt uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.getsockopt(p, level, name, at, lenAt)
	}

	return p.result(0, err)
}

// callPair is socketpair's SOCK_CALL: a call pair (kernel_call.go), its
// serving end first.
func (p *Process) callPair(kind, at uint32) error {
	serving, asking := newCall(kind&sockNonblock != 0)
	cloexec := kind&sockCloexec != 0
	x, err := p.install(serving, 0, cloexec)

	if err != nil {
		asking.unref()

		return err
	}

	y, err := p.install(asking, 0, cloexec)

	if err != nil {
		p.closeFd(x)

		return err
	}

	p.put32(at, uint32(x))
	p.put32(at+4, uint32(y))

	return nil
}

func (p *Process) libcSetsockopt(fd, level, name, at, n uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.setsockopt(p, level, name, at, n)
	}

	return p.result(0, err)
}

func (p *Process) libcSend(fd, buf, n, flags uint32) uint32 {
	return p.socketSend(fd, buf, n, flags, 0, 0)
}

func (p *Process) libcSendto(fd, buf, n, flags, addr, alen uint32) uint32 {
	return p.socketSend(fd, buf, n, flags, addr, alen)
}

func (p *Process) libcRecv(fd, buf, n, flags uint32) uint32 {
	return p.socketRecv(fd, buf, n, flags, 0, 0)
}

func (p *Process) libcRecvfrom(fd, buf, n, flags, addr, lenAt uint32) uint32 {
	return p.socketRecv(fd, buf, n, flags, addr, lenAt)
}

func (p *Process) libcSendmsg(fd, msg, flags uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(s.sendmsg(p, msg, flags))
}

func (p *Process) libcRecvmsg(fd, msg, flags uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(s.recvmsg(p, msg, flags))
}

func (p *Process) libcShutdown(fd, how uint32) uint32 {
	s, err := p.socketAt(fd)

	if err == nil {
		err = s.shutdown(int(how))
	}

	return p.result(0, err)
}

func (p *Process) libcSockatmark(fd uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	// A socket of the kernel's has no out-of-band data: a stream is never
	// at its mark, and the other kinds have none, as Linux's.
	if s.socketKind() == sockStream {
		return p.result(0, nil)
	}

	return p.result(0, syscall.EOPNOTSUPP)
}

func (p *Process) socketAccept(fd, addr, lenAt, flags uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	if flags&^(sockNonblock|sockCloexec) != 0 {
		return p.result(0, syscall.EINVAL)
	}

	if err := p.descriptorFree(); err != nil {
		return p.result(0, err)
	}

	conn, peer, err := s.accept(p, flags)

	if err != nil {
		return p.result(0, err)
	}

	guest, err := p.install(conn, 0, flags&sockCloexec != 0)

	if err != nil {
		return p.result(0, err)
	}

	p.putAddress(peer, addr, lenAt)

	return p.result(guest, nil)
}

func (p *Process) socketSend(fd, buf, n, flags, addr, alen uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	return p.result(s.sendto(p, buf, n, flags, addr, alen))
}

func (p *Process) socketRecv(fd, buf, n, flags, addr, lenAt uint32) uint32 {
	s, err := p.socketAt(fd)

	if err != nil {
		return p.result(0, err)
	}

	got, from, err := s.recvfrom(p, buf, n, flags)

	if err == nil && addr != 0 {
		p.putAddress(from, addr, lenAt)
	}

	return p.result(got, err)
}
