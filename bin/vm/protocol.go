//go:build linux && amd64

package main

import (
	"encoding/binary"
	"syscall"
)

// A server's files: the kernel speaks the protocol of
// lib/protocol/protocol.h here for the POSIX calls of a process, as the
// libc of the process would. An open file is the second end of a pair its
// server made, a seqpacket socketpair's, which an OPEN's OK brings with
// what it is (opened): a file, a terminal's (remote_terminal.go), a
// handle of a directory of rootfs's, which the calls with a path go to
// whole (remote_dir.go), or one of what O_PATH opened that is no
// directory; the server holds the first end, its endpoint of the open
// file. And an end of a pair a process makes its working directory is a
// directory, the root of the server behind it (directory).
//
// A call on an open file goes on a connection to it. The kernel connects
// for the process: CONNECT on the open file's endpoint, a question, which
// the server answers there, the questions' answers in their order, with
// the asking end of a call pair it made (call_end.go), whose serving
// end it keeps. The request is the call sent on the connection, and the
// reply its answer, the server's own or that of one it handed the
// request on to, serving end and all. The process keeps the connection
// for its next call on the open file (connected): a call is a connection
// made only when it has none to it, or the one it has is in a call, a
// handler's call inside a call it interrupted.
//
// A packet is a header and a body; files ride with it.
//
//	header { op u32, arg u32, uid u32, gid u32 }
//
//	uid and gid are who asks: the process the kernel calls for, whose ids
//	are vm's; a server makes what it creates theirs and lets them do what
//	its modes say, gid their group (permits). A reply's are zero.
//
//	An open's OK brings the open file, the second end of a pair the server
//	made, whose first end it keeps, and its body says what it is:
//	  opened { kind u32, flags u32, dev u64, ino u64, id u64 }
//	With no body the end is the opener's as it is, a socket. A READ or a
//	WRITE brings a memfd of the
//	memory the process reads into or writes from (memfd_file.go): the
//	server writes the bytes there or reads them from there itself, from
//	the memfd's byte woff on, one copy, as many as asked, and closes it as
//	it answers. A server that is gone is EIO. An open file's calls:
//	  READ      arg len   io               OK count u32
//	  WRITE     arg len   io               OK count u32 at u64, where an
//	                                       append went
//	  READDIR             after u64        OK (ino u64, pos u64, type u8, name, 0)*
//	  FSTAT                                OK stat
//	  FCHMOD    arg mode
//	  FCHOWN              uid u32 gid u32
//	  FUTIMES   arg when  atime u64 mtime u64   when: 2 atime now, 4 mtime
//	                                       now, 8 atime kept, 16 mtime kept
//	  FTRUNCATE           size u64
//	  FSYNC     arg data
//	  FSTATFS                              OK statfs
//	  CANCEL              on the connection of a READ or a WRITE the server
//	                      keeps while it waits, from the kernel (waiting):
//	                      the server answers that request now, with the
//	                      count so far or ERR EINTR
//	  CONNECT             on the open file's endpoint: OK, fds[0] the asking
//	                      end of a call pair, a connection
//	  READY     arg events                 from the server, on its end of an
//	                                       open file it made telling
//	                                       (openedTells): what poll finds
//	                                       of it now, the last counts
//	replies: OK, ERR with arg the errno
//	io { off u64, woff u32, flags u32 }   flags: ioAppend, a WRITE at the
//	       end the file has, O_APPEND's; ioNonblock, answered at once, EAGAIN
//	       where it would wait, O_NONBLOCK's
//	stat { ino u64, size u64, mtime u64 nanoseconds, mode u32, nlink u32,
//	       uid u32, gid u32, atime u64, dev u64, ctime u64 }
//	statfs { blocks u64, bfree u64, files u64, ffree u64, dev u64,
//	         bsize u32, namemax u32, flags u32 }   flags: 1 read-only
//
// A directory's entries have positions, each after those before it, and a
// READDIR gives those after one: removing what was read moves nothing a
// reading has to come to. dev is the file system's own, the files of two
// never share one. A WRITE with append goes at the end the file has when
// the server takes it: O_APPEND is the server's, so no write of another
// comes between. The locks of flock and fcntl are the kernel's
// (lock_table.go), kept by the dev and ino the server made the open file
// with; a FIFO's pipe is the kernel's too (fifo_name.go).
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

// The kinds of an opened (lib/protocol/protocol.h), what an OPEN's OK says
// the end it brings is, and its server's telling of its readiness; the
// size of one. READY's flags: the terminal's TOSTOP.
const (
	openedFile     = 1
	openedTerminal = 2
	openedDir      = 3
	openedPath     = 4
	openedTells    = 16
	sizeofOpened   = 32
	readyTostop    = 1
)

const (
	rOk          uint32 = 128
	rErr         uint32 = 129
	remoteHeader        = 16
	remoteStat          = 64
	remoteStatfs        = 52
	// The whens of UTIMES and FUTIMES.
	whenAtimeNow  = 2
	whenMtimeNow  = 4
	whenAtimeKept = 8
	whenMtimeKept = 16
	utimeNow      = 1<<30 - 1
	utimeOmit     = 1<<30 - 2
)

// served is the open file an opened makes of a pair's end for a process,
// as the open flags say: a
// file, a terminal's, or a handle of id, of a directory or of what O_PATH
// opened; with openedTells its server's READY is its readiness. The dev
// and ino it gives name it (fileKey): a file and a directory have their
// locks by the name, unless the ino is none, and a terminal is one
// terminal by it.
func served(p *Process, kind uint32, flags int, dev, ino, id uint64, end *pairEnd) (Object, error) {
	var n *remoteNode
	var f Object

	switch kind &^ openedTells {
	case openedFile:
		r := &remoteFile{}
		n, f = &r.node, r

	case openedTerminal:
		t := &remoteTerminal{key: fileKey{dev, ino}}
		n, f = &t.node, t
	case openedDir:
		d := &remoteDir{id: id, opath: flags&oPath != 0}
		n, f = &d.node, d
	case openedPath:
		h := &remotePath{id: id}
		n, f = &h.node, h
	default:
		return nil, syscall.EINVAL
	}

	if what := kind &^ openedTells; ino != 0 && (what == openedFile || what == openedDir) {
		n.locks, n.file = &p.k.locks, fileKey{dev, ino}
	}

	n.end, n.accmode, n.tells = end, flags&syscall.O_ACCMODE, kind&openedTells != 0
	n.refs.Store(1)
	n.append.Store(flags&syscall.O_APPEND != 0)
	n.nonblock.Store(flags&syscall.O_NONBLOCK != 0)

	// What the server sends on its endpoint is the kernel's to take: its
	// READY, and the answers to who connects.
	end.from.drain(n.deliver, n.hungUp)

	return f, nil
}

// replyOf makes of the packet a reply came in the reply itself, its body
// after the header; or it is the server's error, and EIO for a server
// that went with no answer, eof.
func replyOf(pk *packet, eof bool) error {
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

// decodeStat is a Stat_t of the protocol's stat.
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

// decodeStatfs is a Statfs_t of the protocol's statfs: the server's
// device is the fsid.
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

// whens is the when of UTIMES for the times of a call: nil is now for
// both, UTIME_NOW and UTIME_OMIT in a nanoseconds field are what they say.
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

// The VFS's calls (lib/protocol/protocol.h). A process of the system has
// rootfs's handles for its root, its working directory and its directory
// descriptors, and a call with a path goes to rootfs whole, on the handle
// it starts at: the root for an absolute path, else the working directory
// or the directory of a descriptor. rootfs is the VFS: it walks the path,
// follows the links, crosses the mounts, stops ".." at the root and makes
// the last step as Linux makes it; the kernel walks nothing of the
// system's. The id of the process's root, the handle's, goes first in
// every call. What a call opens is the open file its server made of a
// served pair, as it is: a file system's file, which the kernel then
// reads and writes itself (remote_node.go), or another handle.
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

// A service's OPEN, and on a slave's open file another open of the slave.
const rOpen uint32 = 96
