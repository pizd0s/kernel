//go:build linux && amd64

package main

import (
	"encoding/binary"
	"maps"
	"slices"
	"strconv"
)

const (
	dbmMagic   = "PZDBM1\n\x00"
	dbmHeader  = 16
	dbmDeleted = ^uint32(0)
	dbmLargest = 1 << 26
	dbmCompact = 1 << 16
)

type Dbm struct {
	fd        uint32
	path      string
	writable  bool
	failed    bool
	dev, ino  uint64
	scanned   int64
	dead      int64
	index     map[string][3]int64
	keys      []string
	next      int
	key       uint32
	keyRoom   uint32
	value     uint32
	valueRoom uint32
}

func (d *Dbm) copy() *Dbm {
	c := *d

	c.index, c.keys = maps.Clone(d.index), slices.Clone(d.keys)

	return &c
}

func (d *Dbm) reset() {
	d.index, d.scanned, d.dead, d.keys, d.next = map[string][3]int64{}, 0, 0, nil, 0
}

func (d *Dbm) load(p *Process) error {
	o, err := p.fd(d.fd)

	if err != nil {
		return err
	}

	var st Stat

	if err := p.fileStat(o, &st); err != nil {
		return err
	}

	d.dev, d.ino = st.Dev, st.Ino
	d.reset()

	if st.Size == 0 && d.writable {
		head := make([]byte, dbmHeader)

		copy(head, dbmMagic)

		if _, err := p.fileWrite(o, bytesFile(head), dbmHeader, IoMode{}); err != nil {
			return err
		}
	}

	return d.sync(p)
}

func (d *Dbm) reopen(p *Process) error {
	flags := uint32(O_RDONLY | O_CLOEXEC)

	if d.writable {
		flags = O_RDWR | O_APPEND | O_CLOEXEC
	}

	fd, err := p.open(AT_FDCWD, d.path, flags, 0)

	if err != nil {
		return err
	}

	p.closeFd(int(d.fd))
	d.fd = uint32(fd)

	return d.load(p)
}

func (d *Dbm) sync(p *Process) error {
	var st Stat

	if err := p.statPath(d.path, true, &st); err == nil && (st.Dev != d.dev || st.Ino != d.ino) {
		return d.reopen(p)
	}

	o, err := p.fd(d.fd)

	if err != nil {
		return err
	}

	if err := p.fileStat(o, &st); err != nil {
		return err
	}

	if st.Size < d.scanned {
		d.reset()
	}

	if d.scanned == 0 {
		if st.Size < dbmHeader {
			return nil
		}

		head := make([]byte, dbmHeader)

		if n, err := p.filePread(o, bytesFile(head), dbmHeader, 0); err != nil || n != dbmHeader || string(head[:len(dbmMagic)]) != dbmMagic {
			return EINVAL
		}

		d.scanned = dbmHeader
	}

	if st.Size == d.scanned {
		return nil
	}

	tail := make([]byte, st.Size-d.scanned)
	n, err := p.filePread(o, bytesFile(tail), len(tail), d.scanned)

	if err != nil {
		return err
	}

	d.scanned += int64(d.apply(tail[:n], d.scanned))

	return nil
}

func (d *Dbm) apply(b []byte, base int64) int {
	at := 0

	for at+8 <= len(b) {
		klen, vlen := binary.LittleEndian.Uint32(b[at:]), binary.LittleEndian.Uint32(b[at+4:])
		size := 8 + int(klen)

		if vlen != dbmDeleted {
			size += int(vlen)
		}

		if klen > dbmLargest || vlen != dbmDeleted && vlen > dbmLargest || at+size > len(b) {
			break
		}

		key := string(b[at+8 : at+8+int(klen)])

		if old, ok := d.index[key]; ok {
			d.dead += 8 + int64(len(key)) + old[1]
		}

		if vlen == dbmDeleted {
			delete(d.index, key)
			d.dead += int64(size)
		} else {
			d.index[key] = [3]int64{base + int64(at+8) + int64(klen), int64(vlen), base + int64(at)}
		}

		at += size
	}

	return at
}

func (d *Dbm) record(key, value []byte, deleted bool) []byte {
	b := make([]byte, 8, 8+len(key)+len(value))
	vlen := uint32(len(value))

	if deleted {
		vlen = dbmDeleted
	}

	binary.LittleEndian.PutUint32(b, uint32(len(key)))
	binary.LittleEndian.PutUint32(b[4:], vlen)

	return append(append(b, key...), value...)
}

func (d *Dbm) append(p *Process, key, value []byte, deleted bool) error {
	o, err := p.fd(d.fd)

	if err != nil {
		return err
	}

	b := d.record(key, value, deleted)

	if n, err := p.fileWrite(o, bytesFile(b), len(b), IoMode{}); err != nil || n != len(b) {
		return dbmFault(err)
	}

	return d.sync(p)
}

func (d *Dbm) read(p *Process, at, n int64) ([]byte, error) {
	o, err := p.fd(d.fd)

	if err != nil {
		return nil, err
	}

	b := make([]byte, n)

	if got, err := p.filePread(o, bytesFile(b), int(n), at); err != nil || got != int(n) {
		return nil, dbmFault(err)
	}

	return b, nil
}

func (d *Dbm) ordered() []string {
	keys := slices.Collect(maps.Keys(d.index))

	slices.SortFunc(keys, func(a, b string) int {
		return int(d.index[a][2] - d.index[b][2])
	})

	return keys
}

func (d *Dbm) compact(p *Process) error {
	if d.scanned < dbmCompact || 2*d.dead < d.scanned {
		return nil
	}

	o, err := p.fd(d.fd)

	if err != nil {
		return err
	}

	var st Stat

	if err := p.fileStat(o, &st); err != nil {
		return err
	}

	data := make([]byte, dbmHeader)

	copy(data, dbmMagic)

	for _, key := range d.ordered() {
		v := d.index[key]
		value, err := d.read(p, v[0], v[1])

		if err != nil {
			return err
		}

		data = append(data, d.record([]byte(key), value, false)...)
	}

	tmp := d.path + ".tmp." + strconv.Itoa(int(p.pid))
	fd, err := p.open(AT_FDCWD, tmp, O_WRONLY|O_CREAT|O_EXCL|O_CLOEXEC, st.Mode&0o7777)

	if err != nil {
		return err
	}

	t, _ := p.fd(uint32(fd))
	n, err := p.fileWrite(t, bytesFile(data), len(data), IoMode{})

	p.closeFd(fd)

	if err == nil && n == len(data) {
		err = p.rename(AT_FDCWD, tmp, AT_FDCWD, d.path)
	}

	if err != nil || n != len(data) {
		p.remove(AT_FDCWD, tmp, false)

		return dbmFault(err)
	}

	return d.reopen(p)
}

func dbmFault(err error) error {
	if err != nil {
		return err
	}

	return EIO
}
