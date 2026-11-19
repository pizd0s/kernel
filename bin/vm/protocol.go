//go:build linux && amd64

package main

import (
	"encoding/binary"
	"syscall"
)

const (
	openedFile            = 1
	openedTerminal        = 2
	openedDir             = 3
	openedPath            = 4
	openedTells           = 16
	sizeofOpened          = 32
	readyTostop           = 1
	rOk            uint32 = 128
	rErr           uint32 = 129
	remoteHeader          = 16
	remoteStat            = 64
	remoteStatfs          = 52
	whenAtimeNow          = 2
	whenMtimeNow          = 4
	whenAtimeKept         = 8
	whenMtimeKept         = 16
	utimeNow              = 1<<30 - 1
	utimeOmit             = 1<<30 - 2
	rOpen          uint32 = 96
)

const (
	rRead uint32 = iota + 64
	rWrite
	rReaddir
	rFstat
	rFchmod
	rFchown
	rFutimes
	rFtruncate
	rFsync
	rFstatfs
	rCancel
	rReady
	rConnect
)

const (
	vOpen uint32 = iota + 1
	vStat
	vReadlink
	vMkdir
	vMknod
	vSymlink
	vUnlink
	vRename
	vLink
	vChmod
	vChown
	vUtimes
	vTruncate
	vStatfs
	vRealpath
	vMount
	vUnmount
	vPath
	vBind
	vConnect
)

func served(p *Process, kind uint32, flags int, dev, ino, id uint64, end *PairEnd) (Object, error) {
	var n *RemoteNode
	var f Object

	switch kind &^ openedTells {
	case openedFile:
		r := &RemoteFile{}

		n, f = &r.node, r

	case openedTerminal:
		t := &RemoteTerminal{key: FileKey{dev, ino}}

		n, f = &t.node, t
	case openedDir:
		d := &RemoteDir{id: id, opath: flags&oPath != 0}

		n, f = &d.node, d
	case openedPath:
		h := &RemotePath{id: id}

		n, f = &h.node, h
	default:
		return nil, syscall.EINVAL
	}

	if what := kind &^ openedTells; ino != 0 && (what == openedFile || what == openedDir) {
		n.locks, n.file = &p.k.locks, FileKey{dev, ino}
	}

	n.end, n.accmode, n.tells = end, flags&syscall.O_ACCMODE, kind&openedTells != 0
	n.refs.Store(1)
	n.append.Store(flags&syscall.O_APPEND != 0)
	n.nonblock.Store(flags&syscall.O_NONBLOCK != 0)

	end.from.drain(n.deliver, n.hungUp)

	return f, nil
}

func replyOf(pk *Packet, eof bool) error {
	if eof || len(pk.data) < remoteHeader {
		dropFiles(pk.files)
		pk.files = nil

		return syscall.EIO
	}

	if binary.LittleEndian.Uint32(pk.data) == rErr {
		dropFiles(pk.files)
		pk.files = nil

		return syscall.Errno(binary.LittleEndian.Uint32(pk.data[4:]))
	}

	pk.arg = binary.LittleEndian.Uint32(pk.data[4:])
	pk.data = pk.data[remoteHeader:]

	return nil
}

func decodeStat(b []byte, st *syscall.Stat_t) error {
	if len(b) < remoteStat {
		return syscall.EIO
	}

	le := binary.LittleEndian

	*st = syscall.Stat_t{}
	st.Ino, st.Size, st.Mode, st.Nlink = le.Uint64(b), int64(le.Uint64(b[8:])), le.Uint32(b[24:]), uint64(le.Uint32(b[28:]))
	st.Uid, st.Gid, st.Blksize = le.Uint32(b[32:]), le.Uint32(b[36:]), 4096
	st.Blocks = (st.Size + 511) / 512
	st.Mtim = syscall.NsecToTimespec(int64(le.Uint64(b[16:])))
	st.Atim = syscall.NsecToTimespec(int64(le.Uint64(b[40:])))
	st.Dev = le.Uint64(b[48:])
	st.Ctim = syscall.NsecToTimespec(int64(le.Uint64(b[56:])))

	return nil
}

func decodeStatfs(b []byte, st *syscall.Statfs_t) error {
	if len(b) < remoteStatfs {
		return syscall.EIO
	}

	le := binary.LittleEndian
	bsize := int64(le.Uint32(b[40:]))
	dev := le.Uint64(b[32:])

	*st = syscall.Statfs_t{Type: 0x70697a64, Bsize: bsize, Frsize: bsize, Blocks: le.Uint64(b), Bfree: le.Uint64(b[8:]),
		Bavail: le.Uint64(b[8:]), Files: le.Uint64(b[16:]), Ffree: le.Uint64(b[24:]), Namelen: int64(le.Uint32(b[44:])),
		Flags: int64(le.Uint32(b[48:]))}
	st.Fsid.X__val[0], st.Fsid.X__val[1] = int32(uint32(dev)), int32(uint32(dev>>32))

	return nil
}

func whens(ts []syscall.Timespec) (uint32, []byte) {
	when := uint32(0)
	body := make([]byte, 16)

	if ts == nil {
		return when | whenAtimeNow | whenMtimeNow, body
	}

	for i, t := range ts[:2] {
		now, kept := uint32(whenAtimeNow), uint32(whenAtimeKept)

		if i == 1 {
			now, kept = whenMtimeNow, whenMtimeKept
		}

		switch t.Nsec {
		case utimeNow:
			when |= now
		case utimeOmit:
			when |= kept
		default:
			binary.LittleEndian.PutUint64(body[8*i:], uint64(t.Sec)*1e9+uint64(t.Nsec))
		}
	}

	return when, body
}
