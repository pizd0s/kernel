//go:build linux && amd64

package main

import (
	"encoding/binary"
)

const (
	openedFile             = 1
	openedTerminal         = 2
	openedDir              = 3
	openedPath             = 4
	openedTells            = 16
	sizeofOpened           = 32
	readyTostop            = 1
	rOk             uint32 = 128
	rErr            uint32 = 129
	remoteHeader           = 16
	remoteStat             = 64
	remoteStatfs           = 52
	whenAtimeNow           = 2
	whenMtimeNow           = 4
	whenAtimeKept          = 8
	whenMtimeKept          = 16
	rOpen           uint32 = 96
	ioAppend               = 1
	ioNonblock             = 2
	ioAt                   = 4
	openControlling        = 4
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
)

const (
	rTcgetattr uint32 = iota + 112
	rTcsetattr
	rTcflush
	rTcflow
	rTcdrain
	rGetWinsize
	rSetWinsize
	rInq
	rOutq
	rName
	rGetPgrp
	rSetPgrp
	rSetCtty
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

func served(p *Process, kind uint32, flags int, dev, ino, id uint64, end *PairEnd) (*RemoteFD, error) {
	what := kind &^ openedTells

	if what != openedFile && what != openedTerminal && what != openedDir && what != openedPath {
		return nil, EINVAL
	}

	f := &RemoteFD{end: end, id: id, key: FileKey{dev, ino}, accmode: flags & O_ACCMODE, opath: flags&O_PATH != 0 || what == openedPath,
		append: flags&O_APPEND != 0, nonblock: flags&O_NONBLOCK != 0, tells: kind&openedTells != 0}

	if ino != 0 {
		f.locks = &p.k.locks
	}

	f.refs.Store(1)
	end.consume(f.deliver)

	return f, nil
}

func replyOf(pk *Packet, eof bool) error {
	if eof || len(pk.data) < remoteHeader {
		dropFiles(pk.files)
		pk.files = nil

		return EIO
	}

	if binary.LittleEndian.Uint32(pk.data) == rErr {
		dropFiles(pk.files)
		pk.files = nil

		return Errno(binary.LittleEndian.Uint32(pk.data[4:]))
	}

	pk.arg = binary.LittleEndian.Uint32(pk.data[4:])
	pk.data = pk.data[remoteHeader:]

	return nil
}

func decodeStat(b []byte, st *Stat) error {
	if len(b) < remoteStat {
		return EIO
	}

	le := binary.LittleEndian

	*st = Stat{}
	st.Ino, st.Size, st.Mode, st.Nlink = le.Uint64(b), int64(le.Uint64(b[8:])), le.Uint32(b[24:]), uint64(le.Uint32(b[28:]))
	st.Uid, st.Gid, st.Blksize = le.Uint32(b[32:]), le.Uint32(b[36:]), 4096
	st.Blocks = (st.Size + 511) / 512
	st.Mtim = nsecToTimespec(int64(le.Uint64(b[16:])))
	st.Atim = nsecToTimespec(int64(le.Uint64(b[40:])))
	st.Dev = le.Uint64(b[48:])
	st.Ctim = nsecToTimespec(int64(le.Uint64(b[56:])))

	return nil
}

func decodeStatfs(b []byte, st *Statfs) error {
	if len(b) < remoteStatfs {
		return EIO
	}

	le := binary.LittleEndian
	bsize := int64(le.Uint32(b[40:]))
	dev := le.Uint64(b[32:])

	*st = Statfs{Type: 0x70697a64, Bsize: bsize, Frsize: bsize, Blocks: le.Uint64(b), Bfree: le.Uint64(b[8:]),
		Bavail: le.Uint64(b[8:]), Files: le.Uint64(b[16:]), Ffree: le.Uint64(b[24:]), Namelen: int64(le.Uint32(b[44:])),
		Flags: int64(le.Uint32(b[48:]))}
	st.Fsid[0], st.Fsid[1] = int32(uint32(dev)), int32(uint32(dev>>32))

	return nil
}

func whens(ts []Timespec) (uint32, []byte) {
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
		case UTIME_NOW:
			when |= now
		case UTIME_OMIT:
			when |= kept
		default:
			binary.LittleEndian.PutUint64(body[8*i:], uint64(t.Sec)*1e9+uint64(t.Nsec))
		}
	}

	return when, body
}
