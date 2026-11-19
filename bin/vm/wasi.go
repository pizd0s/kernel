//go:build linux && amd64

package main

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"runtime"
	"time"
)

var wasiErrnos = map[Errno]uint32{
	e2big: wasiE2big, eAcces: wasiEacces, eAgain: wasiEagain, eBadf: wasiEbadf,
	eExist: wasiEexist, eFault: wasiEfault, eIntr: wasiEintr, eInval: wasiEinval,
	eIsdir: wasiEisdir, eLoop: wasiEloop, eNametoolong: wasiEnametoolong,
	eNoent: wasiEnoent, eNomem: wasiEnomem, eNospc: wasiEnospc, eNosys: wasiEnosys,
	eNotdir: wasiEnotdir, eNotempty: wasiEnotempty, ePerm: wasiEperm, ePipe: wasiEpipe,
	eSpipe: wasiEspipe, eXdev: wasiExdev,
}

var wasiFunctions = map[string]any{
	"args_get":              wasiArgsGet,
	"args_sizes_get":        wasiArgsSizesGet,
	"environ_get":           wasiEnvironGet,
	"environ_sizes_get":     wasiEnvironSizesGet,
	"clock_time_get":        wasiClockTimeGet,
	"clock_res_get":         wasiClockResGet,
	"fd_write":              wasiFdWrite,
	"fd_read":               wasiFdRead,
	"fd_pwrite":             wasiFdPwrite,
	"fd_pread":              wasiFdPread,
	"fd_close":              wasiFdClose,
	"fd_seek":               wasiFdSeek,
	"fd_sync":               wasiFdSync,
	"fd_fdstat_get":         wasiFdFdstatGet,
	"fd_fdstat_set_flags":   wasiFdFdstatSetFlags,
	"fd_filestat_get":       wasiFdFilestatGet,
	"fd_filestat_set_size":  wasiFdFilestatSetSize,
	"fd_prestat_get":        wasiFdPrestatGet,
	"fd_prestat_dir_name":   wasiFdPrestatDirName,
	"fd_readdir":            wasiFdReaddir,
	"path_filestat_get":     wasiPathFilestatGet,
	"path_open":             wasiPathOpen,
	"path_create_directory": wasiPathCreateDirectory,
	"path_remove_directory": wasiPathRemoveDirectory,
	"path_unlink_file":      wasiPathUnlinkFile,
	"path_rename":           wasiPathRename,
	"path_readlink":         wasiPathReadlink,
	"poll_oneoff":           wasiPollOneoff,
	"random_get":            wasiRandomGet,
	"sched_yield":           wasiSchedYield,
	"proc_exit":             wasiProcExit,
}

const (
	wasiOK           = 0
	wasiE2big        = 1
	wasiEacces       = 2
	wasiEagain       = 6
	wasiEbadf        = 8
	wasiEexist       = 20
	wasiEfault       = 21
	wasiEintr        = 27
	wasiEinval       = 28
	wasiEio          = 29
	wasiEisdir       = 31
	wasiEloop        = 32
	wasiEnametoolong = 37
	wasiEnoent       = 44
	wasiEnomem       = 48
	wasiEnospc       = 51
	wasiEnosys       = 52
	wasiEnotdir      = 54
	wasiEnotempty    = 55
	wasiEperm        = 63
	wasiEpipe        = 64
	wasiEspipe       = 70
	wasiExdev        = 75
	wasiModule       = "wasi_snapshot_preview1"
)

func wasiErrno(err error) uint32 {
	var e Errno

	if err == nil {
		return wasiOK
	}

	if errors.As(err, &e) {
		if w, ok := wasiErrnos[e]; ok {
			return w
		}
	}

	return wasiEio
}

func isWASI(obj *ElfObject) bool {
	for _, imp := range obj.meta.imports {
		if imp.module == wasiModule {
			return true
		}
	}

	return false
}

func guest(ctx uintptr) []byte {
	return instanceAt(ctx).memoryBytes()
}

func span(m []byte, at uint32, n uint64) []byte {
	if uint64(at)+n > uint64(len(m)) {
		return nil
	}

	return m[at : uint64(at)+n]
}

func put32(m []byte, at uint32, v uint32) bool {
	b := span(m, at, 4)

	if b != nil {
		binary.LittleEndian.PutUint32(b, v)
	}

	return b != nil
}

func put64(m []byte, at uint32, v uint64) bool {
	b := span(m, at, 8)

	if b != nil {
		binary.LittleEndian.PutUint64(b, v)
	}

	return b != nil
}

func stringsSizes(m []byte, list []string, count, size uint32) uint32 {
	n := 0

	for _, s := range list {
		n += len(s) + 1
	}

	if !put32(m, count, uint32(len(list))) || !put32(m, size, uint32(n)) {
		return wasiEfault
	}

	return wasiOK
}

func stringsGet(m []byte, list []string, ptrs, buf uint32) uint32 {
	for i, s := range list {
		b := span(m, buf, uint64(len(s)+1))

		if b == nil || !put32(m, ptrs+4*uint32(i), buf) {
			return wasiEfault
		}

		copy(b, s)
		b[len(s)] = 0
		buf += uint32(len(s) + 1)
	}

	return wasiOK
}

func wasiArgsGet(ctx uintptr, argv, buf uint32) uint32 {
	return stringsGet(guest(ctx), proc(ctx).image.argv, argv, buf)
}

func wasiArgsSizesGet(ctx uintptr, count, size uint32) uint32 {
	return stringsSizes(guest(ctx), proc(ctx).image.argv, count, size)
}

func wasiEnvironGet(ctx uintptr, env, buf uint32) uint32 {
	return stringsGet(guest(ctx), proc(ctx).image.envp, env, buf)
}

func wasiEnvironSizesGet(ctx uintptr, count, size uint32) uint32 {
	return stringsSizes(guest(ctx), proc(ctx).image.envp, count, size)
}

func clock(p *Process, id uint32, res bool) (uint64, uint32) {
	if id > 3 {
		return 0, wasiEinval
	}

	if res {
		return 1, wasiOK
	}

	t, err := p.clockNow(id)

	if err != nil {
		return 0, wasiErrno(err)
	}

	return uint64(t), wasiOK
}

func wasiClockTimeGet(ctx uintptr, id uint32, precision uint64, at uint32) uint32 {
	t, e := clock(proc(ctx), id, false)

	if e == wasiOK && !put64(guest(ctx), at, t) {
		return wasiEfault
	}

	return e
}

func wasiClockResGet(ctx uintptr, id uint32, at uint32) uint32 {
	t, e := clock(proc(ctx), id, true)

	if e == wasiOK && !put64(guest(ctx), at, t) {
		return wasiEfault
	}

	return e
}

func iovecs(m []byte, iovs, count uint32) ([][]byte, uint32) {
	if count > 1024 {
		return nil, wasiEinval
	}

	table := span(m, iovs, 8*uint64(count))

	if table == nil {
		return nil, wasiEfault
	}

	out := make([][]byte, 0, count)

	for i := range count {
		base, n := binary.LittleEndian.Uint32(table[8*i:]), binary.LittleEndian.Uint32(table[8*i+4:])
		b := span(m, base, uint64(n))

		if b == nil {
			return nil, wasiEfault
		}

		out = append(out, b)
	}

	return out, wasiOK
}

func vectored(ctx uintptr, fd, iovs, count uint32, offset uint64, positioned, write bool, done uint32) uint32 {
	p, m := proc(ctx), guest(ctx)
	obj, err := p.fd(fd)

	if err != nil {
		return wasiEbadf
	}

	bufs, e := iovecs(m, iovs, count)

	if e != wasiOK {
		return e
	}

	if span(m, done, 4) == nil {
		return wasiEfault
	}

	t := obj.asTransfer()

	if t == nil {
		return wasiEbadf
	}

	total := 0

	for _, b := range bufs {
		if len(b) == 0 {
			continue
		}

		var n int

		switch {
		case positioned && write:
			n, err = t.pwrite(p, b, int64(offset)+int64(total))
		case positioned:
			n, err = t.pread(p, b, int64(offset)+int64(total))
		case write:
			n, err = t.write(p, b, IoMode{})
		default:
			n, err = t.read(p, b, IoMode{})
		}

		total += n

		if err != nil || n < len(b) {
			break
		}
	}

	if err != nil && total == 0 {
		return wasiErrno(err)
	}

	put32(m, done, uint32(total))

	return wasiOK
}

func wasiFdWrite(ctx uintptr, fd, iovs, count, written uint32) uint32 {
	return vectored(ctx, fd, iovs, count, 0, false, true, written)
}

func wasiFdRead(ctx uintptr, fd, iovs, count, read uint32) uint32 {
	return vectored(ctx, fd, iovs, count, 0, false, false, read)
}

func wasiFdPwrite(ctx uintptr, fd, iovs, count uint32, offset uint64, written uint32) uint32 {
	return vectored(ctx, fd, iovs, count, offset, true, true, written)
}

func wasiFdPread(ctx uintptr, fd, iovs, count uint32, offset uint64, read uint32) uint32 {
	return vectored(ctx, fd, iovs, count, offset, true, false, read)
}

func wasiFdClose(ctx uintptr, fd uint32) uint32 {
	p := proc(ctx)

	if !p.closeFd(int(fd)) {
		return wasiEbadf
	}

	if int(fd) == p.wasiRoot {
		p.wasiRoot = -1
	}

	return wasiOK
}

func wasiFdSeek(ctx uintptr, fd uint32, offset uint64, whence uint32, result uint32) uint32 {
	p := proc(ctx)
	obj, err := p.fd(fd)

	if err != nil {
		return wasiEbadf
	}

	if whence > 2 {
		return wasiEinval
	}

	s := obj.asSeeker()

	if s == nil {
		return wasiEspipe
	}

	at, err := s.seek(p, int64(offset), int(whence))

	if err != nil {
		return wasiErrno(err)
	}

	if !put64(guest(ctx), result, uint64(at)) {
		return wasiEfault
	}

	return wasiOK
}

func wasiFdSync(ctx uintptr, fd uint32) uint32 {
	p := proc(ctx)
	a, err := p.attrsAt(fd)

	if err == eBadf {
		return wasiEbadf
	}

	if err != nil {
		return wasiEinval
	}

	return wasiErrno(a.sync(p, false))
}

func filetype(mode uint32) byte {
	switch mode & sIfmt {
	case sIfblk:
		return 1
	case sIfchr:
		return 2
	case sIfdir:
		return 3
	case sIfreg:
		return 4
	case sIfsock:
		return 6
	case sIflnk:
		return 7
	}

	return 0
}

func wasiFdFdstatGet(ctx uintptr, fd, at uint32) uint32 {
	p := proc(ctx)
	obj, err := p.fileAt(fd)

	if err != nil {
		return wasiEbadf
	}

	b := span(guest(ctx), at, 24)

	if b == nil {
		return wasiEfault
	}

	var st Stat

	if err := obj.stat(p, &st); err != nil {
		return wasiErrno(err)
	}

	fl, err := obj.flags()

	if err != nil {
		return wasiErrno(err)
	}

	flags := uint16(0)

	if fl&oAppend != 0 {
		flags |= 1
	}

	if fl&oNonblock != 0 {
		flags |= 4
	}

	clear(b)
	b[0] = filetype(st.Mode)
	binary.LittleEndian.PutUint16(b[2:], flags)
	binary.LittleEndian.PutUint64(b[8:], ^uint64(0))
	binary.LittleEndian.PutUint64(b[16:], ^uint64(0))

	return wasiOK
}

func wasiFdFdstatSetFlags(ctx uintptr, fd, flags uint32) uint32 {
	p := proc(ctx)
	obj, err := p.fileAt(fd)

	if err != nil {
		return wasiEbadf
	}

	fl, err := obj.flags()

	if err != nil {
		return wasiErrno(err)
	}

	fl &^= oAppend | oNonblock

	if flags&1 != 0 {
		fl |= oAppend
	}

	if flags&4 != 0 {
		fl |= oNonblock
	}

	return wasiErrno(obj.setFlags(fl))
}

func nanos(t Timespec) uint64 {
	return uint64(t.Sec)*1e9 + uint64(t.Nsec)
}

func filestat(b []byte, st *Stat) {
	clear(b)
	binary.LittleEndian.PutUint64(b, st.Dev)
	binary.LittleEndian.PutUint64(b[8:], st.Ino)
	b[16] = filetype(st.Mode)
	binary.LittleEndian.PutUint64(b[24:], st.Nlink)
	binary.LittleEndian.PutUint64(b[32:], uint64(st.Size))
	binary.LittleEndian.PutUint64(b[40:], nanos(st.Atim))
	binary.LittleEndian.PutUint64(b[48:], nanos(st.Mtim))
	binary.LittleEndian.PutUint64(b[56:], nanos(st.Ctim))
}

func wasiFdFilestatGet(ctx uintptr, fd, at uint32) uint32 {
	p := proc(ctx)
	obj, err := p.fileAt(fd)

	if err != nil {
		return wasiEbadf
	}

	b := span(guest(ctx), at, 64)

	if b == nil {
		return wasiEfault
	}

	var st Stat

	if err := obj.stat(p, &st); err != nil {
		return wasiErrno(err)
	}

	filestat(b, &st)

	return wasiOK
}

func wasiFdFilestatSetSize(ctx uintptr, fd uint32, size uint64) uint32 {
	p := proc(ctx)
	a, err := p.attrsAt(fd)

	if err == eBadf {
		return wasiEbadf
	}

	if err != nil {
		return wasiEinval
	}

	return wasiErrno(a.truncate(p, int64(size)))
}

func wasiFdPrestatGet(ctx uintptr, fd, at uint32) uint32 {
	if int(fd) != proc(ctx).wasiRoot {
		return wasiEbadf
	}

	m := guest(ctx)

	if !put32(m, at, 0) || !put32(m, at+4, 1) {
		return wasiEfault
	}

	return wasiOK
}

func wasiFdPrestatDirName(ctx uintptr, fd, path, n uint32) uint32 {
	if int(fd) != proc(ctx).wasiRoot {
		return wasiEbadf
	}

	b := span(guest(ctx), path, uint64(n))

	if b == nil {
		return wasiEfault
	}

	if n < 1 {
		return wasiEinval
	}

	b[0] = '/'

	return wasiOK
}

func direntType(t byte) byte {
	switch t {
	case dtBlk:
		return 1
	case dtChr:
		return 2
	case dtDir:
		return 3
	case dtReg:
		return 4
	case dtSock:
		return 6
	case dtLnk:
		return 7
	}

	return 0
}

func wasiFdReaddir(ctx uintptr, fd, buf, n uint32, cookie uint64, used uint32) uint32 {
	p, m := proc(ctx), guest(ctx)
	obj, err := p.fd(fd)

	if err != nil {
		return wasiEbadf
	}

	out := span(m, buf, uint64(n))

	if out == nil || span(m, used, 4) == nil {
		return wasiEfault
	}

	d := obj.asDir()

	if d == nil {
		return wasiEnotdir
	}

	if s := obj.asSeeker(); s != nil {
		if _, err := s.seek(p, 0, 0); err != nil {
			return wasiErrno(err)
		}
	}

	index := uint64(0)
	written := 0
	raw := make([]byte, 64<<10)

	for written < len(out) {
		got, err := d.readdir(p, raw)

		if err != nil {
			return wasiErrno(err)
		}

		if got == 0 {
			break
		}

		for at := 0; at < got && written < len(out); {
			reclen := int(binary.LittleEndian.Uint16(raw[at+16:]))
			name := raw[at+19 : at+reclen]

			for k, c := range name {
				if c == 0 {
					name = name[:k]

					break
				}
			}

			if index >= cookie {
				var head [24]byte
				binary.LittleEndian.PutUint64(head[:], index+1)
				binary.LittleEndian.PutUint64(head[8:], binary.LittleEndian.Uint64(raw[at:]))
				binary.LittleEndian.PutUint32(head[16:], uint32(len(name)))
				head[20] = direntType(raw[at+18])
				written += copy(out[written:], head[:])
				written += copy(out[written:], name)
			}

			index++
			at += reclen
		}
	}

	put32(m, used, uint32(written))

	return wasiOK
}

func wasiPath(m []byte, path, n uint32) (string, uint32) {
	b := span(m, path, uint64(n))

	if b == nil {
		return "", wasiEfault
	}

	if n >= 4096 {
		return "", wasiEnametoolong
	}

	if n == 0 {
		return ".", wasiOK
	}

	return string(b), wasiOK
}

func dirfd(p *Process, fd uint32) (int32, uint32) {
	if _, err := p.fd(fd); err != nil {
		return 0, wasiEbadf
	}

	return int32(fd), wasiOK
}

func wasiPathFilestatGet(ctx uintptr, fd, flags, path, n, at uint32) uint32 {
	p, m := proc(ctx), guest(ctx)
	dir, e := dirfd(p, fd)

	if e != wasiOK {
		return e
	}

	name, e := wasiPath(m, path, n)

	if e != wasiOK {
		return e
	}

	b := span(m, at, 64)

	if b == nil {
		return wasiEfault
	}

	var st Stat

	if err := p.statAt(dir, name, flags&1 != 0, &st); err != nil {
		return wasiErrno(err)
	}

	filestat(b, &st)

	return wasiOK
}

func wasiPathOpen(ctx uintptr, fd, dirflags, path, n, oflags uint32, rights, inheriting uint64, fdflags, opened uint32) uint32 {
	p, m := proc(ctx), guest(ctx)
	dir, e := dirfd(p, fd)

	if e != wasiOK {
		return e
	}

	name, e := wasiPath(m, path, n)

	if e != wasiOK {
		return e
	}

	if span(m, opened, 4) == nil {
		return wasiEfault
	}

	read := rights&(1<<1) != 0 || rights&(1<<14) != 0
	write := rights&(1<<6) != 0 || rights&(1<<22) != 0
	flags := 0

	switch {
	case read && write:
		flags |= oRdwr
	case write:
		flags |= oWronly
	default:
		flags |= oRdonly
	}

	for bit, f := range map[uint32]int{1: oCreat, 2: oDirectory, 4: oExcl, 8: oTrunc} {
		if oflags&bit != 0 {
			flags |= f
		}
	}

	if fdflags&1 != 0 {
		flags |= oAppend
	}

	if fdflags&4 != 0 {
		flags |= oNonblock
	}

	if dirflags&1 == 0 {
		flags |= oNofollow
	}

	if err := p.descriptorFree(); err != nil {
		return wasiErrno(err)
	}

	mode := 0o666 &^ p.umask
	f, err := p.openAt(dir, name, flags, mode)

	if err == eIsdir && write && oflags&1 == 0 {
		f, err = p.openAt(dir, name, flags&^(oWronly|oRdwr)|oRdonly, mode)
	}

	if err != nil {
		return wasiErrno(err)
	}

	r, err := p.install(f, 0, true)

	if err != nil {
		return wasiErrno(err)
	}

	put32(m, opened, uint32(r))

	return wasiOK
}

func wasiPathCreateDirectory(ctx uintptr, fd, path, n uint32) uint32 {
	p := proc(ctx)
	dir, e := dirfd(p, fd)

	if e != wasiOK {
		return e
	}

	name, e := wasiPath(guest(ctx), path, n)

	if e != wasiOK {
		return e
	}

	return wasiErrno(p.mkdir(dir, name, 0o777))
}

func wasiPathRemoveDirectory(ctx uintptr, fd, path, n uint32) uint32 {
	return wasiRemove(ctx, fd, path, n, true)
}

func wasiPathUnlinkFile(ctx uintptr, fd, path, n uint32) uint32 {
	return wasiRemove(ctx, fd, path, n, false)
}

func wasiRemove(ctx uintptr, fd, path, n uint32, directory bool) uint32 {
	p := proc(ctx)
	dir, e := dirfd(p, fd)

	if e != wasiOK {
		return e
	}

	name, e := wasiPath(guest(ctx), path, n)

	if e != wasiOK {
		return e
	}

	return wasiErrno(p.remove(dir, name, directory))
}

func wasiPathRename(ctx uintptr, fd, path, n, fd2, path2, n2 uint32) uint32 {
	p, m := proc(ctx), guest(ctx)
	dir, e := dirfd(p, fd)
	dir2, e2 := dirfd(p, fd2)

	if e != wasiOK || e2 != wasiOK {
		return wasiEbadf
	}

	old, e := wasiPath(m, path, n)

	if e != wasiOK {
		return e
	}

	new, e := wasiPath(m, path2, n2)

	if e != wasiOK {
		return e
	}

	return wasiErrno(p.rename(dir, old, dir2, new))
}

func wasiPathReadlink(ctx uintptr, fd, path, n, buf, size, used uint32) uint32 {
	p, m := proc(ctx), guest(ctx)
	dir, e := dirfd(p, fd)

	if e != wasiOK {
		return e
	}

	name, e := wasiPath(m, path, n)

	if e != wasiOK {
		return e
	}

	if span(m, buf, uint64(size)) == nil || span(m, used, 4) == nil {
		return wasiEfault
	}

	r, err := p.readlinkAt(dir, name, buf, size)

	if err != nil {
		return wasiErrno(err)
	}

	put32(m, used, uint32(r))

	return wasiOK
}

func wasiPollOneoff(ctx uintptr, in, out, count, events uint32) uint32 {
	if count == 0 {
		return wasiEinval
	}

	p, m := proc(ctx), guest(ctx)
	subs := span(m, in, 48*uint64(count))
	evs := span(m, out, 32*uint64(count))

	if subs == nil || evs == nil || span(m, events, 4) == nil {
		return wasiEfault
	}

	ready := uint32(0)
	sleep := int64(-1)
	earliest := uint32(0)

	for i := range count {
		s := subs[48*i:]
		tag := s[8]

		if tag != 0 {
			e := evs[32*ready : 32*ready+32]

			clear(e)
			copy(e, s[:8])
			e[10] = tag
			ready++

			continue
		}

		timeout := binary.LittleEndian.Uint64(s[24:])

		if binary.LittleEndian.Uint16(s[40:])&1 != 0 {
			now, errno := clock(p, binary.LittleEndian.Uint32(s[16:]), false)

			if errno != wasiOK {
				return errno
			}

			timeout = max(timeout, now) - now
		}

		if sleep < 0 || int64(timeout) < sleep {
			sleep = int64(timeout)
			earliest = i
		}
	}

	if ready == 0 && sleep >= 0 {
		if p.sleep(time.Duration(sleep)) > 0 {
			return wasiEintr
		}

		e := evs[:32]

		clear(e)
		copy(e, subs[48*earliest:48*earliest+8])
		ready = 1
	}

	put32(m, events, ready)

	return wasiOK
}

func wasiRandomGet(ctx uintptr, buf, n uint32) uint32 {
	b := span(guest(ctx), buf, uint64(n))

	if b == nil {
		return wasiEfault
	}

	rand.Read(b)

	return wasiOK
}

func wasiSchedYield(ctx uintptr) uint32 {
	runtime.Gosched()

	return wasiOK
}

func wasiProcExit(ctx uintptr, code uint32) {
	proc(ctx).exitWith(int(code&0xff) << 8)
}
