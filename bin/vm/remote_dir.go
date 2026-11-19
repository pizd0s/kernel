//go:build linux && amd64

package main

import (
	"encoding/binary"
)

const openControlling = 4

type RemoteDir struct {
	node RemoteNode
	id   uint64

	opath bool
}

func (d *RemoteDir) ref() {
	d.node.ref()
}

func (d *RemoteDir) unref() {
	d.node.unref()
}

func (d *RemoteDir) asFile() File {
	return d
}

func (d *RemoteDir) asTransfer() Transfer {
	return nil
}

func (d *RemoteDir) asSeeker() Seeker {
	return d
}

func (d *RemoteDir) asDir() Dir {
	return d
}

func (d *RemoteDir) asTerminal() Terminal {
	return nil
}

func (d *RemoteDir) asWindowed() Windowed {
	return nil
}

func (d *RemoteDir) asLink() Link {
	return nil
}

func (d *RemoteDir) asAttrs() Attrs {
	return d
}

func (d *RemoteDir) asSocket() Socket {
	return nil
}

func (d *RemoteDir) asIoctl() Ioctl {
	return nil
}

func (d *RemoteDir) asLocker() Locker {
	if d.opath {
		return nil
	}

	return d
}

func (d *RemoteDir) asEpoll() *Epoll {
	return nil
}

func (d *RemoteDir) asMemfd() *MemfdFile {
	return nil
}

func (d *RemoteDir) asPair() *PairEnd {
	return nil
}

func (d *RemoteDir) asListener() *UnixListener {
	return nil
}

func (d *RemoteDir) asConsole() *ConsoleEnd {
	return nil
}

func (d *RemoteDir) asVFS() *RemoteDir {
	return d
}

func (d *RemoteDir) asCall() *CallEnd {
	return nil
}

func (d *RemoteDir) stat(p *Process, st *Stat) error {
	return d.node.stat(p, st)
}

func (d *RemoteDir) poll(events int16) int16 {
	return d.node.poll(events)
}

func (d *RemoteDir) subscribe(s *Subscription, events int16) {
	d.node.subscribe(s, events)
}

func (d *RemoteDir) unsubscribe(s *Subscription) {
	d.node.unsubscribe(s)
}

func (d *RemoteDir) flags() (int, error) {
	return d.node.flags()
}

func (d *RemoteDir) setFlags(fl int) error {
	return d.node.setFlags(fl)
}

func (d *RemoteDir) truncate(p *Process, size int64) error {
	return d.node.truncate(p, size)
}

func (d *RemoteDir) sync(p *Process, data bool) error {
	return d.node.sync(p, data)
}

func (d *RemoteDir) chmod(p *Process, mode uint32) error {
	return d.node.chmod(p, mode)
}

func (d *RemoteDir) chown(p *Process, uid, gid int) error {
	return d.node.chown(p, uid, gid)
}

func (d *RemoteDir) utimes(p *Process, ts []Timespec) error {
	return d.node.utimes(p, ts)
}

func (d *RemoteDir) statfs(p *Process, st *Statfs) error {
	return d.node.statfs(p, st)
}

func (d *RemoteDir) flock(p *Process, op int) error {
	return d.node.flock(p, op)
}

func (d *RemoteDir) recordLock(p *Process, cmd int, lk *Flock) error {
	return d.node.recordLock(p, cmd, lk)
}

func (d *RemoteDir) readdir(p *Process, b []byte) (int, error) {
	var after [8]byte
	binary.LittleEndian.PutUint64(after[:], d.node.after)

	pk, err := d.node.on(p, rReaddir, 0, after[:])

	if err != nil {
		return 0, err
	}

	data := pk.data
	n := 0
	le := binary.LittleEndian

	for len(data) >= 17 {
		ino, pos := le.Uint64(data), le.Uint64(data[8:])
		kind := data[16]
		end := 17

		for end < len(data) && data[end] != 0 {
			end++
		}

		name := data[17:end]
		reclen := (19 + len(name) + 1 + 7) &^ 7

		if n+reclen > len(b) {
			break
		}

		clear(b[n : n+reclen])
		le.PutUint64(b[n:], ino)
		le.PutUint64(b[n+8:], pos)
		le.PutUint16(b[n+16:], uint16(reclen))
		b[n+18] = kind
		copy(b[n+19:], name)
		n += reclen
		d.node.after = pos
		data = data[min(end+1, len(data)):]
	}

	return n, nil
}

func (d *RemoteDir) seek(p *Process, off int64, whence int) (int64, error) {
	if off != 0 || whence != 0 {
		return 0, eInval
	}

	d.node.after = 0

	return 0, nil
}

func flagOf(follow bool) uint32 {
	if follow {
		return 0
	}

	return atSymlinkNofollow
}

func (d *RemoteDir) openPath(p *Process, path string, flags int, mode uint32) (Object, error) {
	if p.chaosFail("openat") {
		return nil, eIo
	}

	body := make([]byte, 4, 4+len(path))

	binary.LittleEndian.PutUint32(body, mode)

	pk, err := d.node.vfs(p, d.id, vOpen, uint32(flags), append(body, path...))

	if err != nil {
		return nil, err
	}

	if pk.arg&openControlling != 0 {
		dropFiles(pk.files)

		return p.openControlling(flags)
	}

	if len(pk.files) == 0 {
		var st Stat

		if err := decodeStat(pk.data, &st); err != nil || st.Mode&sIfmt != sIfifo {
			return nil, eIo
		}

		return p.openFifo(&st, flags)
	}

	f, err := p.opened(&pk)

	if err != nil {
		return nil, err
	}

	if flags&oDirectory != 0 && f.asDir() == nil {
		f.unref()

		return nil, eNotdir
	}

	if t := f.asTerminal(); t != nil && flags&oNoctty == 0 {
		p.acquire(t)
	}

	return f, nil
}

func (d *RemoteDir) statPath(p *Process, path string, follow bool, st *Stat) error {
	if p.chaosFail("file-stat") {
		return eIo
	}

	arg := uint32(0)

	if follow {
		arg = 1
	}

	pk, err := d.node.vfs(p, d.id, vStat, arg, []byte(path))

	if err != nil {
		return err
	}

	return decodeStat(pk.data, st)
}

func (d *RemoteDir) readlinkPath(p *Process, path string) (string, error) {
	pk, err := d.node.vfs(p, d.id, vReadlink, 0, []byte(path))

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

func (d *RemoteDir) mkdirPath(p *Process, path string, mode uint32) error {
	_, err := d.node.vfs(p, d.id, vMkdir, mode, []byte(path))

	return err
}

func (d *RemoteDir) mknodPath(p *Process, path string, mode uint32) error {
	_, err := d.node.vfs(p, d.id, vMknod, mode, []byte(path))

	return err
}

func (d *RemoteDir) symlinkPath(p *Process, target, path string) error {
	_, err := d.node.vfs(p, d.id, vSymlink, 0, append(append([]byte(target), 0), path...))

	return err
}

func (d *RemoteDir) unlinkPath(p *Process, path string, dir bool) error {
	flags := uint32(0)

	if dir {
		flags = atRemovedir
	}

	_, err := d.node.vfs(p, d.id, vUnlink, flags, []byte(path))

	return err
}

func twoPaths(to Dir, old, new string) ([]byte, error) {
	h := to.asVFS()

	if h == nil {
		return nil, eXdev
	}

	body := make([]byte, 8, 8+len(old)+1+len(new))

	binary.LittleEndian.PutUint64(body, h.id)

	return append(append(append(body, old...), 0), new...), nil
}

func (d *RemoteDir) renamePath(p *Process, old string, to Dir, new string) error {
	body, err := twoPaths(to, old, new)

	if err == nil {
		_, err = d.node.vfs(p, d.id, vRename, 0, body)
	}

	return err
}

func (d *RemoteDir) linkPath(p *Process, old string, to Dir, new string, follow bool) error {
	body, err := twoPaths(to, old, new)
	arg := uint32(0)

	if follow {
		arg = 1
	}

	if err == nil {
		_, err = d.node.vfs(p, d.id, vLink, arg, body)
	}

	return err
}

func (d *RemoteDir) chmodPath(p *Process, path string, mode uint32, follow bool) error {
	body := make([]byte, 4, 4+len(path))

	binary.LittleEndian.PutUint32(body, flagOf(follow))

	_, err := d.node.vfs(p, d.id, vChmod, mode, append(body, path...))

	return err
}

func (d *RemoteDir) chownPath(p *Process, path string, uid, gid int, follow bool) error {
	body := make([]byte, 12, 12+len(path))

	binary.LittleEndian.PutUint32(body, flagOf(follow))
	binary.LittleEndian.PutUint32(body[4:], uint32(uid))
	binary.LittleEndian.PutUint32(body[8:], uint32(gid))

	_, err := d.node.vfs(p, d.id, vChown, 0, append(body, path...))

	return err
}

func (d *RemoteDir) utimesPath(p *Process, path string, ts []Timespec, follow bool) error {
	when, times := whens(ts)
	body := make([]byte, 4, 4+len(times)+len(path))

	binary.LittleEndian.PutUint32(body, flagOf(follow))

	_, err := d.node.vfs(p, d.id, vUtimes, when, append(append(body, times...), path...))

	return err
}

func (d *RemoteDir) truncatePath(p *Process, path string, size int64) error {
	body := make([]byte, 8, 8+len(path))

	binary.LittleEndian.PutUint64(body, uint64(size))

	_, err := d.node.vfs(p, d.id, vTruncate, 0, append(body, path...))

	return err
}

func (d *RemoteDir) statfsPath(p *Process, path string, st *Statfs) error {
	pk, err := d.node.vfs(p, d.id, vStatfs, 0, []byte(path))

	if err != nil {
		return err
	}

	return decodeStatfs(pk.data, st)
}

func (d *RemoteDir) realpathOf(p *Process, path string) (string, error) {
	pk, err := d.node.vfs(p, d.id, vRealpath, 0, []byte(path))

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

func (d *RemoteDir) path(p *Process) (string, error) {
	pk, err := d.node.vfs(p, d.id, vPath, 0, nil)

	if err != nil {
		return "", err
	}

	return string(pk.data), nil
}

func (d *RemoteDir) mountPath(p *Process, target, source string, conn Object) error {
	body := append(append([]byte(target), 0), source...)

	if conn != nil {
		_, err := d.node.vfs(p, d.id, vMount, 0, body, conn)

		return err
	}

	_, err := d.node.vfs(p, d.id, vMount, 0, body)

	return err
}

func (d *RemoteDir) bindPath(p *Process, path string, mode uint32, held Object) error {
	_, err := d.node.vfs(p, d.id, vBind, mode, []byte(path), held)

	return err
}

func (d *RemoteDir) connectPath(p *Process, path string) (Object, error) {
	pk, err := d.node.vfs(p, d.id, vConnect, 0, []byte(path))

	if err != nil {
		return nil, err
	}

	if len(pk.files) == 0 {
		return nil, eConnrefused
	}

	return pk.files[0], nil
}

func (d *RemoteDir) unmountPath(p *Process, path string, flags uint32) error {
	_, err := d.node.vfs(p, d.id, vUnmount, flags, []byte(path))

	return err
}
