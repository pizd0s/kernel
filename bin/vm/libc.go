//go:build linux && amd64

package main

func libc__kernel_start(errnoAt, environAt, argcAt, argvAt, tznameAt, timezoneAt, daylightAt, herrnoAt uint32) {
	proc().libc__kernel_start(errnoAt, environAt, argcAt, argvAt, tznameAt, timezoneAt, daylightAt, herrnoAt)
}

func libc__kernel_program(buf, size uint32) uint32 {
	return proc().libc__kernel_program(buf, size)
}

func libcRead(fd, buf, n uint32) uint32 {
	return proc().libcRead(fd, buf, n)
}

func libcWrite(fd, buf, n uint32) uint32 {
	return proc().libcWrite(fd, buf, n)
}

func libcReadv(fd, iovs, iovsSize, count uint32) uint32 {
	p := proc()

	p.sized("readv", iovsSize, sizeofIovec)

	return p.libcReadv(fd, iovs, count)
}

func libcWritev(fd, iovs, iovsSize, count uint32) uint32 {
	p := proc()

	p.sized("writev", iovsSize, sizeofIovec)

	return p.libcWritev(fd, iovs, count)
}

func libcPreadv(fd, iovs, iovsSize, count uint32, off uint64) uint32 {
	p := proc()

	p.sized("preadv", iovsSize, sizeofIovec)

	return p.libcPreadv(fd, iovs, count, off)
}

func libcPwritev(fd, iovs, iovsSize, count uint32, off uint64) uint32 {
	p := proc()

	p.sized("pwritev", iovsSize, sizeofIovec)

	return p.libcPwritev(fd, iovs, count, off)
}

func libcPread(fd, buf, n uint32, off uint64) uint32 {
	return proc().libcPread(fd, buf, n, off)
}

func libcPwrite(fd, buf, n uint32, off uint64) uint32 {
	return proc().libcPwrite(fd, buf, n, off)
}

func libcCopy_file_range(in, offIn, out, offOut, n, flags uint32) uint32 {
	return proc().libcCopy_file_range(in, offIn, out, offOut, n, flags)
}

func libcFdatasync(fd uint32) uint32 {
	return proc().libcFdatasync(fd)
}

func libcSync() {
	proc().libcSync()
}

func libcSyncfs(fd uint32) uint32 {
	return proc().libcSyncfs(fd)
}

func libcFlock(fd, op uint32) uint32 {
	return proc().libcFlock(fd, op)
}

func libcLockf(fd, cmd uint32, n uint64) uint32 {
	return proc().libcLockf(fd, cmd, n)
}

func libcPosix_fadvise(fd uint32, off, n uint64, advice uint32) uint32 {
	return proc().libcPosix_fadvise(fd, off, n, advice)
}

func libcPosix_fallocate(fd uint32, off, n uint64) uint32 {
	return proc().libcPosix_fallocate(fd, off, n)
}

func libcSelect(n, r, rSize, w, wSize, e, eSize, tv, tvSize uint32) uint32 {
	p := proc()

	p.sized("select", rSize, sizeofFdSet, wSize, sizeofFdSet, eSize, sizeofFdSet, tvSize, sizeofTimeval)

	return p.libcSelect(n, r, w, e, tv)
}

func libcPselect(n, r, rSize, w, wSize, e, eSize, ts, tsSize, mask, maskSize uint32) uint32 {
	p := proc()

	p.sized("pselect", rSize, sizeofFdSet, wSize, sizeofFdSet, eSize, sizeofFdSet, tsSize, sizeofTimespec, maskSize, sizeofSigset)

	return p.libcPselect(n, r, w, e, ts, mask)
}

func libcLseek(fd uint32, off uint64, whence uint32) uint64 {
	return proc().libcLseek(fd, off, whence)
}

func libcClose(fd uint32) uint32 {
	return proc().libcClose(fd)
}

func libcDup(fd uint32) uint32 {
	return proc().libcDup(fd)
}

func libcDup2(old, new uint32) uint32 {
	return proc().libcDup2(old, new)
}

func libcDup3(old, new, flags uint32) uint32 {
	return proc().libcDup3(old, new, flags)
}

func libcPipe(fds uint32) uint32 {
	return proc().libcPipe(fds)
}

func libcPipe2(fds, flags uint32) uint32 {
	return proc().libcPipe2(fds, flags)
}

func libcTcdrain(fd uint32) uint32 {
	return proc().libcTcdrain(fd)
}

func libcTcsendbreak(fd, duration uint32) uint32 {
	return proc().libcTcsendbreak(fd, duration)
}

func libcTcflow(fd, action uint32) uint32 {
	return proc().libcTcflow(fd, action)
}

func libcTcflush(fd, queue uint32) uint32 {
	return proc().libcTcflush(fd, queue)
}

func libcTcgetsid(fd uint32) uint32 {
	return proc().libcTcgetsid(fd)
}

func libcCfgetospeed(at, atSize uint32) uint32 {
	p := proc()

	p.sized("cfgetospeed", atSize, sizeofTermios)

	return p.libcCfgetospeed(at)
}

func libcCfgetispeed(at, atSize uint32) uint32 {
	p := proc()

	p.sized("cfgetispeed", atSize, sizeofTermios)

	return p.libcCfgetispeed(at)
}

func libcCfsetospeed(at, atSize, speed uint32) uint32 {
	p := proc()

	p.sized("cfsetospeed", atSize, sizeofTermios)

	return p.libcCfsetospeed(at, speed)
}

func libcCfsetspeed(at, atSize, speed uint32) uint32 {
	p := proc()

	p.sized("cfsetspeed", atSize, sizeofTermios)

	return p.libcCfsetspeed(at, speed)
}

func libcCfsetispeed(at, atSize, speed uint32) uint32 {
	p := proc()

	p.sized("cfsetispeed", atSize, sizeofTermios)

	return p.libcCfsetispeed(at, speed)
}

func libcFsync(fd uint32) uint32 {
	return proc().libcFsync(fd)
}

func libcFtruncate(fd uint32, size uint64) uint32 {
	return proc().libcFtruncate(fd, size)
}

func libcFstat(fd, at, atSize uint32) uint32 {
	p := proc()

	p.sized("fstat", atSize, sizeofStat)

	return p.libcFstat(fd, at)
}

func libcFchmod(fd, mode uint32) uint32 {
	return proc().libcFchmod(fd, mode)
}

func libcFchown(fd, uid, gid uint32) uint32 {
	return proc().libcFchown(fd, uid, gid)
}

func libcFutimes(fd, times, timesSize uint32) uint32 {
	p := proc()

	p.sized("futimes", timesSize, sizeofTimeval)

	return p.libcFutimes(fd, times)
}

func libcFutimens(fd, times, timesSize uint32) uint32 {
	p := proc()

	p.sized("futimens", timesSize, sizeofTimespec)

	return p.libcFutimens(fd, times)
}

func libcFcntl(fd, cmd, va, size uint32) uint32 {
	p := proc()

	p.sized("fcntl", size, fcntlSize(cmd))

	return p.libcFcntl(fd, cmd, va)
}

func libcIoctl(fd, req, va, size uint32) uint32 {
	p := proc()

	p.sized("ioctl", size, ioctlSize(req))

	return p.libcIoctl(fd, req, va)
}

func libcIsatty(fd uint32) uint32 {
	return proc().libcIsatty(fd)
}

func libcTcgetattr(fd, at, atSize uint32) uint32 {
	p := proc()

	p.sized("tcgetattr", atSize, sizeofTermios)

	return p.libcTcgetattr(fd, at)
}

func libcTcsetattr(fd, action, at, atSize uint32) uint32 {
	p := proc()

	p.sized("tcsetattr", atSize, sizeofTermios)

	return p.libcTcsetattr(fd, action, at)
}

func libcCfmakeraw(at, atSize uint32) {
	p := proc()

	p.sized("cfmakeraw", atSize, sizeofTermios)

	p.libcCfmakeraw(at)
}

func libcPoll(at, atSize, n, timeout uint32) uint32 {
	p := proc()

	p.sized("poll", atSize, sizeofPollfd)

	return p.libcPoll(at, n, timeout)
}

func libcPpoll(at, atSize, n, ts, tsSize, mask, maskSize uint32) uint32 {
	p := proc()

	p.sized("ppoll", atSize, sizeofPollfd, tsSize, sizeofTimespec, maskSize, sizeofSigset)

	return p.libcPpoll(at, n, ts, mask)
}

func libcOpen(path, flags, va uint32) uint32 {
	return proc().libcOpen(path, flags, va)
}

func libcOpenat(dirfd, path, flags, va uint32) uint32 {
	return proc().libcOpenat(dirfd, path, flags, va)
}

func libcStat(path, at, atSize uint32) uint32 {
	p := proc()

	p.sized("stat", atSize, sizeofStat)

	return p.libcStat(path, at)
}

func libcLstat(path, at, atSize uint32) uint32 {
	p := proc()

	p.sized("lstat", atSize, sizeofStat)

	return p.libcLstat(path, at)
}

func libcFstatat(dirfd, path, at, atSize, flags uint32) uint32 {
	p := proc()

	p.sized("fstatat", atSize, sizeofStat)

	return p.libcFstatat(dirfd, path, at, flags)
}

func libcAccess(path, mode uint32) uint32 {
	return proc().libcAccess(path, mode)
}

func libcFaccessat(dirfd, path, mode, flags uint32) uint32 {
	return proc().libcFaccessat(dirfd, path, mode, flags)
}

func libcTruncate(path uint32, size uint64) uint32 {
	return proc().libcTruncate(path, size)
}

func libcStatvfs(path, at, atSize uint32) uint32 {
	p := proc()

	p.sized("statvfs", atSize, sizeofStatvfs)

	return p.libcStatvfs(path, at)
}

func libcFstatvfs(fd, at, atSize uint32) uint32 {
	p := proc()

	p.sized("fstatvfs", atSize, sizeofStatvfs)

	return p.libcFstatvfs(fd, at)
}

func libcFchdir(fd uint32) uint32 {
	return proc().libcFchdir(fd)
}

func libcAttach(fd uint32) uint32 {
	return proc().libcAttach(fd)
}

func libcWatchdir(fd, flags uint32) uint32 {
	return proc().libcWatchdir(fd, flags)
}

func libcChroot(path uint32) uint32 {
	return proc().libcChroot(path)
}

func libcChdir(path uint32) uint32 {
	return proc().libcChdir(path)
}

func libcGetcwd(buf, size uint32) uint32 {
	return proc().libcGetcwd(buf, size)
}

func libcMkdir(path, mode uint32) uint32 {
	return proc().libcMkdir(path, mode)
}

func libcMkdirat(dirfd, path, mode uint32) uint32 {
	return proc().libcMkdirat(dirfd, path, mode)
}

func libcRmdir(path uint32) uint32 {
	return proc().libcRmdir(path)
}

func libcUnlink(path uint32) uint32 {
	return proc().libcUnlink(path)
}

func libcUnlinkat(dirfd, path, flags uint32) uint32 {
	return proc().libcUnlinkat(dirfd, path, flags)
}

func libcMount(source, target, fstype, flags, data uint32) uint32 {
	return proc().libcMount(source, target, fstype, flags, data)
}

func libcUmount(target uint32) uint32 {
	return proc().libcUmount(target)
}

func libcUmount2(target, flags uint32) uint32 {
	return proc().libcUmount2(target, flags)
}

func libcRemove(path uint32) uint32 {
	return proc().libcRemove(path)
}

func libcRename(old, new uint32) uint32 {
	return proc().libcRename(old, new)
}

func libcRenameat(olddir, old, newdir, new uint32) uint32 {
	return proc().libcRenameat(olddir, old, newdir, new)
}

func libcLink(old, new uint32) uint32 {
	return proc().libcLink(old, new)
}

func libcLinkat(olddir, old, newdir, new, flags uint32) uint32 {
	return proc().libcLinkat(olddir, old, newdir, new, flags)
}

func libcSymlink(target, path uint32) uint32 {
	return proc().libcSymlink(target, path)
}

func libcSymlinkat(target, dirfd, path uint32) uint32 {
	return proc().libcSymlinkat(target, dirfd, path)
}

func libcReadlink(path, buf, size uint32) uint32 {
	return proc().libcReadlink(path, buf, size)
}

func libcReadlinkat(dirfd, path, buf, size uint32) uint32 {
	return proc().libcReadlinkat(dirfd, path, buf, size)
}

func libcCanonicalize_file_name(path uint32) uint32 {
	return proc().libcCanonicalize_file_name(path)
}

func libcGet_current_dir_name() uint32 {
	return proc().libcGet_current_dir_name()
}

func libcCreat(path, mode uint32) uint32 {
	return proc().libcCreat(path, mode)
}

func libcEaccess(path, mode uint32) uint32 {
	return proc().libcEaccess(path, mode)
}

func libcEuidaccess(path, mode uint32) uint32 {
	return proc().libcEuidaccess(path, mode)
}

func libcStatfs(path, at, atSize uint32) uint32 {
	p := proc()

	p.sized("statfs", atSize, sizeofStatfs)

	return p.libcStatfs(path, at)
}

func libcFstatfs(fd, at, atSize uint32) uint32 {
	p := proc()

	p.sized("fstatfs", atSize, sizeofStatfs)

	return p.libcFstatfs(fd, at)
}

func libcAcct(path uint32) uint32 {
	return proc().libcAcct(path)
}

func libcRealpath(path, resolved uint32) uint32 {
	return proc().libcRealpath(path, resolved)
}

func libcChmod(path, mode uint32) uint32 {
	return proc().libcChmod(path, mode)
}

func libcFchmodat(dirfd, path, mode, flags uint32) uint32 {
	return proc().libcFchmodat(dirfd, path, mode, flags)
}

func libcChown(path, uid, gid uint32) uint32 {
	return proc().libcChown(path, uid, gid)
}

func libcLchown(path, uid, gid uint32) uint32 {
	return proc().libcLchown(path, uid, gid)
}

func libcFchownat(dirfd, path, uid, gid, flags uint32) uint32 {
	return proc().libcFchownat(dirfd, path, uid, gid, flags)
}

func libcMknod(path, mode uint32, dev uint64) uint32 {
	return proc().libcMknod(path, mode, dev)
}

func libcMknodat(dirfd, path, mode uint32, dev uint64) uint32 {
	return proc().libcMknodat(dirfd, path, mode, dev)
}

func libcMkfifo(path, mode uint32) uint32 {
	return proc().libcMkfifo(path, mode)
}

func libcMkfifoat(dirfd, path, mode uint32) uint32 {
	return proc().libcMkfifoat(dirfd, path, mode)
}

func libcUtimes(path, times, timesSize uint32) uint32 {
	p := proc()

	p.sized("utimes", timesSize, sizeofTimeval)

	return p.libcUtimes(path, times)
}

func libcLutimes(path, times, timesSize uint32) uint32 {
	p := proc()

	p.sized("lutimes", timesSize, sizeofTimeval)

	return p.libcLutimes(path, times)
}

func libcUtime(path, times, timesSize uint32) uint32 {
	p := proc()

	p.sized("utime", timesSize, sizeofUtimbuf)

	return p.libcUtime(path, times)
}

func libcUtimensat(dirfd, path, times, timesSize, flags uint32) uint32 {
	p := proc()

	p.sized("utimensat", timesSize, sizeofTimespec)

	return p.libcUtimensat(dirfd, path, times, flags)
}

func libcDirfd(dir uint32) uint32 {
	return proc().libcDirfd(dir)
}

func libcReaddir_r(dir, entry, entrySize, result, resultSize uint32) uint32 {
	p := proc()

	p.sized("readdir_r", entrySize, sizeofDirent, resultSize, sizeofDirent)

	return p.libcReaddir_r(dir, entry, result)
}

func libcTelldir(dir uint32) uint32 {
	return proc().libcTelldir(dir)
}

func libcSeekdir(dir, at uint32) {
	proc().libcSeekdir(dir, at)
}

func libcAlphasort(a, aSize, b, bSize uint32) uint32 {
	p := proc()

	p.sized("alphasort", aSize, sizeofDirent, bSize, sizeofDirent)

	return p.libcAlphasort(a, b)
}

func libcVersionsort(a, aSize, b, bSize uint32) uint32 {
	p := proc()

	p.sized("versionsort", aSize, sizeofDirent, bSize, sizeofDirent)

	return p.libcVersionsort(a, b)
}

func libcMkstemp(template uint32) uint32 {
	return proc().libcMkstemp(template)
}

func libcMkostemp(template, flags uint32) uint32 {
	return proc().libcMkostemp(template, flags)
}

func libcMkstemps(template, suffix uint32) uint32 {
	return proc().libcMkstemps(template, suffix)
}

func libcMkdtemp(template uint32) uint32 {
	return proc().libcMkdtemp(template)
}

func libcMktemp(template uint32) uint32 {
	return proc().libcMktemp(template)
}

func libcOpendir(path uint32) uint32 {
	return proc().libcOpendir(path)
}

func libcFdopendir(fd uint32) uint32 {
	return proc().libcFdopendir(fd)
}

func libcRewinddir(dir uint32) {
	proc().libcRewinddir(dir)
}

func libcReaddir(dir, size uint32) uint32 {
	p := proc()

	p.sized("readdir", size, sizeofDirent)

	return p.libcReaddir(dir)
}

func libcClosedir(dir uint32) uint32 {
	return proc().libcClosedir(dir)
}

func libcMalloc(n uint32) uint32 {
	return proc().libcMalloc(n)
}

func libcFree(ptr uint32) {
	proc().libcFree(ptr)
}

func libcCalloc(n, size uint32) uint32 {
	return proc().libcCalloc(n, size)
}

func libcRealloc(ptr, n uint32) uint32 {
	return proc().libcRealloc(ptr, n)
}

func libcMallopt(param, value uint32) uint32 {
	return proc().libcMallopt(param, value)
}

func libcGetpagesize() uint32 {
	return proc().libcGetpagesize()
}

func libcMmap(addr, n, prot, flags, fd uint32, off uint64) uint32 {
	return proc().libcMmap(addr, n, prot, flags, fd, off)
}

func libcMemfd_create(addr, n, flags uint32) uint32 {
	return proc().libcMemfd_create(addr, n, flags)
}

func libcMunmap(addr, n uint32) uint32 {
	return proc().libcMunmap(addr, n)
}

func libcMsync(addr, n, flags uint32) uint32 {
	return proc().libcMsync(addr, n, flags)
}

func libcMadvise(addr, n, advice uint32) uint32 {
	return proc().libcMadvise(addr, n, advice)
}

func libcMprotect(addr, n, prot uint32) uint32 {
	return proc().libcMprotect(addr, n, prot)
}

func libcMlock(addr, n uint32) uint32 {
	return proc().libcMlock(addr, n)
}

func libcMunlock(addr, n uint32) uint32 {
	return proc().libcMunlock(addr, n)
}

func libcMlockall(flags uint32) uint32 {
	return proc().libcMlockall(flags)
}

func libcMunlockall() uint32 {
	return proc().libcMunlockall()
}

func libcMemalign(align, n uint32) uint32 {
	return proc().libcMemalign(align, n)
}

func libcAligned_alloc(align, n uint32) uint32 {
	return proc().libcAligned_alloc(align, n)
}

func libcValloc(n uint32) uint32 {
	return proc().libcValloc(n)
}

func libcPosix_memalign(out, align, n uint32) uint32 {
	return proc().libcPosix_memalign(out, align, n)
}

func libcMalloc_usable_size(ptr uint32) uint32 {
	return proc().libcMalloc_usable_size(ptr)
}

func libcMalloc_trim(pad uint32) uint32 {
	return proc().libcMalloc_trim(pad)
}

func libcReallocarray(ptr, n, size uint32) uint32 {
	return proc().libcReallocarray(ptr, n, size)
}

func libcStrncmp(a, b, n uint32) uint32 {
	return proc().libcStrncmp(a, b, n)
}

func libcStrcasecmp(a, b uint32) uint32 {
	return proc().libcStrcasecmp(a, b)
}

func libcStrncasecmp(a, b, n uint32) uint32 {
	return proc().libcStrncasecmp(a, b, n)
}

func libcStrchr(s, c uint32) uint32 {
	return proc().libcStrchr(s, c)
}

func libcStrchrnul(s, c uint32) uint32 {
	return proc().libcStrchrnul(s, c)
}

func libcStrrchr(s, c uint32) uint32 {
	return proc().libcStrrchr(s, c)
}

func libcStrcasestr(h, n uint32) uint32 {
	return proc().libcStrcasestr(h, n)
}

func libcStpcpy(d, s uint32) uint32 {
	return proc().libcStpcpy(d, s)
}

func libcStrncpy(d, s, n uint32) uint32 {
	return proc().libcStrncpy(d, s, n)
}

func libcStpncpy(d, s, n uint32) uint32 {
	return proc().libcStpncpy(d, s, n)
}

func libcStrdup(s uint32) uint32 {
	return proc().libcStrdup(s)
}

func libcStrndup(s, n uint32) uint32 {
	return proc().libcStrndup(s, n)
}

func libcStrtok_r(s, delim, save uint32) uint32 {
	return proc().libcStrtok_r(s, delim, save)
}

func libcStrsep(at, delim uint32) uint32 {
	return proc().libcStrsep(at, delim)
}

func libcStrverscmp(a, b uint32) uint32 {
	return proc().libcStrverscmp(a, b)
}

func libcStrtod(s, end uint32) float64 {
	return proc().libcStrtod(s, end)
}

func libcStrtof(s, end uint32) float32 {
	return proc().libcStrtof(s, end)
}

func libcStrtold(s, end uint32) float64 {
	return proc().libcStrtod(s, end)
}

func libcWcstod(s, end uint32) float64 {
	return proc().libcWcstod(s, end)
}

func libcWcstof(s, end uint32) float32 {
	return proc().libcWcstof(s, end)
}

func libcWcstold(s, end uint32) float64 {
	return proc().libcWcstod(s, end)
}

func libcA64l(s uint32) uint32 {
	return proc().libcA64l(s)
}

func libcL64a(v uint32) uint32 {
	return proc().libcL64a(v)
}

func libcGcvt(v float64, n, buf uint32) uint32 {
	return proc().libcGcvt(v, n, buf)
}

func libcCuserid(s uint32) uint32 {
	return proc().libcCuserid(s)
}

func libcSighold(sig uint32) uint32 {
	return proc().libcSighold(sig)
}

func libcSigrelse(sig uint32) uint32 {
	return proc().libcSigrelse(sig)
}

func libcSigignore(sig uint32) uint32 {
	return proc().libcSigignore(sig)
}

func libcSigpause(sig uint32) uint32 {
	return proc().libcSigpause(sig)
}

func libcSigset(sig, disp uint32) uint32 {
	return proc().libcSigset(sig, disp)
}

func libcIconv_open(to, from uint32) uint32 {
	return proc().libcIconv_open(to, from)
}

func libcIconv(cd, inbuf, inleft, outbuf, outleft uint32) uint32 {
	return proc().libcIconv(cd, inbuf, inleft, outbuf, outleft)
}

func libcIconv_close(cd uint32) uint32 {
	return proc().libcIconv_close(cd)
}

func libcStrtol(s, end, base uint32) uint32 {
	return proc().libcStrtol(s, end, base)
}

func libcStrtoul(s, end, base uint32) uint32 {
	return proc().libcStrtoul(s, end, base)
}

func libcStrtoll(s, end, base uint32) uint64 {
	return proc().libcStrtoll(s, end, base)
}

func libcStrtoull(s, end, base uint32) uint64 {
	return proc().libcStrtoull(s, end, base)
}

func libcWcstol(s, end, base uint32) uint32 {
	return proc().libcWcstol(s, end, base)
}

func libcWcstoul(s, end, base uint32) uint32 {
	return proc().libcWcstoul(s, end, base)
}

func libcWcstoll(s, end, base uint32) uint64 {
	return proc().libcWcstoll(s, end, base)
}

func libcWcstoull(s, end, base uint32) uint64 {
	return proc().libcWcstoull(s, end, base)
}

func libcAtoi(s uint32) uint32 {
	return proc().libcAtol(s)
}

func libcAtol(s uint32) uint32 {
	return proc().libcAtol(s)
}

func libcAtoll(s uint32) uint64 {
	return proc().libcAtoll(s)
}

func libcAtof(s uint32) float64 {
	return proc().libcAtof(s)
}

func libcStrtod_l(s, end, locale uint32) float64 {
	return proc().libcStrtod(s, end)
}

func libcStrtof_l(s, end, locale uint32) float32 {
	return proc().libcStrtof(s, end)
}

func libcStrtold_l(s, end, locale uint32) float64 {
	return proc().libcStrtod(s, end)
}

func libc__dtoa(v float64, mode, n, decpt, sign, end uint32) uint32 {
	return proc().libc__dtoa(v, mode, n, decpt, sign, end)
}

func libc__ldtoa(v, mode, n, decpt, sign, end uint32) uint32 {
	return proc().libc__ldtoa(v, mode, n, decpt, sign, end)
}

func libc__hdtoa(v float64, xdigits, n, decpt, sign, end uint32) uint32 {
	return proc().libc__hdtoa(v, xdigits, n, decpt, sign, end)
}

func libc__hldtoa(v float64, xdigits, n, decpt, sign, end uint32) uint32 {
	return proc().libc__hdtoa(v, xdigits, n, decpt, sign, end)
}

func libcAbs(n uint32) uint32 {
	return proc().libcAbs(n)
}

func libcStrerror(e uint32) uint32 {
	return proc().libcStrerror(e)
}

func libcStrsignal(sig uint32) uint32 {
	return proc().libcStrsignal(sig)
}

func libcDirname(s uint32) uint32 {
	return proc().libcDirname(s)
}

func libcFnmatch(pattern, s, flags uint32) uint32 {
	return proc().libcFnmatch(pattern, s, flags)
}

func libcMemrchr(s, c, n uint32) uint32 {
	return proc().libcMemrchr(s, c, n)
}

func libcRawmemchr(s, c uint32) uint32 {
	return proc().libcRawmemchr(s, c)
}

func libcMemccpy(d, s, c, n uint32) uint32 {
	return proc().libcMemccpy(d, s, c, n)
}

func libcSwab(s, d, n uint32) {
	proc().libcSwab(s, d, n)
}

func libcStrtok(s, delim uint32) uint32 {
	return proc().libcStrtok(s, delim)
}

func libcStrcoll(a, b uint32) uint32 {
	return proc().libcStrcoll(a, b)
}

func libcStrxfrm(d, s, n uint32) uint32 {
	return proc().libcStrxfrm(d, s, n)
}

func libcStrerror_r(e, buf, n uint32) uint32 {
	return proc().libcStrerror_r(e, buf, n)
}

func libcBasename(s uint32) uint32 {
	return proc().libcBasename(s)
}

func libcFfs(x uint32) uint32 {
	return proc().libcFfs(x)
}

func libcFfsl(x uint32) uint32 {
	return proc().libcFfsl(x)
}

func libcFfsll(x uint64) uint32 {
	return proc().libcFfsll(x)
}

func libcLabs(n uint32) uint32 {
	return proc().libcLabs(n)
}

func libcLlabs(n uint64) uint64 {
	return proc().libcLlabs(n)
}

func libcDiv(ret, a, b uint32) {
	proc().libcDiv(ret, a, b)
}

func libcLldiv(ret uint32, a, b uint64) {
	proc().libcLldiv(ret, a, b)
}

func libcTime(at uint32) uint64 {
	return proc().libcTime(at)
}

func libcGettimeofday(tv, tvSize, tz uint32) uint32 {
	p := proc()

	p.sized("gettimeofday", tvSize, sizeofTimeval)

	return p.libcGettimeofday(tv, tz)
}

func libcClock_settime(clock, ts, tsSize uint32) uint32 {
	p := proc()

	p.sized("clock_settime", tsSize, sizeofTimespec)

	return p.libcClock_settime(clock, ts)
}

func libcLocaltime(t, size uint32) uint32 {
	p := proc()

	p.sized("localtime", size, sizeofTm)

	return p.libcLocaltime(t)
}

func libcLocaltime_r(t, at, atSize, size uint32) uint32 {
	p := proc()

	p.sized("localtime_r", atSize, sizeofTm, size, sizeofTm)

	return p.libcLocaltime_r(t, at)
}

func libcTzset() {
	proc().libcTzset()
}

func libcAsctime(tm, tmSize uint32) uint32 {
	p := proc()

	p.sized("asctime", tmSize, sizeofTm)

	return p.libcAsctime(tm)
}

func libcAsctime_r(tm, tmSize, buf uint32) uint32 {
	p := proc()

	p.sized("asctime_r", tmSize, sizeofTm)

	return p.libcAsctime_r(tm, buf)
}

func libcCtime_r(t, buf uint32) uint32 {
	return proc().libcCtime_r(t, buf)
}

func libcDifftime(t1, t0 uint64) float64 {
	return proc().libcDifftime(t1, t0)
}

func libcTimegm(at, atSize uint32) uint64 {
	p := proc()

	p.sized("timegm", atSize, sizeofTm)

	return p.libcTimegm(at)
}

func libcSettimeofday(tv, tvSize, tz, tzSize uint32) uint32 {
	p := proc()

	p.sized("settimeofday", tvSize, sizeofTimeval, tzSize, sizeofTimezone)

	return p.libcSettimeofday(tv, tz)
}

func libcStime(t uint32) uint32 {
	return proc().libcStime(t)
}

func libcAdjtime(delta, deltaSize, old, oldSize uint32) uint32 {
	p := proc()

	p.sized("adjtime", deltaSize, sizeofTimeval, oldSize, sizeofTimeval)

	return p.libcAdjtime(delta, old)
}

func libcUsleep(us uint32) uint32 {
	return proc().libcUsleep(us)
}

func libcUalarm(value, interval uint32) uint32 {
	return proc().libcUalarm(value, interval)
}

func libcGmtime(t, size uint32) uint32 {
	p := proc()

	p.sized("gmtime", size, sizeofTm)

	return p.libcGmtime(t)
}

func libcGmtime_r(t, at, atSize, size uint32) uint32 {
	p := proc()

	p.sized("gmtime_r", atSize, sizeofTm, size, sizeofTm)

	return p.libcGmtime_r(t, at)
}

func libcClock() uint64 {
	return proc().libcClock()
}

func libcClock_getres(clock, res, resSize uint32) uint32 {
	p := proc()

	p.sized("clock_getres", resSize, sizeofTimespec)

	return p.libcClock_getres(clock, res)
}

func libcClock_nanosleep(clock, flags, req, reqSize, rem, remSize uint32) uint32 {
	p := proc()

	p.sized("clock_nanosleep", reqSize, sizeofTimespec, remSize, sizeofTimespec)

	return p.libcClock_nanosleep(clock, flags, req, rem)
}

func libcSetitimer(which, value, valueSize, old, oldSize uint32) uint32 {
	p := proc()

	p.sized("setitimer", valueSize, sizeofItimerval, oldSize, sizeofItimerval)

	return p.libcSetitimer(which, value, old)
}

func libcGetitimer(which, at, atSize uint32) uint32 {
	p := proc()

	p.sized("getitimer", atSize, sizeofItimerval)

	return p.libcGetitimer(which, at)
}

func libcAlarm(seconds uint32) uint32 {
	return proc().libcAlarm(seconds)
}

func libcMktime(at, atSize uint32) uint64 {
	p := proc()

	p.sized("mktime", atSize, sizeofTm)

	return p.libcMktime(at)
}

func libcCtime(t uint32) uint32 {
	return proc().libcCtime(t)
}

func libcStrftime(s, max, format, tm, tmSize uint32) uint32 {
	p := proc()

	p.sized("strftime", tmSize, sizeofTm)

	return p.libcStrftime(s, max, format, tm)
}

func libcStrptime(s, format, tm, tmSize uint32) uint32 {
	p := proc()

	p.sized("strptime", tmSize, sizeofTm)

	return p.libcStrptime(s, format, tm)
}

func libcTimes(at, atSize uint32) uint64 {
	p := proc()

	p.sized("times", atSize, sizeofTms)

	return p.libcTimes(at)
}

func libcSleep(seconds uint32) uint32 {
	return proc().libcSleep(seconds)
}

func libcClock_gettime(clock, ts, tsSize uint32) uint32 {
	p := proc()

	p.sized("clock_gettime", tsSize, sizeofTimespec)

	return p.libcClock_gettime(clock, ts)
}

func libcNanosleep(req, reqSize, rem, remSize uint32) uint32 {
	p := proc()

	p.sized("nanosleep", reqSize, sizeofTimespec, remSize, sizeofTimespec)

	return p.libcNanosleep(req, rem)
}

func libcRegcomp(preg, pregSize, pattern, flags uint32) uint32 {
	p := proc()

	p.sized("regcomp", pregSize, sizeofRegex)

	return p.libcRegcomp(preg, pattern, flags)
}

func libcRegexec(preg, pregSize, s, nmatch, pmatch, pmatchSize, eflags uint32) uint32 {
	p := proc()

	p.sized("regexec", pregSize, sizeofRegex, pmatchSize, sizeofRegmatch)

	return p.libcRegexec(preg, s, nmatch, pmatch, eflags)
}

func libcRegfree(preg, pregSize uint32) {
	p := proc()

	p.sized("regfree", pregSize, sizeofRegex)

	p.libcRegfree(preg)
}

func libcRegerror(code, preg, pregSize, buf, size uint32) uint32 {
	p := proc()

	p.sized("regerror", pregSize, sizeofRegex)

	return p.libcRegerror(code, preg, buf, size)
}

func libcSignal(sig, handler uint32) uint32 {
	return proc().libcSignal(sig, handler)
}

func libcSigaction(sig, act, actSize, old, oldSize, infoSize uint32) uint32 {
	p := proc()

	p.sized("sigaction", actSize, sizeofSigaction, oldSize, sizeofSigaction, infoSize, sizeofSiginfo)

	return p.libcSigaction(sig, act, old)
}

func libcSigprocmask(how, set, setSize, old, oldSize uint32) uint32 {
	p := proc()

	p.sized("sigprocmask", setSize, sizeofSigset, oldSize, sizeofSigset)

	return p.libcSigprocmask(how, set, old)
}

func libcPthread_sigmask(how, set, setSize, old, oldSize uint32) uint32 {
	p := proc()

	p.sized("pthread_sigmask", setSize, sizeofSigset, oldSize, sizeofSigset)

	return p.libcPthread_sigmask(how, set, old)
}

func libcSigfillset(set, setSize uint32) uint32 {
	p := proc()

	p.sized("sigfillset", setSize, sizeofSigset)

	return p.libcSigfillset(set)
}

func libcSigemptyset(set, setSize uint32) uint32 {
	p := proc()

	p.sized("sigemptyset", setSize, sizeofSigset)

	return p.libcSigemptyset(set)
}

func libcSigaddset(set, setSize, sig uint32) uint32 {
	p := proc()

	p.sized("sigaddset", setSize, sizeofSigset)

	return p.libcSigaddset(set, sig)
}

func libcSigdelset(set, setSize, sig uint32) uint32 {
	p := proc()

	p.sized("sigdelset", setSize, sizeofSigset)

	return p.libcSigdelset(set, sig)
}

func libcSigismember(set, setSize, sig uint32) uint32 {
	p := proc()

	p.sized("sigismember", setSize, sizeofSigset)

	return p.libcSigismember(set, sig)
}

func libcSigisemptyset(set, setSize uint32) uint32 {
	p := proc()

	p.sized("sigisemptyset", setSize, sizeofSigset)

	return p.libcSigisemptyset(set)
}

func libcSigorset(d, dSize, a, aSize, b, bSize uint32) uint32 {
	p := proc()

	p.sized("sigorset", dSize, sizeofSigset, aSize, sizeofSigset, bSize, sizeofSigset)

	return p.libcSigorset(d, a, b)
}

func libcSigandset(d, dSize, a, aSize, b, bSize uint32) uint32 {
	p := proc()

	p.sized("sigandset", dSize, sizeofSigset, aSize, sizeofSigset, bSize, sizeofSigset)

	return p.libcSigandset(d, a, b)
}

func libcSigpending(set, setSize uint32) uint32 {
	p := proc()

	p.sized("sigpending", setSize, sizeofSigset)

	return p.libcSigpending(set)
}

func libcSigwait(set, setSize, at uint32) uint32 {
	p := proc()

	p.sized("sigwait", setSize, sizeofSigset)

	return p.libcSigwait(set, at)
}

func libcSigwaitinfo(set, setSize, info, infoSize uint32) uint32 {
	p := proc()

	p.sized("sigwaitinfo", setSize, sizeofSigset, infoSize, sizeofSiginfo)

	return p.libcSigwaitinfo(set, info)
}

func libcSigtimedwait(set, setSize, info, infoSize, timeout, timeoutSize uint32) uint32 {
	p := proc()

	p.sized("sigtimedwait", setSize, sizeofSigset, infoSize, sizeofSiginfo, timeoutSize, sizeofTimespec)

	return p.libcSigtimedwait(set, info, timeout)
}

func libcSigaltstack(ss, ssSize, old, oldSize uint32) uint32 {
	p := proc()

	p.sized("sigaltstack", ssSize, sizeofStack, oldSize, sizeofStack)

	return p.libcSigaltstack(ss, old)
}

func libcSiginterrupt(sig, flag uint32) uint32 {
	return proc().libcSiginterrupt(sig, flag)
}

func libcPause() uint32 {
	return proc().libcPause()
}

func libcSigsuspend(set, setSize uint32) uint32 {
	p := proc()

	p.sized("sigsuspend", setSize, sizeofSigset)

	return p.libcSigsuspend(set)
}

func libcRaise(sig uint32) uint32 {
	return proc().libcRaise(sig)
}

func libcKill(pid, sig uint32) uint32 {
	return proc().libcKill(pid, sig)
}

func libcSigqueue(pid, sig, value uint32) uint32 {
	return proc().libcSigqueue(pid, sig, value)
}

func libcKillpg(pgrp, sig uint32) uint32 {
	return proc().libcKillpg(pgrp, sig)
}

func libcAbort() {
	proc().libcAbort()
}

func libcGetpid() uint32 {
	return proc().libcGetpid()
}

func libcGetppid() uint32 {
	return proc().libcGetppid()
}

func libcGetpgrp() uint32 {
	return proc().libcGetpgrp()
}

func libcGetuid() uint32 {
	return proc().libcGetuid()
}

func libcGeteuid() uint32 {
	return proc().libcGeteuid()
}

func libcGetgid() uint32 {
	return proc().libcGetgid()
}

func libcGetegid() uint32 {
	return proc().libcGetegid()
}

func libcGetgroups(n, list uint32) uint32 {
	return proc().libcGetgroups(n, list)
}

func libcUname(at, atSize uint32) uint32 {
	p := proc()

	p.sized("uname", atSize, sizeofUtsname)

	return p.libcUname(at)
}

func libcSysconf(name uint32) uint32 {
	return proc().libcSysconf(name)
}

func libcUmask(mask uint32) uint32 {
	return proc().libcUmask(mask)
}

func libcRand() uint32 {
	return proc().libcRand()
}

func libcSrand(seed uint32) {
	proc().libcSrand(seed)
}

func libcGetenv(name uint32) uint32 {
	return proc().libcGetenv(name)
}

func libcPutenv(s uint32) uint32 {
	return proc().libcPutenv(s)
}

func libcSetenv(at, value, overwrite uint32) uint32 {
	return proc().libcSetenv(at, value, overwrite)
}

func libcUnsetenv(at uint32) uint32 {
	return proc().libcUnsetenv(at)
}

func libcClearenv() uint32 {
	return proc().libcClearenv()
}

func libcExecve(path, argv, envp uint32) uint32 {
	return proc().libcExecve(path, argv, envp)
}

func libcExecvp(file, argv uint32) uint32 {
	return proc().libcExecvp(file, argv)
}

func libcExecv(path, argv uint32) uint32 {
	return proc().libcExecv(path, argv)
}

func libcExecvpe(file, argv, envp uint32) uint32 {
	return proc().libcExecvpe(file, argv, envp)
}

func libcExecle(path, arg0, va uint32) uint32 {
	return proc().libcExecle(path, arg0, va)
}

func libcFexecve(fd, argv, envp uint32) uint32 {
	return proc().libcFexecve(fd, argv, envp)
}

func libcPosix_spawn(pid, path, actions, actionsSize, attr, attrSize, argv, envp uint32) uint32 {
	p := proc()

	p.sized("posix_spawn", actionsSize, sizeofPosixSpawnFileActions, attrSize, sizeofPosixSpawnattr)

	return p.libcPosix_spawn(pid, path, actions, attr, argv, envp)
}

func libcPosix_spawnp(pid, file, actions, actionsSize, attr, attrSize, argv, envp uint32) uint32 {
	p := proc()

	p.sized("posix_spawnp", actionsSize, sizeofPosixSpawnFileActions, attrSize, sizeofPosixSpawnattr)

	return p.libcPosix_spawnp(pid, file, actions, attr, argv, envp)
}

func libcPosix_spawn_file_actions_init(actions, actionsSize uint32) uint32 {
	p := proc()

	p.sized("posix_spawn_file_actions_init", actionsSize, sizeofPosixSpawnFileActions)

	return p.libcPosix_spawn_file_actions_init(actions)
}

func libcPosix_spawn_file_actions_destroy(actions, actionsSize uint32) uint32 {
	p := proc()

	p.sized("posix_spawn_file_actions_destroy", actionsSize, sizeofPosixSpawnFileActions)

	return p.libcPosix_spawn_file_actions_destroy(actions)
}

func libcPosix_spawn_file_actions_addopen(actions, actionsSize, fd, path, flags, mode uint32) uint32 {
	p := proc()

	p.sized("posix_spawn_file_actions_addopen", actionsSize, sizeofPosixSpawnFileActions)

	return p.libcPosix_spawn_file_actions_addopen(actions, fd, path, flags, mode)
}

func libcPosix_spawn_file_actions_addclose(actions, actionsSize, fd uint32) uint32 {
	p := proc()

	p.sized("posix_spawn_file_actions_addclose", actionsSize, sizeofPosixSpawnFileActions)

	return p.libcPosix_spawn_file_actions_addclose(actions, fd)
}

func libcPosix_spawn_file_actions_adddup2(actions, actionsSize, fd, to uint32) uint32 {
	p := proc()

	p.sized("posix_spawn_file_actions_adddup2", actionsSize, sizeofPosixSpawnFileActions)

	return p.libcPosix_spawn_file_actions_adddup2(actions, fd, to)
}

func libcPosix_spawn_file_actions_addchdir(actions, actionsSize, path uint32) uint32 {
	p := proc()

	p.sized("posix_spawn_file_actions_addchdir", actionsSize, sizeofPosixSpawnFileActions)

	return p.libcPosix_spawn_file_actions_addchdir(actions, path)
}

func libcPosix_spawn_file_actions_addfchdir(actions, actionsSize, fd uint32) uint32 {
	p := proc()

	p.sized("posix_spawn_file_actions_addfchdir", actionsSize, sizeofPosixSpawnFileActions)

	return p.libcPosix_spawn_file_actions_addfchdir(actions, fd)
}

func libcPosix_spawn_file_actions_addchdir_np(actions, actionsSize, path uint32) uint32 {
	p := proc()

	p.sized("posix_spawn_file_actions_addchdir_np", actionsSize, sizeofPosixSpawnFileActions)

	return p.libcPosix_spawn_file_actions_addchdir_np(actions, path)
}

func libcPosix_spawn_file_actions_addfchdir_np(actions, actionsSize, fd uint32) uint32 {
	p := proc()

	p.sized("posix_spawn_file_actions_addfchdir_np", actionsSize, sizeofPosixSpawnFileActions)

	return p.libcPosix_spawn_file_actions_addfchdir_np(actions, fd)
}

func libcPosix_spawnattr_init(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_init", attrSize, sizeofPosixSpawnattr)

	return p.libcPosix_spawnattr_init(attr)
}

func libcPosix_spawnattr_destroy(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_destroy", attrSize, sizeofPosixSpawnattr)

	return p.libcPosix_spawnattr_destroy(attr)
}

func libcPosix_spawnattr_getflags(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_getflags", attrSize, sizeofPosixSpawnattr)

	return p.libcPosix_spawnattr_getflags(attr, out)
}

func libcPosix_spawnattr_setflags(attr, attrSize, flags uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_setflags", attrSize, sizeofPosixSpawnattr)

	return p.libcPosix_spawnattr_setflags(attr, flags)
}

func libcPosix_spawnattr_getpgroup(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_getpgroup", attrSize, sizeofPosixSpawnattr)

	return p.libcPosix_spawnattr_getpgroup(attr, out)
}

func libcPosix_spawnattr_setpgroup(attr, attrSize, pgroup uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_setpgroup", attrSize, sizeofPosixSpawnattr)

	return p.libcPosix_spawnattr_setpgroup(attr, pgroup)
}

func libcPosix_spawnattr_getsigdefault(attr, attrSize, out, outSize uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_getsigdefault", attrSize, sizeofPosixSpawnattr, outSize, sizeofSigset)

	return p.libcPosix_spawnattr_getsigdefault(attr, out)
}

func libcPosix_spawnattr_setsigdefault(attr, attrSize, set, setSize uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_setsigdefault", attrSize, sizeofPosixSpawnattr, setSize, sizeofSigset)

	return p.libcPosix_spawnattr_setsigdefault(attr, set)
}

func libcPosix_spawnattr_getsigmask(attr, attrSize, out, outSize uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_getsigmask", attrSize, sizeofPosixSpawnattr, outSize, sizeofSigset)

	return p.libcPosix_spawnattr_getsigmask(attr, out)
}

func libcPosix_spawnattr_setsigmask(attr, attrSize, set, setSize uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_setsigmask", attrSize, sizeofPosixSpawnattr, setSize, sizeofSigset)

	return p.libcPosix_spawnattr_setsigmask(attr, set)
}

func libcPosix_spawnattr_getschedpolicy(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_getschedpolicy", attrSize, sizeofPosixSpawnattr)

	return p.libcPosix_spawnattr_getschedpolicy(attr, out)
}

func libcPosix_spawnattr_setschedpolicy(attr, attrSize, policy uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_setschedpolicy", attrSize, sizeofPosixSpawnattr)

	return p.libcPosix_spawnattr_setschedpolicy(attr, policy)
}

func libcPosix_spawnattr_getschedparam(attr, attrSize, out, outSize uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_getschedparam", attrSize, sizeofPosixSpawnattr, outSize, sizeofSchedParam)

	return p.libcPosix_spawnattr_getschedparam(attr, out)
}

func libcPosix_spawnattr_setschedparam(attr, attrSize, param, paramSize uint32) uint32 {
	p := proc()

	p.sized("posix_spawnattr_setschedparam", attrSize, sizeofPosixSpawnattr, paramSize, sizeofSchedParam)

	return p.libcPosix_spawnattr_setschedparam(attr, param)
}

func libcExecl(path, arg0, va uint32) uint32 {
	return proc().libcExecl(path, arg0, va)
}

func libcExeclp(file, arg0, va uint32) uint32 {
	return proc().libcExeclp(file, arg0, va)
}

func libc_exit(code uint32) {
	proc().libc_exit(code)
}

func libc_Exit(code uint32) {
	proc().libc_Exit(code)
}

func libcSystem(cmd uint32) uint32 {
	return proc().libcSystem(cmd)
}

func libcSetpgid(pid, pgid uint32) uint32 {
	return proc().libcSetpgid(pid, pgid)
}

func libcTcgetpgrp(fd uint32) uint32 {
	return proc().libcTcgetpgrp(fd)
}

func libcTcsetpgrp(fd, pgrp uint32) uint32 {
	return proc().libcTcsetpgrp(fd, pgrp)
}

func libcSetjmp(buf, bufSize uint32) uint32 {
	p := proc()

	p.sized("setjmp", bufSize, sizeofJmpBuf)

	return p.libcSetjmp(buf)
}

func libcSigsetjmp(buf, bufSize, saveMask uint32) uint32 {
	p := proc()

	p.sized("sigsetjmp", bufSize, sizeofJmpBuf)

	return p.libcSigsetjmp(buf, saveMask)
}

func libcLongjmp(buf, bufSize, val uint32) {
	p := proc()

	p.sized("longjmp", bufSize, sizeofJmpBuf)

	p.libcLongjmp(buf, val)
}

func libcVfork() uint32 {
	return proc().libcVfork()
}

func libcFork() uint32 {
	return proc().libcFork()
}

func libcWaitpid(pid, status, options uint32) uint32 {
	return proc().libcWaitpid(pid, status, options)
}

func libcWait(status uint32) uint32 {
	return proc().libcWait(status)
}

func libcWait3(status, options, usage, usageSize uint32) uint32 {
	p := proc()

	p.sized("wait3", usageSize, sizeofRusage)

	return p.libcWait3(status, options, usage)
}

func libcWait4(pid, status, options, usage, usageSize uint32) uint32 {
	p := proc()

	p.sized("wait4", usageSize, sizeofRusage)

	return p.libcWait4(pid, status, options, usage)
}

func libcWaitid(idtype, id, info, infoSize, options uint32) uint32 {
	p := proc()

	p.sized("waitid", infoSize, sizeofSiginfo)

	return p.libcWaitid(idtype, id, info, options)
}

func libcSetlocale(cat, name uint32) uint32 {
	return proc().libcSetlocale(cat, name)
}

func libcLocaleconv(size uint32) uint32 {
	p := proc()

	p.sized("localeconv", size, sizeofLconv)

	return p.libcLocaleconv()
}

func libcNl_langinfo(item uint32) uint32 {
	return proc().libcNl_langinfo(item)
}

func libcTowlower(c uint32) uint32 {
	return proc().libcTowlower(c)
}

func libcTowupper(c uint32) uint32 {
	return proc().libcTowupper(c)
}

func libcWctype(name uint32) uint32 {
	return proc().libcWctype(name)
}

func libcIswctype(c, class uint32) uint32 {
	return proc().libcIswctype(c, class)
}

func libcWctrans(name uint32) uint32 {
	return proc().libcWctrans(name)
}

func libcTowctrans(c, trans uint32) uint32 {
	return proc().libcTowctrans(c, trans)
}

func libcIswalnum(c uint32) uint32 {
	return proc().libcIswalnum(c)
}

func libcIswalpha(c uint32) uint32 {
	return proc().libcIswalpha(c)
}

func libcIswblank(c uint32) uint32 {
	return proc().libcIswblank(c)
}

func libcIswcntrl(c uint32) uint32 {
	return proc().libcIswcntrl(c)
}

func libcIswdigit(c uint32) uint32 {
	return proc().libcIswdigit(c)
}

func libcIswgraph(c uint32) uint32 {
	return proc().libcIswgraph(c)
}

func libcIswlower(c uint32) uint32 {
	return proc().libcIswlower(c)
}

func libcIswprint(c uint32) uint32 {
	return proc().libcIswprint(c)
}

func libcIswpunct(c uint32) uint32 {
	return proc().libcIswpunct(c)
}

func libcIswspace(c uint32) uint32 {
	return proc().libcIswspace(c)
}

func libcIswupper(c uint32) uint32 {
	return proc().libcIswupper(c)
}

func libcIswxdigit(c uint32) uint32 {
	return proc().libcIswxdigit(c)
}

func libcWcslen(s uint32) uint32 {
	return proc().libcWcslen(s)
}

func libcWcsnlen(s, n uint32) uint32 {
	return proc().libcWcsnlen(s, n)
}

func libcWcscpy(d, s uint32) uint32 {
	return proc().libcWcscpy(d, s)
}

func libcWcsncpy(d, s, n uint32) uint32 {
	return proc().libcWcsncpy(d, s, n)
}

func libcWcscat(d, s uint32) uint32 {
	return proc().libcWcscat(d, s)
}

func libcWcscmp(a, b uint32) uint32 {
	return proc().libcWcscmp(a, b)
}

func libcWcsncmp(a, b, n uint32) uint32 {
	return proc().libcWcsncmp(a, b, n)
}

func libcWcscoll(a, b uint32) uint32 {
	return proc().libcWcscoll(a, b)
}

func libcWcsxfrm(d, s, n uint32) uint32 {
	return proc().libcWcsxfrm(d, s, n)
}

func libcWcschr(s, c uint32) uint32 {
	return proc().libcWcschr(s, c)
}

func libcWcsrchr(s, c uint32) uint32 {
	return proc().libcWcsrchr(s, c)
}

func libcWcsstr(h, n uint32) uint32 {
	return proc().libcWcsstr(h, n)
}

func libcWcspbrk(s, set uint32) uint32 {
	return proc().libcWcspbrk(s, set)
}

func libcWcsspn(s, set uint32) uint32 {
	return proc().libcWcsspn(s, set)
}

func libcWcscspn(s, set uint32) uint32 {
	return proc().libcWcscspn(s, set)
}

func libcWcstok(s, sep, save uint32) uint32 {
	return proc().libcWcstok(s, sep, save)
}

func libcWcsdup(s uint32) uint32 {
	return proc().libcWcsdup(s)
}

func libcWcsftime(s, max, format, tm, tmSize uint32) uint32 {
	p := proc()

	p.sized("wcsftime", tmSize, sizeofTm)

	return p.libcWcsftime(s, max, format, tm)
}

func libcWmemchr(s, c, n uint32) uint32 {
	return proc().libcWmemchr(s, c, n)
}

func libcWmemcmp(a, b, n uint32) uint32 {
	return proc().libcWmemcmp(a, b, n)
}

func libcWmemcpy(d, s, n uint32) uint32 {
	return proc().libcWmemcpy(d, s, n)
}

func libcWmemmove(d, s, n uint32) uint32 {
	return proc().libcWmemmove(d, s, n)
}

func libcWmemset(d, c, n uint32) uint32 {
	return proc().libcWmemset(d, c, n)
}

func libcWcwidth(c uint32) uint32 {
	return proc().libcWcwidth(c)
}

func libcWcswidth(s, n uint32) uint32 {
	return proc().libcWcswidth(s, n)
}

func libcSetuid(uid uint32) uint32 {
	return proc().libcSetuid(uid)
}

func libcSeteuid(uid uint32) uint32 {
	return proc().libcSeteuid(uid)
}

func libcSetreuid(r, e uint32) uint32 {
	return proc().libcSetreuid(r, e)
}

func libcSetresuid(r, e, s uint32) uint32 {
	return proc().libcSetresuid(r, e, s)
}

func libcSetgid(gid uint32) uint32 {
	return proc().libcSetgid(gid)
}

func libcSetegid(gid uint32) uint32 {
	return proc().libcSetegid(gid)
}

func libcSetregid(r, e uint32) uint32 {
	return proc().libcSetregid(r, e)
}

func libcSetresgid(r, e, s uint32) uint32 {
	return proc().libcSetresgid(r, e, s)
}

func libcGetresuid(r, e, s uint32) uint32 {
	return proc().libcGetresuid(r, e, s)
}

func libcGetresgid(r, e, s uint32) uint32 {
	return proc().libcGetresgid(r, e, s)
}

func libcSetgroups(n, list uint32) uint32 {
	return proc().libcSetgroups(n, list)
}

func libcInitgroups(user, gid uint32) uint32 {
	return proc().libcInitgroups(user, gid)
}

func libcSetsid() uint32 {
	return proc().libcSetsid()
}

func libcGetsid(pid uint32) uint32 {
	return proc().libcGetsid(pid)
}

func libcGetpgid(pid uint32) uint32 {
	return proc().libcGetpgid(pid)
}

func libcSetpgrp() uint32 {
	return proc().libcSetpgrp()
}

func libcNice(inc uint32) uint32 {
	return proc().libcNice(inc)
}

func libcGetpriority(which, who uint32) uint32 {
	return proc().libcGetpriority(which, who)
}

func libcSetpriority(which, who, prio uint32) uint32 {
	return proc().libcSetpriority(which, who, prio)
}

func libcSched_yield() uint32 {
	return proc().libcSched_yield()
}

func libcSched_get_priority_max(policy uint32) uint32 {
	return proc().libcSched_get_priority_max(policy)
}

func libcSched_get_priority_min(policy uint32) uint32 {
	return proc().libcSched_get_priority_min(policy)
}

func libcSched_getscheduler(pid uint32) uint32 {
	return proc().libcSched_getscheduler(pid)
}

func libcSched_getparam(pid, param, paramSize uint32) uint32 {
	p := proc()

	p.sized("sched_getparam", paramSize, sizeofSchedParam)

	return p.libcSched_getparam(pid, param)
}

func libcSched_setparam(pid, param, paramSize uint32) uint32 {
	p := proc()

	p.sized("sched_setparam", paramSize, sizeofSchedParam)

	return p.libcSched_setparam(pid, param)
}

func libcSched_setscheduler(pid, policy, param, paramSize uint32) uint32 {
	p := proc()

	p.sized("sched_setscheduler", paramSize, sizeofSchedParam)

	return p.libcSched_setscheduler(pid, policy, param)
}

func libcSched_rr_get_interval(pid, ts, tsSize uint32) uint32 {
	p := proc()

	p.sized("sched_rr_get_interval", tsSize, sizeofTimespec)

	return p.libcSched_rr_get_interval(pid, ts)
}

func libcOpenlog(ident, option, facility uint32) {
	proc().libcOpenlog(ident, option, facility)
}

func libcCloselog() {
	proc().libcCloselog()
}

func libcSetlogmask(mask uint32) uint32 {
	return proc().libcSetlogmask(mask)
}

func libc__kernel_syslog(priority, message uint32) {
	proc().libc__kernel_syslog(priority, message)
}

func libcSysinfo(at, atSize uint32) uint32 {
	p := proc()

	p.sized("sysinfo", atSize, sizeofSysinfo)

	return p.libcSysinfo(at)
}

func libcGet_nprocs() uint32 {
	return proc().libcGet_nprocs()
}

func libcGet_nprocs_conf() uint32 {
	return proc().libcGet_nprocs_conf()
}

func libcGet_phys_pages() uint32 {
	return proc().libcGet_phys_pages()
}

func libcGet_avphys_pages() uint32 {
	return proc().libcGet_avphys_pages()
}

func libcGetloadavg(at, n uint32) uint32 {
	return proc().libcGetloadavg(at, n)
}

func libcGetrusage(who, at, atSize uint32) uint32 {
	p := proc()

	p.sized("getrusage", atSize, sizeofRusage)

	return p.libcGetrusage(who, at)
}

func libcGetrlimit(res, at, atSize uint32) uint32 {
	p := proc()

	p.sized("getrlimit", atSize, sizeofRlimit)

	return p.libcGetrlimit(res, at)
}

func libcSetrlimit(res, at, atSize uint32) uint32 {
	p := proc()

	p.sized("setrlimit", atSize, sizeofRlimit)

	return p.libcSetrlimit(res, at)
}

func libcPrlimit(pid, res, set, setSize, old, oldSize uint32) uint32 {
	p := proc()

	p.sized("prlimit", setSize, sizeofRlimit, oldSize, sizeofRlimit)

	return p.libcPrlimit(pid, res, set, old)
}

func libcGetdtablesize() uint32 {
	return proc().libcGetdtablesize()
}

func libcConfstr(name, buf, n uint32) uint32 {
	return proc().libcConfstr(name, buf, n)
}

func libcPathconf(path, name uint32) uint32 {
	return proc().libcPathconf(path, name)
}

func libcFpathconf(fd, name uint32) uint32 {
	return proc().libcFpathconf(fd, name)
}

func libcGetlogin() uint32 {
	return proc().libcGetlogin()
}

func libcGetlogin_r(buf, n uint32) uint32 {
	return proc().libcGetlogin_r(buf, n)
}

func libcCtermid(s uint32) uint32 {
	return proc().libcCtermid(s)
}

func libcTtyname(fd uint32) uint32 {
	return proc().libcTtyname(fd)
}

func libcTtyname_r(fd, buf, n uint32) uint32 {
	return proc().libcTtyname_r(fd, buf, n)
}

func libcGetpwnam(name, size uint32) uint32 {
	p := proc()

	p.sized("getpwnam", size, sizeofPasswd)

	return p.libcGetpwnam(name)
}

func libcGetpwuid(uid, size uint32) uint32 {
	p := proc()

	p.sized("getpwuid", size, sizeofPasswd)

	return p.libcGetpwuid(uid)
}

func libcGetpwnam_r(name, pwd, pwdSize, buf, size, result, resultSize uint32) uint32 {
	p := proc()

	p.sized("getpwnam_r", pwdSize, sizeofPasswd, resultSize, sizeofPasswd)

	return p.libcGetpwnam_r(name, pwd, buf, size, result)
}

func libcGetpwuid_r(uid, pwd, pwdSize, buf, size, result, resultSize uint32) uint32 {
	p := proc()

	p.sized("getpwuid_r", pwdSize, sizeofPasswd, resultSize, sizeofPasswd)

	return p.libcGetpwuid_r(uid, pwd, buf, size, result)
}

func libcGetpwent(size uint32) uint32 {
	p := proc()

	p.sized("getpwent", size, sizeofPasswd)

	return p.libcGetpwent()
}

func libcSetpwent() {
	proc().libcSetpwent()
}

func libcEndpwent() {
	proc().libcEndpwent()
}

func libcGetgrnam(name, size uint32) uint32 {
	p := proc()

	p.sized("getgrnam", size, sizeofGroup)

	return p.libcGetgrnam(name)
}

func libcGetgrgid(gid, size uint32) uint32 {
	p := proc()

	p.sized("getgrgid", size, sizeofGroup)

	return p.libcGetgrgid(gid)
}

func libcGetgrnam_r(name, grp, grpSize, buf, size, result, resultSize uint32) uint32 {
	p := proc()

	p.sized("getgrnam_r", grpSize, sizeofGroup, resultSize, sizeofGroup)

	return p.libcGetgrnam_r(name, grp, buf, size, result)
}

func libcGetgrgid_r(gid, grp, grpSize, buf, size, result, resultSize uint32) uint32 {
	p := proc()

	p.sized("getgrgid_r", grpSize, sizeofGroup, resultSize, sizeofGroup)

	return p.libcGetgrgid_r(gid, grp, buf, size, result)
}

func libcGetgrent(size uint32) uint32 {
	p := proc()

	p.sized("getgrent", size, sizeofGroup)

	return p.libcGetgrent()
}

func libcSetgrent() {
	proc().libcSetgrent()
}

func libcEndgrent() {
	proc().libcEndgrent()
}

func libcGetgrouplist(user, group, groups, count uint32) uint32 {
	return proc().libcGetgrouplist(user, group, groups, count)
}

func libcPosix_openpt(flags uint32) uint32 {
	return proc().libcPosix_openpt(flags)
}

func libcGrantpt(fd uint32) uint32 {
	return proc().libcGrantpt(fd)
}

func libcUnlockpt(fd uint32) uint32 {
	return proc().libcUnlockpt(fd)
}

func libcPtsname(fd uint32) uint32 {
	return proc().libcPtsname(fd)
}

func libcPtsname_r(fd, buf, size uint32) uint32 {
	return proc().libcPtsname_r(fd, buf, size)
}

func libcOpenpty(amaster, aslave, name, termp, termpSize, winp, winpSize uint32) uint32 {
	p := proc()

	p.sized("openpty", termpSize, sizeofTermios, winpSize, sizeofWinsize)

	return p.libcOpenpty(amaster, aslave, name, termp, winp)
}

func libcForkpty(amaster, name, termp, termpSize, winp, winpSize uint32) uint32 {
	p := proc()

	p.sized("forkpty", termpSize, sizeofTermios, winpSize, sizeofWinsize)

	return p.libcForkpty(amaster, name, termp, winp)
}

func libcGlob(pattern, flags, errfunc, out, outSize uint32) uint32 {
	p := proc()

	p.sized("glob", outSize, sizeofGlob)

	return p.libcGlob(pattern, flags, errfunc, out)
}

func libcGlobfree(out, outSize uint32) {
	p := proc()

	p.sized("globfree", outSize, sizeofGlob)

	p.libcGlobfree(out)
}

func libcRandom() uint32 {
	return proc().libcRandom()
}

func libcSrandom(seed uint32) {
	proc().libcSrandom(seed)
}

func libcInitstate(seed, state, size uint32) uint32 {
	return proc().libcInitstate(seed, state, size)
}

func libcSetstate(state uint32) uint32 {
	return proc().libcSetstate(state)
}

func libcRand_r(seed uint32) uint32 {
	return proc().libcRand_r(seed)
}

func libcSecure_getenv(name uint32) uint32 {
	return proc().libcSecure_getenv(name)
}

func libcGetsubopt(opt, keys, val uint32) uint32 {
	return proc().libcGetsubopt(opt, keys, val)
}

func libcDaemon(nochdir, noclose uint32) uint32 {
	return proc().libcDaemon(nochdir, noclose)
}

func libcPrctl(option, va uint32) uint32 {
	return proc().libcPrctl(option, va)
}

func libcGetpass(prompt uint32) uint32 {
	return proc().libcGetpass(prompt)
}

func libcVhangup() uint32 {
	return proc().libcVhangup()
}

func libcGetusershell() uint32 {
	return proc().libcGetusershell()
}

func libcSetusershell() {
	proc().libcSetusershell()
}

func libcEndusershell() {
	proc().libcEndusershell()
}

func libcGethostname(name, n uint32) uint32 {
	return proc().libcGethostname(name, n)
}

func libcGetentropy(buf, n uint32) uint32 {
	return proc().libcGetentropy(buf, n)
}

func libcGetdomainname(name, n uint32) uint32 {
	return proc().libcGetdomainname(name, n)
}

func libcSethostname(name, n uint32) uint32 {
	return proc().libcSethostname(name, n)
}

func libcSetdomainname(name, n uint32) uint32 {
	return proc().libcSetdomainname(name, n)
}

func libcGethostid() uint32 {
	return proc().libcGethostid()
}

func libcSethostid(id uint32) uint32 {
	return proc().libcSethostid(id)
}

func libcPsignal(sig, s uint32) {
	proc().libcPsignal(sig, s)
}

func libcPsiginfo(info, infoSize, s uint32) {
	p := proc()

	p.sized("psiginfo", infoSize, sizeofSiginfo)

	p.libcPsiginfo(info, s)
}

func libc__assert_fail(expr, file, line, function uint32) {
	proc().libc__assert_fail(expr, file, line, function)
}

func libcSocket(domain, kind, protocol uint32) uint32 {
	return proc().libcSocket(domain, kind, protocol)
}

func libcSocketpair(domain, kind, protocol, at uint32) uint32 {
	return proc().libcSocketpair(domain, kind, protocol, at)
}

func libcBind(fd, addr, n uint32) uint32 {
	return proc().libcBind(fd, addr, n)
}

func libcConnect(fd, addr, n uint32) uint32 {
	return proc().libcConnect(fd, addr, n)
}

func libcListen(fd, backlog uint32) uint32 {
	return proc().libcListen(fd, backlog)
}

func libcAccept(fd, addr, lenAt uint32) uint32 {
	return proc().libcAccept(fd, addr, lenAt)
}

func libcAccept4(fd, addr, lenAt, flags uint32) uint32 {
	return proc().libcAccept4(fd, addr, lenAt, flags)
}

func libcGetsockname(fd, addr, lenAt uint32) uint32 {
	return proc().libcGetsockname(fd, addr, lenAt)
}

func libcGetpeername(fd, addr, lenAt uint32) uint32 {
	return proc().libcGetpeername(fd, addr, lenAt)
}

func libcGetsockopt(fd, level, name, at, lenAt uint32) uint32 {
	return proc().libcGetsockopt(fd, level, name, at, lenAt)
}

func libcSetsockopt(fd, level, name, at, n uint32) uint32 {
	return proc().libcSetsockopt(fd, level, name, at, n)
}

func libcSend(fd, buf, n, flags uint32) uint32 {
	return proc().libcSend(fd, buf, n, flags)
}

func libcSendto(fd, buf, n, flags, addr, alen uint32) uint32 {
	return proc().libcSendto(fd, buf, n, flags, addr, alen)
}

func libcRecv(fd, buf, n, flags uint32) uint32 {
	return proc().libcRecv(fd, buf, n, flags)
}

func libcRecvfrom(fd, buf, n, flags, addr, lenAt uint32) uint32 {
	return proc().libcRecvfrom(fd, buf, n, flags, addr, lenAt)
}

func libcSendmsg(fd, msg, msgSize, iovSize, flags uint32) uint32 {
	p := proc()

	p.sized("sendmsg", msgSize, sizeofMsghdr, iovSize, sizeofIovec)

	return p.libcSendmsg(fd, msg, flags)
}

func libcRecvmsg(fd, msg, msgSize, iovSize, flags uint32) uint32 {
	p := proc()

	p.sized("recvmsg", msgSize, sizeofMsghdr, iovSize, sizeofIovec)

	return p.libcRecvmsg(fd, msg, flags)
}

func libcShutdown(fd, how uint32) uint32 {
	return proc().libcShutdown(fd, how)
}

func libcSockatmark(fd uint32) uint32 {
	return proc().libcSockatmark(fd)
}

func libcGetaddrinfo(node, service, hints, hintsSize, res, resSize uint32) uint32 {
	p := proc()

	p.sized("getaddrinfo", hintsSize, sizeofAddrinfo, resSize, sizeofAddrinfo)

	return p.libcGetaddrinfo(node, service, hints, res)
}

func libcFreeaddrinfo(res, resSize uint32) {
	p := proc()

	p.sized("freeaddrinfo", resSize, sizeofAddrinfo)

	p.libcFreeaddrinfo(res)
}

func libcGai_strerror(code uint32) uint32 {
	return proc().libcGai_strerror(code)
}

func libcGetnameinfo(sa, salen, host, hostlen, serv, servlen, flags uint32) uint32 {
	return proc().libcGetnameinfo(sa, salen, host, hostlen, serv, servlen, flags)
}

func libcGethostbyname(name, size uint32) uint32 {
	p := proc()

	p.sized("gethostbyname", size, sizeofHostent)

	return p.libcGethostbyname(name)
}

func libcGethostbyname2(name, family, size uint32) uint32 {
	p := proc()

	p.sized("gethostbyname2", size, sizeofHostent)

	return p.libcGethostbyname2(name, family)
}

func libcGethostbyaddr(addr, n, family, size uint32) uint32 {
	p := proc()

	p.sized("gethostbyaddr", size, sizeofHostent)

	return p.libcGethostbyaddr(addr, n, family)
}

func libcGethostent(size uint32) uint32 {
	p := proc()

	p.sized("gethostent", size, sizeofHostent)

	return p.libcGethostent()
}

func libcSethostent(stay uint32) {
	proc().libcSethostent(stay)
}

func libcEndhostent() {
	proc().libcEndhostent()
}

func libcGetservbyname(name, proto, size uint32) uint32 {
	p := proc()

	p.sized("getservbyname", size, sizeofServent)

	return p.libcGetservbyname(name, proto)
}

func libcGetservbyport(port, proto, size uint32) uint32 {
	p := proc()

	p.sized("getservbyport", size, sizeofServent)

	return p.libcGetservbyport(port, proto)
}

func libcGetprotobyname(name, size uint32) uint32 {
	p := proc()

	p.sized("getprotobyname", size, sizeofProtoent)

	return p.libcGetprotobyname(name)
}

func libcGetprotobynumber(number, size uint32) uint32 {
	p := proc()

	p.sized("getprotobynumber", size, sizeofProtoent)

	return p.libcGetprotobynumber(number)
}

func libcGetnetbyname(name, size uint32) uint32 {
	p := proc()

	p.sized("getnetbyname", size, sizeofNetent)

	return p.libcGetnetbyname(name)
}

func libcGetnetbyaddr(net, family, size uint32) uint32 {
	p := proc()

	p.sized("getnetbyaddr", size, sizeofNetent)

	return p.libcGetnetbyaddr(net, family)
}

func libcHstrerror(code uint32) uint32 {
	return proc().libcHstrerror(code)
}

func libcHerror(s uint32) {
	proc().libcHerror(s)
}

func libcInet_aton(s, at, atSize uint32) uint32 {
	p := proc()

	p.sized("inet_aton", atSize, sizeofInAddr)

	return p.libcInet_aton(s, at)
}

func libcInet_addr(s uint32) uint32 {
	return proc().libcInet_addr(s)
}

func libcInet_network(s uint32) uint32 {
	return proc().libcInet_network(s)
}

func libcInet_ntoa(in uint32) uint32 {
	return proc().libcInet_ntoa(in)
}

func libcInet_pton(family, s, dst uint32) uint32 {
	return proc().libcInet_pton(family, s, dst)
}

func libcInet_ntop(family, src, dst, size uint32) uint32 {
	return proc().libcInet_ntop(family, src, dst, size)
}

func libcInet_makeaddr(n, h uint32) uint32 {
	return proc().libcInet_makeaddr(n, h)
}

func libcInet_lnaof(in uint32) uint32 {
	return proc().libcInet_lnaof(in)
}

func libcInet_netof(in uint32) uint32 {
	return proc().libcInet_netof(in)
}

func libcIf_nametoindex(name uint32) uint32 {
	return proc().libcIf_nametoindex(name)
}

func libcIf_indextoname(index, buf uint32) uint32 {
	return proc().libcIf_indextoname(index, buf)
}

func libcIf_nameindex(size uint32) uint32 {
	p := proc()

	p.sized("if_nameindex", size, sizeofIfNameindex)

	return p.libcIf_nameindex()
}

func libcIf_freenameindex(block, blockSize uint32) {
	p := proc()

	p.sized("if_freenameindex", blockSize, sizeofIfNameindex)

	p.libcIf_freenameindex(block)
}

func libcCrypt(key, setting uint32) uint32 {
	return proc().libcCrypt(key, setting)
}

func libcEpoll_create(size uint32) uint32 {
	return proc().libcEpoll_create(size)
}

func libcEpoll_create1(flags uint32) uint32 {
	return proc().libcEpoll_create1(flags)
}

func libcEpoll_ctl(epfd, op, fd, at, atSize uint32) uint32 {
	p := proc()

	p.sized("epoll_ctl", atSize, sizeofEpollEvent)

	return p.libcEpoll_ctl(epfd, op, fd, at)
}

func libcEpoll_wait(epfd, at, atSize, max, timeout uint32) uint32 {
	p := proc()

	p.sized("epoll_wait", atSize, sizeofEpollEvent)

	return p.libcEpoll_wait(epfd, at, max, timeout)
}

func libcEpoll_pwait(epfd, at, atSize, max, timeout, mask, maskSize uint32) uint32 {
	p := proc()

	p.sized("epoll_pwait", atSize, sizeofEpollEvent, maskSize, sizeofSigset)

	return p.libcEpoll_pwait(epfd, at, max, timeout, mask)
}

func libcIsalnum(c uint32) uint32 {
	return proc().libcIsalnum(c)
}

func libcIsalpha(c uint32) uint32 {
	return proc().libcIsalpha(c)
}

func libcIsascii(c uint32) uint32 {
	return proc().libcIsascii(c)
}

func libcIsblank(c uint32) uint32 {
	return proc().libcIsblank(c)
}

func libcIscntrl(c uint32) uint32 {
	return proc().libcIscntrl(c)
}

func libcIsdigit(c uint32) uint32 {
	return proc().libcIsdigit(c)
}

func libcIsgraph(c uint32) uint32 {
	return proc().libcIsgraph(c)
}

func libcIslower(c uint32) uint32 {
	return proc().libcIslower(c)
}

func libcIsprint(c uint32) uint32 {
	return proc().libcIsprint(c)
}

func libcIspunct(c uint32) uint32 {
	return proc().libcIspunct(c)
}

func libcIsspace(c uint32) uint32 {
	return proc().libcIsspace(c)
}

func libcIsupper(c uint32) uint32 {
	return proc().libcIsupper(c)
}

func libcIsxdigit(c uint32) uint32 {
	return proc().libcIsxdigit(c)
}

func libcTolower(c uint32) uint32 {
	return proc().libcTolower(c)
}

func libcToupper(c uint32) uint32 {
	return proc().libcToupper(c)
}

func libcToascii(c uint32) uint32 {
	return proc().libcToascii(c)
}

func libcMemmem(h, hn, n, nn uint32) uint32 {
	return proc().libcMemmem(h, hn, n, nn)
}

func libcMempcpy(d, s, n uint32) uint32 {
	return proc().libcMempcpy(d, s, n)
}

func libcStrnlen(s, n uint32) uint32 {
	return proc().libcStrnlen(s, n)
}

func libcStrncat(d, s, n uint32) uint32 {
	return proc().libcStrncat(d, s, n)
}

func libcStrlcpy(d, s, n uint32) uint32 {
	return proc().libcStrlcpy(d, s, n)
}

func libcStrlcat(d, s, n uint32) uint32 {
	return proc().libcStrlcat(d, s, n)
}

func libcStrstr(h, n uint32) uint32 {
	return proc().libcStrstr(h, n)
}

func libcStrpbrk(s, accept uint32) uint32 {
	return proc().libcStrpbrk(s, accept)
}

func libcExplicit_bzero(d, n uint32) {
	proc().libcExplicit_bzero(d, n)
}

func libcBcopy(s, d, n uint32) {
	proc().libcBcopy(s, d, n)
}

func libcBzero(d, n uint32) {
	proc().libcBzero(d, n)
}

func libcMemcpy(d, s, n uint32) uint32 {
	return proc().libcMemcpy(d, s, n)
}

func libcMemmove(d, s, n uint32) uint32 {
	return proc().libcMemmove(d, s, n)
}

func libcMemset(d, c, n uint32) uint32 {
	return proc().libcMemset(d, c, n)
}

func libcMemcmp(a, b, n uint32) uint32 {
	return proc().libcMemcmp(a, b, n)
}

func libcBcmp(a, b, n uint32) uint32 {
	return proc().libcBcmp(a, b, n)
}

func libcMemchr(s, c, n uint32) uint32 {
	return proc().libcMemchr(s, c, n)
}

func libcStrlen(s uint32) uint32 {
	return proc().libcStrlen(s)
}

func libcStrcpy(d, s uint32) uint32 {
	return proc().libcStrcpy(d, s)
}

func libcStrcat(d, s uint32) uint32 {
	return proc().libcStrcat(d, s)
}

func libcStrcmp(a, b uint32) uint32 {
	return proc().libcStrcmp(a, b)
}

func libcStrspn(s, set uint32) uint32 {
	return proc().libcStrspn(s, set)
}

func libcStrcspn(s, set uint32) uint32 {
	return proc().libcStrcspn(s, set)
}

func libcExecmode(modes, classes, sink uint32) uint32 {
	return proc().libcExecmode(modes, classes, sink)
}

func fcntlSize(cmd uint32) uint32 {
	switch cmd {
	case F_GETLK, F_SETLK, F_SETLKW:
		return sizeofFlock
	}

	return 0
}

func libcPthread_create(thread, attr, attrSize, fn, d uint32) uint32 {
	p := proc()

	p.sized("pthread_create", attrSize, sizeofPthreadAttr)

	return p.libcPthread_create(thread, attr, fn, d)
}

func libcPthread_join(thread, out uint32) uint32 {
	return proc().libcPthread_join(thread, out)
}

func libcPthread_detach(thread uint32) uint32 {
	return proc().libcPthread_detach(thread)
}

func libcPthread_self() uint32 {
	return proc().libcPthread_self()
}

func libcPthread_equal(a, b uint32) uint32 {
	return proc().libcPthread_equal(a, b)
}

func libcPthread_cancel(thread uint32) uint32 {
	return proc().libcPthread_cancel(thread)
}

func libcPthread_setcancelstate(value, out uint32) uint32 {
	return proc().libcPthread_setcancelstate(value, out)
}

func libcPthread_setcanceltype(value, out uint32) uint32 {
	return proc().libcPthread_setcanceltype(value, out)
}

func libcPthread_getschedparam(thread, out, param, paramSize uint32) uint32 {
	p := proc()

	p.sized("pthread_getschedparam", paramSize, sizeofSchedParam)

	return p.libcPthread_getschedparam(thread, out, param)
}

func libcPthread_setschedparam(thread, value, param, paramSize uint32) uint32 {
	p := proc()

	p.sized("pthread_setschedparam", paramSize, sizeofSchedParam)

	return p.libcPthread_setschedparam(thread, value, param)
}

func libcPthread_setschedprio(thread, value uint32) uint32 {
	return proc().libcPthread_setschedprio(thread, value)
}

func libcPthread_getconcurrency() uint32 {
	return proc().libcPthread_getconcurrency()
}

func libcPthread_setconcurrency(value uint32) uint32 {
	return proc().libcPthread_setconcurrency(value)
}

func libcPthread_getcpuclockid(thread, out uint32) uint32 {
	return proc().libcPthread_getcpuclockid(thread, out)
}

func libcPthread_attr_init(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_init", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_init(attr)
}

func libcPthread_attr_destroy(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_destroy", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_destroy(attr)
}

func libcPthread_attr_setdetachstate(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_setdetachstate", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_setdetachstate(attr, value)
}

func libcPthread_attr_getdetachstate(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_getdetachstate", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_getdetachstate(attr, out)
}

func libcPthread_attr_setstacksize(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_setstacksize", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_setstacksize(attr, value)
}

func libcPthread_attr_getstacksize(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_getstacksize", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_getstacksize(attr, out)
}

func libcPthread_attr_setguardsize(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_setguardsize", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_setguardsize(attr, value)
}

func libcPthread_attr_getguardsize(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_getguardsize", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_getguardsize(attr, out)
}

func libcPthread_attr_setscope(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_setscope", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_setscope(attr, value)
}

func libcPthread_attr_getscope(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_getscope", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_getscope(attr, out)
}

func libcPthread_attr_setschedpolicy(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_setschedpolicy", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_setschedpolicy(attr, value)
}

func libcPthread_attr_getschedpolicy(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_getschedpolicy", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_getschedpolicy(attr, out)
}

func libcPthread_attr_setinheritsched(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_setinheritsched", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_setinheritsched(attr, value)
}

func libcPthread_attr_getinheritsched(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_getinheritsched", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_getinheritsched(attr, out)
}

func libcPthread_attr_setstack(attr, attrSize, b, value uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_setstack", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_setstack(attr, b, value)
}

func libcPthread_attr_getstack(attr, attrSize, b, c uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_getstack", attrSize, sizeofPthreadAttr)

	return p.libcPthread_attr_getstack(attr, b, c)
}

func libcPthread_attr_setschedparam(attr, attrSize, param, paramSize uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_setschedparam", attrSize, sizeofPthreadAttr, paramSize, sizeofSchedParam)

	return p.libcPthread_attr_setschedparam(attr, param)
}

func libcPthread_attr_getschedparam(attr, attrSize, param, paramSize uint32) uint32 {
	p := proc()

	p.sized("pthread_attr_getschedparam", attrSize, sizeofPthreadAttr, paramSize, sizeofSchedParam)

	return p.libcPthread_attr_getschedparam(attr, param)
}

func libcPthread_key_create(key, fn uint32) uint32 {
	return proc().libcPthread_key_create(key, fn)
}

func libcPthread_key_delete(key uint32) uint32 {
	return proc().libcPthread_key_delete(key)
}

func libcPthread_getspecific(key uint32) uint32 {
	return proc().libcPthread_getspecific(key)
}

func libcPthread_setspecific(key, b uint32) uint32 {
	return proc().libcPthread_setspecific(key, b)
}

func libcPthread_mutexattr_init(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_init", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_init(attr)
}

func libcPthread_mutexattr_destroy(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_destroy", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_destroy(attr)
}

func libcPthread_mutexattr_settype(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_settype", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_settype(attr, value)
}

func libcPthread_mutexattr_gettype(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_gettype", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_gettype(attr, out)
}

func libcPthread_mutexattr_setprotocol(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_setprotocol", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_setprotocol(attr, value)
}

func libcPthread_mutexattr_getprotocol(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_getprotocol", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_getprotocol(attr, out)
}

func libcPthread_mutexattr_setrobust(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_setrobust", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_setrobust(attr, value)
}

func libcPthread_mutexattr_getrobust(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_getrobust", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_getrobust(attr, out)
}

func libcPthread_mutexattr_setpshared(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_setpshared", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_setpshared(attr, value)
}

func libcPthread_mutexattr_getpshared(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_getpshared", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_getpshared(attr, out)
}

func libcPthread_mutexattr_setprioceiling(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_setprioceiling", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_setprioceiling(attr, value)
}

func libcPthread_mutexattr_getprioceiling(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_mutexattr_getprioceiling", attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutexattr_getprioceiling(attr, out)
}

func libcPthread_mutex_init(mutex, mutexSize, attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_mutex_init", mutexSize, sizeofPthreadMutex, attrSize, sizeofPthreadMutexattr)

	return p.libcPthread_mutex_init(mutex, attr)
}

func libcPthread_mutex_destroy(mutex, mutexSize uint32) uint32 {
	p := proc()

	p.sized("pthread_mutex_destroy", mutexSize, sizeofPthreadMutex)

	return p.libcPthread_mutex_destroy(mutex)
}

func libcPthread_mutex_lock(mutex, mutexSize uint32) uint32 {
	p := proc()

	p.sized("pthread_mutex_lock", mutexSize, sizeofPthreadMutex)

	return p.libcPthread_mutex_lock(mutex)
}

func libcPthread_mutex_trylock(mutex, mutexSize uint32) uint32 {
	p := proc()

	p.sized("pthread_mutex_trylock", mutexSize, sizeofPthreadMutex)

	return p.libcPthread_mutex_trylock(mutex)
}

func libcPthread_mutex_unlock(mutex, mutexSize uint32) uint32 {
	p := proc()

	p.sized("pthread_mutex_unlock", mutexSize, sizeofPthreadMutex)

	return p.libcPthread_mutex_unlock(mutex)
}

func libcPthread_mutex_consistent(mutex, mutexSize uint32) uint32 {
	p := proc()

	p.sized("pthread_mutex_consistent", mutexSize, sizeofPthreadMutex)

	return p.libcPthread_mutex_consistent(mutex)
}

func libcPthread_mutex_timedlock(mutex, mutexSize, at, atSize uint32) uint32 {
	p := proc()

	p.sized("pthread_mutex_timedlock", mutexSize, sizeofPthreadMutex, atSize, sizeofTimespec)

	return p.libcPthread_mutex_timedlock(mutex, at)
}

func libcPthread_mutex_getprioceiling(mutex, mutexSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_mutex_getprioceiling", mutexSize, sizeofPthreadMutex)

	return p.libcPthread_mutex_getprioceiling(mutex, out)
}

func libcPthread_mutex_setprioceiling(mutex, mutexSize, value, out uint32) uint32 {
	p := proc()

	p.sized("pthread_mutex_setprioceiling", mutexSize, sizeofPthreadMutex)

	return p.libcPthread_mutex_setprioceiling(mutex, value, out)
}

func libcPthread_condattr_init(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_condattr_init", attrSize, sizeofPthreadCondattr)

	return p.libcPthread_condattr_init(attr)
}

func libcPthread_condattr_destroy(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_condattr_destroy", attrSize, sizeofPthreadCondattr)

	return p.libcPthread_condattr_destroy(attr)
}

func libcPthread_condattr_setclock(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_condattr_setclock", attrSize, sizeofPthreadCondattr)

	return p.libcPthread_condattr_setclock(attr, value)
}

func libcPthread_condattr_getclock(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_condattr_getclock", attrSize, sizeofPthreadCondattr)

	return p.libcPthread_condattr_getclock(attr, out)
}

func libcPthread_condattr_setpshared(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_condattr_setpshared", attrSize, sizeofPthreadCondattr)

	return p.libcPthread_condattr_setpshared(attr, value)
}

func libcPthread_condattr_getpshared(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_condattr_getpshared", attrSize, sizeofPthreadCondattr)

	return p.libcPthread_condattr_getpshared(attr, out)
}

func libcPthread_cond_init(cond, condSize, attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_cond_init", condSize, sizeofPthreadCond, attrSize, sizeofPthreadCondattr)

	return p.libcPthread_cond_init(cond, attr)
}

func libcPthread_cond_destroy(cond, condSize uint32) uint32 {
	p := proc()

	p.sized("pthread_cond_destroy", condSize, sizeofPthreadCond)

	return p.libcPthread_cond_destroy(cond)
}

func libcPthread_cond_signal(cond, condSize uint32) uint32 {
	p := proc()

	p.sized("pthread_cond_signal", condSize, sizeofPthreadCond)

	return p.libcPthread_cond_signal(cond)
}

func libcPthread_cond_broadcast(cond, condSize uint32) uint32 {
	p := proc()

	p.sized("pthread_cond_broadcast", condSize, sizeofPthreadCond)

	return p.libcPthread_cond_broadcast(cond)
}

func libcPthread_cond_wait(cond, condSize, mutex, mutexSize uint32) uint32 {
	p := proc()

	p.sized("pthread_cond_wait", condSize, sizeofPthreadCond, mutexSize, sizeofPthreadMutex)

	return p.libcPthread_cond_wait(cond, mutex)
}

func libcPthread_cond_timedwait(cond, condSize, mutex, mutexSize, at, atSize uint32) uint32 {
	p := proc()

	p.sized("pthread_cond_timedwait", condSize, sizeofPthreadCond, mutexSize, sizeofPthreadMutex, atSize, sizeofTimespec)

	return p.libcPthread_cond_timedwait(cond, mutex, at)
}

func libcPthread_rwlockattr_init(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlockattr_init", attrSize, sizeofPthreadRwlockattr)

	return p.libcPthread_rwlockattr_init(attr)
}

func libcPthread_rwlockattr_destroy(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlockattr_destroy", attrSize, sizeofPthreadRwlockattr)

	return p.libcPthread_rwlockattr_destroy(attr)
}

func libcPthread_rwlockattr_setpshared(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlockattr_setpshared", attrSize, sizeofPthreadRwlockattr)

	return p.libcPthread_rwlockattr_setpshared(attr, value)
}

func libcPthread_rwlockattr_getpshared(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlockattr_getpshared", attrSize, sizeofPthreadRwlockattr)

	return p.libcPthread_rwlockattr_getpshared(attr, out)
}

func libcPthread_rwlock_init(lock, lockSize, attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_init", lockSize, sizeofPthreadRwlock, attrSize, sizeofPthreadRwlockattr)

	return p.libcPthread_rwlock_init(lock, attr)
}

func libcPthread_rwlock_destroy(lock, lockSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_destroy", lockSize, sizeofPthreadRwlock)

	return p.libcPthread_rwlock_destroy(lock)
}

func libcPthread_rwlock_rdlock(lock, lockSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_rdlock", lockSize, sizeofPthreadRwlock)

	return p.libcPthread_rwlock_rdlock(lock)
}

func libcPthread_rwlock_tryrdlock(lock, lockSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_tryrdlock", lockSize, sizeofPthreadRwlock)

	return p.libcPthread_rwlock_tryrdlock(lock)
}

func libcPthread_rwlock_wrlock(lock, lockSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_wrlock", lockSize, sizeofPthreadRwlock)

	return p.libcPthread_rwlock_wrlock(lock)
}

func libcPthread_rwlock_trywrlock(lock, lockSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_trywrlock", lockSize, sizeofPthreadRwlock)

	return p.libcPthread_rwlock_trywrlock(lock)
}

func libcPthread_rwlock_unlock(lock, lockSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_unlock", lockSize, sizeofPthreadRwlock)

	return p.libcPthread_rwlock_unlock(lock)
}

func libcPthread_rwlock_timedrdlock(lock, lockSize, at, atSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_timedrdlock", lockSize, sizeofPthreadRwlock, atSize, sizeofTimespec)

	return p.libcPthread_rwlock_timedrdlock(lock, at)
}

func libcPthread_rwlock_timedwrlock(lock, lockSize, at, atSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_timedwrlock", lockSize, sizeofPthreadRwlock, atSize, sizeofTimespec)

	return p.libcPthread_rwlock_timedwrlock(lock, at)
}

func libcPthread_barrierattr_init(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_barrierattr_init", attrSize, sizeofPthreadBarrierattr)

	return p.libcPthread_barrierattr_init(attr)
}

func libcPthread_barrierattr_destroy(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_barrierattr_destroy", attrSize, sizeofPthreadBarrierattr)

	return p.libcPthread_barrierattr_destroy(attr)
}

func libcPthread_barrierattr_setpshared(attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_barrierattr_setpshared", attrSize, sizeofPthreadBarrierattr)

	return p.libcPthread_barrierattr_setpshared(attr, value)
}

func libcPthread_barrierattr_getpshared(attr, attrSize, out uint32) uint32 {
	p := proc()

	p.sized("pthread_barrierattr_getpshared", attrSize, sizeofPthreadBarrierattr)

	return p.libcPthread_barrierattr_getpshared(attr, out)
}

func libcPthread_barrier_init(barrier, barrierSize, attr, attrSize, value uint32) uint32 {
	p := proc()

	p.sized("pthread_barrier_init", barrierSize, sizeofPthreadBarrier, attrSize, sizeofPthreadBarrierattr)

	return p.libcPthread_barrier_init(barrier, attr, value)
}

func libcPthread_barrier_destroy(barrier, barrierSize uint32) uint32 {
	p := proc()

	p.sized("pthread_barrier_destroy", barrierSize, sizeofPthreadBarrier)

	return p.libcPthread_barrier_destroy(barrier)
}

func libcPthread_barrier_wait(barrier, barrierSize uint32) uint32 {
	p := proc()

	p.sized("pthread_barrier_wait", barrierSize, sizeofPthreadBarrier)

	return p.libcPthread_barrier_wait(barrier)
}

func libcPthread_spin_init(lock, value uint32) uint32 {
	return proc().libcPthread_spin_init(lock, value)
}

func libcPthread_spin_destroy(lock uint32) uint32 {
	return proc().libcPthread_spin_destroy(lock)
}

func libcPthread_spin_lock(lock uint32) uint32 {
	return proc().libcPthread_spin_lock(lock)
}

func libcPthread_spin_trylock(lock uint32) uint32 {
	return proc().libcPthread_spin_trylock(lock)
}

func libcPthread_spin_unlock(lock uint32) uint32 {
	return proc().libcPthread_spin_unlock(lock)
}

func libcPthread_getaffinity_np(thread, value, set uint32) uint32 {
	return proc().libcPthread_getaffinity_np(thread, value, set)
}

func libcPthread_setaffinity_np(thread, value, set uint32) uint32 {
	return proc().libcPthread_setaffinity_np(thread, value, set)
}

func libcPthread_getattr_np(thread, attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_getattr_np", attrSize, sizeofPthreadAttr)

	return p.libcPthread_getattr_np(thread, attr)
}

func libcPthread_getattr_default_np(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_getattr_default_np", attrSize, sizeofPthreadAttr)

	return p.libcPthread_getattr_default_np(attr)
}

func libcPthread_setattr_default_np(attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("pthread_setattr_default_np", attrSize, sizeofPthreadAttr)

	return p.libcPthread_setattr_default_np(attr)
}

func libcPthread_getname_np(thread, b, value uint32) uint32 {
	return proc().libcPthread_getname_np(thread, b, value)
}

func libcPthread_setname_np(thread, b uint32) uint32 {
	return proc().libcPthread_setname_np(thread, b)
}

func libcPthread_tryjoin_np(thread, out uint32) uint32 {
	return proc().libcPthread_tryjoin_np(thread, out)
}

func libcPthread_timedjoin_np(thread, out, at, atSize uint32) uint32 {
	p := proc()

	p.sized("pthread_timedjoin_np", atSize, sizeofTimespec)

	return p.libcPthread_timedjoin_np(thread, out, at)
}

func libc__pthread_once_begin(once uint32) uint32 {
	return proc().libc__pthread_once_begin(once)
}

func libc__pthread_once_end(once uint32) {
	proc().libc__pthread_once_end(once)
}

func libc__pthread_cancelled(value uint32) uint32 {
	return proc().libc__pthread_cancelled(value)
}

func libc__pthread_key_take(key, fn uint32) uint32 {
	return proc().libc__pthread_key_take(key, fn)
}

func libc__sched_cpucount(value, set uint32) uint32 {
	return proc().libc__sched_cpucount(value, set)
}

func libcSched_getaffinity(a, b, set uint32) uint32 {
	return proc().libcSched_getaffinity(a, b, set)
}

func libcSched_setaffinity(a, b, set uint32) uint32 {
	return proc().libcSched_setaffinity(a, b, set)
}

func libcSched_getcpu() uint32 {
	return proc().libcSched_getcpu()
}

func libcSem_init(sem, semSize, b, c uint32) uint32 {
	p := proc()

	p.sized("sem_init", semSize, sizeofSem)

	return p.libcSem_init(sem, b, c)
}

func libcSem_destroy(sem, semSize uint32) uint32 {
	p := proc()

	p.sized("sem_destroy", semSize, sizeofSem)

	return p.libcSem_destroy(sem)
}

func libcSem_wait(sem, semSize uint32) uint32 {
	p := proc()

	p.sized("sem_wait", semSize, sizeofSem)

	return p.libcSem_wait(sem)
}

func libcSem_trywait(sem, semSize uint32) uint32 {
	p := proc()

	p.sized("sem_trywait", semSize, sizeofSem)

	return p.libcSem_trywait(sem)
}

func libcSem_post(sem, semSize uint32) uint32 {
	p := proc()

	p.sized("sem_post", semSize, sizeofSem)

	return p.libcSem_post(sem)
}

func libcSem_close(sem, semSize uint32) uint32 {
	p := proc()

	p.sized("sem_close", semSize, sizeofSem)

	return p.libcSem_close(sem)
}

func libcSem_timedwait(sem, semSize, at, atSize uint32) uint32 {
	p := proc()

	p.sized("sem_timedwait", semSize, sizeofSem, atSize, sizeofTimespec)

	return p.libcSem_timedwait(sem, at)
}

func libcSem_getvalue(sem, semSize, out uint32) uint32 {
	p := proc()

	p.sized("sem_getvalue", semSize, sizeofSem)

	return p.libcSem_getvalue(sem, out)
}

func libcSem_unlink(a uint32) uint32 {
	return proc().libcSem_unlink(a)
}

func libcPthread_kill(thread, value uint32) uint32 {
	return proc().libcPthread_kill(thread, value)
}

func libcThrd_create(thread, fn, c uint32) uint32 {
	return proc().libcThrd_create(thread, fn, c)
}

func libcThrd_join(thread, out uint32) uint32 {
	return proc().libcThrd_join(thread, out)
}

func libcThrd_detach(thread uint32) uint32 {
	return proc().libcThrd_detach(thread)
}

func libcThrd_current() uint32 {
	return proc().libcThrd_current()
}

func libcThrd_equal(a, b uint32) uint32 {
	return proc().libcThrd_equal(a, b)
}

func libcThrd_sleep(a, aSize, b, bSize uint32) uint32 {
	p := proc()

	p.sized("thrd_sleep", aSize, sizeofTimespec, bSize, sizeofTimespec)

	return p.libcThrd_sleep(a, b)
}

func libcThrd_yield() {
	proc().libcThrd_yield()
}

func libcMtx_init(mutex, mutexSize, value uint32) uint32 {
	p := proc()

	p.sized("mtx_init", mutexSize, sizeofMtx)

	return p.libcMtx_init(mutex, value)
}

func libcMtx_destroy(mutex, mutexSize uint32) {
	p := proc()

	p.sized("mtx_destroy", mutexSize, sizeofMtx)

	p.libcMtx_destroy(mutex)
}

func libcMtx_lock(mutex, mutexSize uint32) uint32 {
	p := proc()

	p.sized("mtx_lock", mutexSize, sizeofMtx)

	return p.libcMtx_lock(mutex)
}

func libcMtx_trylock(mutex, mutexSize uint32) uint32 {
	p := proc()

	p.sized("mtx_trylock", mutexSize, sizeofMtx)

	return p.libcMtx_trylock(mutex)
}

func libcMtx_unlock(mutex, mutexSize uint32) uint32 {
	p := proc()

	p.sized("mtx_unlock", mutexSize, sizeofMtx)

	return p.libcMtx_unlock(mutex)
}

func libcMtx_timedlock(mutex, mutexSize, at, atSize uint32) uint32 {
	p := proc()

	p.sized("mtx_timedlock", mutexSize, sizeofMtx, atSize, sizeofTimespec)

	return p.libcMtx_timedlock(mutex, at)
}

func libcCnd_init(cond, condSize uint32) uint32 {
	p := proc()

	p.sized("cnd_init", condSize, sizeofCnd)

	return p.libcCnd_init(cond)
}

func libcCnd_destroy(cond, condSize uint32) {
	p := proc()

	p.sized("cnd_destroy", condSize, sizeofCnd)

	p.libcCnd_destroy(cond)
}

func libcCnd_broadcast(cond, condSize uint32) uint32 {
	p := proc()

	p.sized("cnd_broadcast", condSize, sizeofCnd)

	return p.libcCnd_broadcast(cond)
}

func libcCnd_signal(cond, condSize uint32) uint32 {
	p := proc()

	p.sized("cnd_signal", condSize, sizeofCnd)

	return p.libcCnd_signal(cond)
}

func libcCnd_timedwait(cond, condSize, mutex, mutexSize, at, atSize uint32) uint32 {
	p := proc()

	p.sized("cnd_timedwait", condSize, sizeofCnd, mutexSize, sizeofMtx, atSize, sizeofTimespec)

	return p.libcCnd_timedwait(cond, mutex, at)
}

func libcCnd_wait(cond, condSize, mutex, mutexSize uint32) uint32 {
	p := proc()

	p.sized("cnd_wait", condSize, sizeofCnd, mutexSize, sizeofMtx)

	return p.libcCnd_wait(cond, mutex)
}

func libcTss_create(key, fn uint32) uint32 {
	return proc().libcTss_create(key, fn)
}

func libcTss_delete(key uint32) {
	proc().libcTss_delete(key)
}

func libcTss_set(key, b uint32) uint32 {
	return proc().libcTss_set(key, b)
}

func libcTss_get(key uint32) uint32 {
	return proc().libcTss_get(key)
}

func libcGettid() uint32 {
	return proc().libcGettid()
}

func libcClock_getcpuclockid(pid, out uint32) uint32 {
	return proc().libcClock_getcpuclockid(pid, out)
}

func libcSem_open(name, flags, va, semSize uint32) uint32 {
	p := proc()

	p.sized("sem_open", semSize, sizeofSem)

	return p.libcSem_open(name, flags, va)
}

func libcMkostemps(template, suffix, flags uint32) uint32 {
	return proc().libcMkostemps(template, suffix, flags)
}

func libcSetkey(key uint32) {
	proc().libcSetkey(key)
}

func libcEncrypt(block, decrypt uint32) {
	proc().libcEncrypt(block, decrypt)
}

func libcStrerror_l(e, locale uint32) uint32 {
	return proc().libcStrerror_l(e, locale)
}

func libcStrcasecmp_l(a, b, locale uint32) uint32 {
	return proc().libcStrcasecmp_l(a, b, locale)
}

func libcStrncasecmp_l(a, b, n, locale uint32) uint32 {
	return proc().libcStrncasecmp_l(a, b, n, locale)
}

func libcLchmod(path, mode uint32) uint32 {
	return proc().libcLchmod(path, mode)
}

func libcPosix_close(fd, flags uint32) uint32 {
	return proc().libcPosix_close(fd, flags)
}

func libcBrk(end uint32) uint32 {
	return proc().libcBrk(end)
}

func libcSbrk(increment uint32) uint32 {
	return proc().libcSbrk(increment)
}

func libcIssetugid() uint32 {
	return proc().libcIssetugid()
}

func libcWcsncat(d, s, n uint32) uint32 {
	return proc().libcWcsncat(d, s, n)
}

func libcWcpcpy(d, s uint32) uint32 {
	return proc().libcWcpcpy(d, s)
}

func libcWcpncpy(d, s, n uint32) uint32 {
	return proc().libcWcpncpy(d, s, n)
}

func libcWcscasecmp(a, b uint32) uint32 {
	return proc().libcWcscasecmp(a, b)
}

func libcWcsncasecmp(a, b, n uint32) uint32 {
	return proc().libcWcsncasecmp(a, b, n)
}

func libcWcscasecmp_l(a, b, locale uint32) uint32 {
	return proc().libcWcscasecmp_l(a, b, locale)
}

func libcWcsncasecmp_l(a, b, n, locale uint32) uint32 {
	return proc().libcWcsncasecmp_l(a, b, n, locale)
}

func libcWcswcs(h, n uint32) uint32 {
	return proc().libcWcswcs(h, n)
}

func libcWcsftime_l(s, max, format, tm, tmSize, locale uint32) uint32 {
	p := proc()

	p.sized("wcsftime_l", tmSize, sizeofTm)

	return p.libcWcsftime_l(s, max, format, tm, locale)
}

func libcTowctrans_l(c, trans, locale uint32) uint32 {
	return proc().libcTowctrans_l(c, trans, locale)
}

func libcWctrans_l(name, locale uint32) uint32 {
	return proc().libcWctrans_l(name, locale)
}

func libcTimespec_get(ts, tsSize, base uint32) uint32 {
	p := proc()

	p.sized("timespec_get", tsSize, sizeofTimespec)

	return p.libcTimespec_get(ts, base)
}

func libcEventfd(count, flags uint32) uint32 {
	return proc().libcEventfd(count, flags)
}

func libcEventfd_read(fd, value uint32) uint32 {
	return proc().libcEventfd_read(fd, value)
}

func libcEventfd_write(fd uint32, value uint64) uint32 {
	return proc().libcEventfd_write(fd, value)
}

func libcTimerfd_create(clock, flags uint32) uint32 {
	return proc().libcTimerfd_create(clock, flags)
}

func libcTimerfd_settime(fd, flags, value, valueSize, old, oldSize uint32) uint32 {
	p := proc()

	p.sized("timerfd_settime", valueSize, sizeofItimerspec)
	p.sized("timerfd_settime", oldSize, sizeofItimerspec)

	return p.libcTimerfd_settime(fd, flags, value, old)
}

func libcTimerfd_gettime(fd, at, size uint32) uint32 {
	p := proc()

	p.sized("timerfd_gettime", size, sizeofItimerspec)

	return p.libcTimerfd_gettime(fd, at)
}

func libcInotify_init() uint32 {
	return proc().libcInotify_init()
}

func libcInotify_init1(flags uint32) uint32 {
	return proc().libcInotify_init1(flags)
}

func libcInotify_add_watch(fd, path, mask uint32) uint32 {
	return proc().libcInotify_add_watch(fd, path, mask)
}

func libcInotify_rm_watch(fd, wd uint32) uint32 {
	return proc().libcInotify_rm_watch(fd, wd)
}

func libcTimer_create(clock, event, eventSize, at uint32) uint32 {
	p := proc()

	p.sized("timer_create", eventSize, sizeofSigevent)

	return p.libcTimer_create(clock, event, at)
}

func libcTimer_delete(timer uint32) uint32 {
	return proc().libcTimer_delete(timer)
}

func libcTimer_settime(timer, flags, value, valueSize, old, oldSize uint32) uint32 {
	p := proc()

	p.sized("timer_settime", valueSize, sizeofItimerspec, oldSize, sizeofItimerspec)

	return p.libcTimer_settime(timer, flags, value, old)
}

func libcTimer_gettime(timer, at, size uint32) uint32 {
	p := proc()

	p.sized("timer_gettime", size, sizeofItimerspec)

	return p.libcTimer_gettime(timer, at)
}

func libcTimer_getoverrun(timer uint32) uint32 {
	return proc().libcTimer_getoverrun(timer)
}

func ioctlSize(req uint32) uint32 {
	switch req {
	case TCGETS, TCSETS, TCSETS + 1, TCSETS + 2:
		return sizeofTermios
	case TIOCGWINSZ, TIOCSWINSZ:
		return sizeofWinsize
	}

	return 0
}

func libcPthread_mutex_clocklock(mutex, mutexSize, clock, at, atSize uint32) uint32 {
	p := proc()

	p.sized("pthread_mutex_clocklock", mutexSize, sizeofPthreadMutex, atSize, sizeofTimespec)

	return p.libcPthread_mutex_clocklock(mutex, clock, at)
}

func libcPthread_cond_clockwait(cond, condSize, mutex, mutexSize, clock, at, atSize uint32) uint32 {
	p := proc()

	p.sized("pthread_cond_clockwait", condSize, sizeofPthreadCond, mutexSize, sizeofPthreadMutex, atSize, sizeofTimespec)

	return p.libcPthread_cond_clockwait(cond, mutex, clock, at)
}

func libcPthread_rwlock_clockrdlock(lock, lockSize, clock, at, atSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_clockrdlock", lockSize, sizeofPthreadRwlock, atSize, sizeofTimespec)

	return p.libcPthread_rwlock_clockrdlock(lock, clock, at)
}

func libcPthread_rwlock_clockwrlock(lock, lockSize, clock, at, atSize uint32) uint32 {
	p := proc()

	p.sized("pthread_rwlock_clockwrlock", lockSize, sizeofPthreadRwlock, atSize, sizeofTimespec)

	return p.libcPthread_rwlock_clockwrlock(lock, clock, at)
}

func libcSem_clockwait(sem, semSize, clock, at, atSize uint32) uint32 {
	p := proc()

	p.sized("sem_clockwait", semSize, sizeofSem, atSize, sizeofTimespec)

	return p.libcSem_clockwait(sem, clock, at)
}

func libcSig2str(sig, out uint32) uint32 {
	return proc().libcSig2str(sig, out)
}

func libcStr2sig(name, out uint32) uint32 {
	return proc().libcStr2sig(name, out)
}

func libcTcgetwinsize(fd, size, sizeSize uint32) uint32 {
	p := proc()

	p.sized("tcgetwinsize", sizeSize, sizeofWinsize)

	return p.libcTcgetwinsize(fd, size)
}

func libcTcsetwinsize(fd, size, sizeSize uint32) uint32 {
	p := proc()

	p.sized("tcsetwinsize", sizeSize, sizeofWinsize)

	return p.libcTcsetwinsize(fd, size)
}

func libcWcslcpy(d, s, n uint32) uint32 {
	return proc().libcWcslcpy(d, s, n)
}

func libcWcslcat(d, s, n uint32) uint32 {
	return proc().libcWcslcat(d, s, n)
}

func libcNewlocale(mask, name, base uint32) uint32 {
	return proc().libcNewlocale(mask, name, base)
}

func libcDuplocale(locale uint32) uint32 {
	return proc().libcDuplocale(locale)
}

func libcFreelocale(locale uint32) {
	proc().libcFreelocale(locale)
}

func libcUselocale(locale uint32) uint32 {
	return proc().libcUselocale(locale)
}

func libcGetlocalename_l(category, locale uint32) uint32 {
	return proc().libcGetlocalename_l(category, locale)
}

func libcGettext_l(message, locale uint32) uint32 {
	return proc().libcGettext_l(message, locale)
}

func libcDgettext_l(domain, message, locale uint32) uint32 {
	return proc().libcDgettext_l(domain, message, locale)
}

func libcDcgettext_l(domain, message, category, locale uint32) uint32 {
	return proc().libcDcgettext_l(domain, message, category, locale)
}

func libcNgettext_l(one, many, n, locale uint32) uint32 {
	return proc().libcNgettext_l(one, many, n, locale)
}

func libcDngettext_l(domain, one, many, n, locale uint32) uint32 {
	return proc().libcDngettext_l(domain, one, many, n, locale)
}

func libcDcngettext_l(domain, one, many, n, category, locale uint32) uint32 {
	return proc().libcDcngettext_l(domain, one, many, n, category, locale)
}

func libcPosix_getdents(fd, buf, n, flags uint32) uint32 {
	return proc().libcPosix_getdents(fd, buf, n, flags)
}

func libcPosix_devctl(fd, cmd, data, n, info uint32) uint32 {
	return proc().libcPosix_devctl(fd, cmd, data, n, info)
}

func libcSetservent(stay uint32) {
	proc().libcSetservent(stay)
}

func libcEndservent() {
	proc().libcEndservent()
}

func libcGetservent(size uint32) uint32 {
	p := proc()

	p.sized("getservent", size, sizeofServent)

	return p.libcGetservent()
}

func libcSetprotoent(stay uint32) {
	proc().libcSetprotoent(stay)
}

func libcEndprotoent() {
	proc().libcEndprotoent()
}

func libcGetprotoent(size uint32) uint32 {
	p := proc()

	p.sized("getprotoent", size, sizeofProtoent)

	return p.libcGetprotoent()
}

func libcSetnetent(stay uint32) {
	proc().libcSetnetent(stay)
}

func libcEndnetent() {
	proc().libcEndnetent()
}

func libcGetnetent(size uint32) uint32 {
	p := proc()

	p.sized("getnetent", size, sizeofNetent)

	return p.libcGetnetent()
}

func libcGethostbyname_r(name, ret, retSize, buf, n, result, resultSize, herr uint32) uint32 {
	p := proc()

	p.sized("gethostbyname_r", retSize, sizeofHostent, resultSize, sizeofHostent)

	return p.libcGethostbyname_r(name, ret, buf, n, result, herr)
}

func libcGethostbyname2_r(name, family, ret, retSize, buf, n, result, resultSize, herr uint32) uint32 {
	p := proc()

	p.sized("gethostbyname2_r", retSize, sizeofHostent, resultSize, sizeofHostent)

	return p.libcGethostbyname2_r(name, family, ret, buf, n, result, herr)
}

func libcGethostbyaddr_r(addr, length, family, ret, retSize, buf, n, result, resultSize, herr uint32) uint32 {
	p := proc()

	p.sized("gethostbyaddr_r", retSize, sizeofHostent, resultSize, sizeofHostent)

	return p.libcGethostbyaddr_r(addr, length, family, ret, buf, n, result, herr)
}

func libcGetservbyname_r(name, proto, ret, retSize, buf, n, result, resultSize uint32) uint32 {
	p := proc()

	p.sized("getservbyname_r", retSize, sizeofServent, resultSize, sizeofServent)

	return p.libcGetservbyname_r(name, proto, ret, buf, n, result)
}

func libcGetservbyport_r(port, proto, ret, retSize, buf, n, result, resultSize uint32) uint32 {
	p := proc()

	p.sized("getservbyport_r", retSize, sizeofServent, resultSize, sizeofServent)

	return p.libcGetservbyport_r(port, proto, ret, buf, n, result)
}

func libcMq_open(name, flags, va, attrSize uint32) uint32 {
	p := proc()

	p.sized("mq_open", attrSize, sizeofMqAttr)

	return p.libcMq_open(name, flags, va)
}

func libcMq_close(fd uint32) uint32 {
	return proc().libcMq_close(fd)
}

func libcMq_unlink(name uint32) uint32 {
	return proc().libcMq_unlink(name)
}

func libcMq_send(fd, msg, n, prio uint32) uint32 {
	return proc().libcMq_send(fd, msg, n, prio)
}

func libcMq_timedsend(fd, msg, n, prio, at, atSize uint32) uint32 {
	p := proc()

	p.sized("mq_timedsend", atSize, sizeofTimespec)

	return p.libcMq_timedsend(fd, msg, n, prio, at)
}

func libcMq_receive(fd, buf, n, prio uint32) uint32 {
	return proc().libcMq_receive(fd, buf, n, prio)
}

func libcMq_timedreceive(fd, buf, n, prio, at, atSize uint32) uint32 {
	p := proc()

	p.sized("mq_timedreceive", atSize, sizeofTimespec)

	return p.libcMq_timedreceive(fd, buf, n, prio, at)
}

func libcMq_getattr(fd, attr, attrSize uint32) uint32 {
	p := proc()

	p.sized("mq_getattr", attrSize, sizeofMqAttr)

	return p.libcMq_getattr(fd, attr)
}

func libcMq_setattr(fd, attr, attrSize, old, oldSize uint32) uint32 {
	p := proc()

	p.sized("mq_setattr", attrSize, sizeofMqAttr, oldSize, sizeofMqAttr)

	return p.libcMq_setattr(fd, attr, old)
}

func libcMq_notify(fd, event, eventSize uint32) uint32 {
	p := proc()

	p.sized("mq_notify", eventSize, sizeofSigevent)

	return p.libcMq_notify(fd, event)
}

func libcFtok(path, id uint32) uint32 {
	return proc().libcFtok(path, id)
}

func libcMsgget(key, flags uint32) uint32 {
	return proc().libcMsgget(key, flags)
}

func libcMsgsnd(id, msg, n, flags uint32) uint32 {
	return proc().libcMsgsnd(id, msg, n, flags)
}

func libcMsgrcv(id, msg, n, mtype, flags uint32) uint32 {
	return proc().libcMsgrcv(id, msg, n, mtype, flags)
}

func libcMsgctl(id, cmd, buf, bufSize uint32) uint32 {
	p := proc()

	p.sized("msgctl", bufSize, sizeofMsqidDs)

	return p.libcMsgctl(id, cmd, buf)
}

func libcSemget(key, n, flags uint32) uint32 {
	return proc().libcSemget(key, n, flags)
}

func libcSemop(id, ops, opsSize, n uint32) uint32 {
	p := proc()

	p.sized("semop", opsSize, sizeofSembuf)

	return p.libcSemop(id, ops, n)
}

func libcSemtimedop(id, ops, opsSize, n, at, atSize uint32) uint32 {
	p := proc()

	p.sized("semtimedop", opsSize, sizeofSembuf, atSize, sizeofTimespec)

	return p.libcSemtimedop(id, ops, n, at)
}

func libcSemctl(id, num, cmd, va, dsSize uint32) uint32 {
	p := proc()

	p.sized("semctl", dsSize, sizeofSemidDs)

	return p.libcSemctl(id, num, cmd, va)
}

func libcSetutxent() {
	proc().libcSetutxent()
}

func libcEndutxent() {
	proc().libcEndutxent()
}

func libcGetutxent(size uint32) uint32 {
	p := proc()

	p.sized("getutxent", size, sizeofUtmpx)

	return p.libcGetutxent()
}

func libcGetutxid(id, idSize, size uint32) uint32 {
	p := proc()

	p.sized("getutxid", idSize, sizeofUtmpx, size, sizeofUtmpx)

	return p.libcGetutxid(id)
}

func libcGetutxline(line, lineSize, size uint32) uint32 {
	p := proc()

	p.sized("getutxline", lineSize, sizeofUtmpx, size, sizeofUtmpx)

	return p.libcGetutxline(line)
}

func libcPututxline(ut, utSize, size uint32) uint32 {
	p := proc()

	p.sized("pututxline", utSize, sizeofUtmpx, size, sizeofUtmpx)

	return p.libcPututxline(ut)
}

func libcAio_read(cb, cbSize uint32) uint32 {
	p := proc()

	p.sized("aio_read", cbSize, sizeofAiocb)

	return p.libcAio_read(cb)
}

func libcAio_write(cb, cbSize uint32) uint32 {
	p := proc()

	p.sized("aio_write", cbSize, sizeofAiocb)

	return p.libcAio_write(cb)
}

func libcAio_fsync(op, cb, cbSize uint32) uint32 {
	p := proc()

	p.sized("aio_fsync", cbSize, sizeofAiocb)

	return p.libcAio_fsync(op, cb)
}

func libcAio_error(cb, cbSize uint32) uint32 {
	p := proc()

	p.sized("aio_error", cbSize, sizeofAiocb)

	return p.libcAio_error(cb)
}

func libcAio_return(cb, cbSize uint32) uint32 {
	p := proc()

	p.sized("aio_return", cbSize, sizeofAiocb)

	return p.libcAio_return(cb)
}

func libcAio_suspend(list, listSize, n, timeout, timeoutSize uint32) uint32 {
	p := proc()

	p.sized("aio_suspend", listSize, sizeofAiocb, timeoutSize, sizeofTimespec)

	return p.libcAio_suspend(list, n, timeout)
}

func libcAio_cancel(fd, cb, cbSize uint32) uint32 {
	p := proc()

	p.sized("aio_cancel", cbSize, sizeofAiocb)

	return p.libcAio_cancel(fd, cb)
}

func libcLio_listio(mode, list, listSize, n, sig, sigSize uint32) uint32 {
	p := proc()

	p.sized("lio_listio", listSize, sizeofAiocb, sigSize, sizeofSigevent)

	return p.libcLio_listio(mode, list, n, sig)
}

func libcGetrandom(buf, n, flags uint32) uint32 {
	return proc().libcGetrandom(buf, n, flags)
}

func libcUlimit(cmd, va uint32) uint32 {
	return proc().libcUlimit(cmd, va)
}

func libcPosix_madvise(addr, n, advice uint32) uint32 {
	return proc().libcPosix_madvise(addr, n, advice)
}

func libcFmtmsg(classification, label, severity, text, action, tag uint32) uint32 {
	return proc().libcFmtmsg(classification, label, severity, text, action, tag)
}

func libcEther_ntoa(addr uint32) uint32 {
	return proc().libcEther_ntoa(addr)
}

func libcEther_ntoa_r(addr, buf uint32) uint32 {
	return proc().libcEther_ntoa_r(addr, buf)
}

func libcEther_aton(text uint32) uint32 {
	return proc().libcEther_aton(text)
}

func libcEther_aton_r(text, addr uint32) uint32 {
	return proc().libcEther_aton_r(text, addr)
}

func libcEther_ntohost(host, addr uint32) uint32 {
	return proc().libcEther_ntohost(host, addr)
}

func libcEther_hostton(host, addr uint32) uint32 {
	return proc().libcEther_hostton(host, addr)
}

func libcEther_line(line, addr, host uint32) uint32 {
	return proc().libcEther_line(line, addr, host)
}
