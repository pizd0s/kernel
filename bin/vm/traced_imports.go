//go:build linux && amd64

package main

import (
	"reflect"
	"strings"
)

var libraryCalls = func() map[string]bool {
	names := strings.Fields(`
		abs aligned_alloc alphasort asctime asctime_r basename bcmp bcopy bzero calloc cfgetispeed cfgetospeed cfmakeraw cfsetispeed
		cfsetospeed cfsetspeed clearenv crypt difftime dirname div explicit_bzero ffs ffsl ffsll fnmatch free getenv getpagesize
		getsubopt globfree gmtime gmtime_r imaxabs imaxdiv index inet_aton inet_ntoa inet_ntop inet_pton initstate labs ldiv llabs lldiv
		malloc malloc_usable_size mallopt memalign memccpy memchr memcmp memcpy memmem memmove mempcpy memrchr memset posix_memalign
		putenv rand rand_r random realloc reallocarray rindex secure_getenv setenv setstate sigaddset sigandset sigdelset sigemptyset
		sigfillset sigisemptyset sigismember sigorset srand srandom stpcpy stpncpy strcasecmp strcasestr strcat strchr strchrnul strcmp
		strcpy strcspn strdup strerror strerror_r strlcat strlcpy strlen strncasecmp strncat strncmp strncpy strndup strnlen strpbrk
		strrchr strsep strsignal strspn strstr strtok strtok_r strverscmp swab timegm unsetenv versionsort
	`)

	out := make(map[string]bool, len(names))

	for _, name := range names {
		out[name] = true
	}

	return out
}()

var untracedCalls = map[string]bool{
	"setjmp": true, "_setjmp": true, "sigsetjmp": true, "longjmp": true, "_longjmp": true, "siglongjmp": true,
	"vfork": true, "_Fork": true, "__kernel_begin": true, "__errno_location": true, "__kernel_program": true, "__kernel_name": true, "__assert_fail": true,
}

var callFormats = func() map[string]string {
	out := map[string]string{}

	for _, word := range strings.Fields(`
		_Exit:vd _Fork:d __assert_fail:vssds __dtoa:sfddppp __errno_location:p __glob_home:dsdppds __hdtoa:sfsdppp __hldtoa:sfsdppp
		__ldtoa:spddppp __pthread_cancelled:dd __pthread_key_take:ppp __pthread_once_begin:dp __pthread_once_end:vp __sched_cpucount:ddp
		_exit:vd _longjmp:vpdd _setjmp:dpd abort:v abs:dd accept:ddpp accept4:ddppd access:dsd acct:ds adjtime:dpdpd aio_cancel:ddpd
		aio_error:dpd aio_fsync:ddpd aio_read:dpd aio_return:dpd aio_suspend:dpddpd aio_write:dpd alarm:dd aligned_alloc:pdd
		alphasort:dpdpd asctime:spd asctime_r:spdp atof:fs atoi:ds atol:ds atoll:ds attach:dd basename:sp bcmp:dppd bcopy:vppd bind:ddpd
		bzero:vpd calloc:pdd cfgetispeed:dpd cfgetospeed:dpd cfmakeraw:vpd cfsetispeed:dpdd cfsetospeed:dpdd cfsetspeed:dpdd chdir:ds
		chmod:dso chown:dsdd chroot:ds clearenv:d clock:d clock_getcpuclockid:ddp clock_getres:ddpd clock_gettime:ddpd
		clock_nanosleep:dddpdpd clock_settime:ddpd close:dd closedir:dp cnd_broadcast:dpd cnd_destroy:vpd cnd_init:dpd cnd_signal:dpd
		cnd_timedwait:dpdpdpd cnd_wait:dpdpd confstr:ddpd connect:ddpd copy_file_range:ddpdpdd creat:dso crypt:sss ctermid:sp difftime:fdd
		dirfd:dp dirname:sp div:ddd dup:dd dup2:ddd dup3:dddd eaccess:dsd epoll_create:dd epoll_create1:dd epoll_ctl:ddddpd
		epoll_pwait:ddpdddpd epoll_wait:ddpddd eventfd:ddd eventfd_read:ddp eventfd_write:ddd execl:dssp execle:dssp execmode:dddd
		execv:dsp execve:dspp explicit_bzero:vpd faccessat:ddsdd fchdir:dd fchmod:ddo fchmodat:ddsod fchown:dddd fchownat:ddsddd
		fcntl:dddpd fdatasync:dd fdopendir:pd fexecve:ddpp ffs:dd ffsl:dd ffsll:dd flock:ddd fnmatch:dssd fpathconf:ddd free:vp fstat:ddpd
		fstatat:ddspdd fstatfs:ddpd fstatvfs:ddpd fsync:dd ftok:dsd ftruncate:ddd futimens:ddpd futimes:ddpd get_avphys_pages:d
		get_nprocs:d get_nprocs_conf:d get_phys_pages:d getcwd:spd getdomainname:dpd getdtablesize:d getegid:d getentropy:dpd getenv:ss
		geteuid:d getgid:d getgroups:ddp gethostid:d gethostname:dpd getitimer:ddpd getloadavg:dpd getlogin:s getlogin_r:dpd getpagesize:d
		getpeername:ddpp getpgid:dd getpgrp:d getpid:d getppid:d getpriority:ddd getrandom:dpdd getresgid:dppp getresuid:dppp
		getrlimit:ddpd getrusage:ddpd getsid:dd getsockname:ddpp getsockopt:ddddpp getsubopt:dppp gettid:d gettimeofday:dpdp getuid:d
		globfree:vpd gmtime:ppd gmtime_r:pppdd grantpt:dd iconv:ddpppp iconv_close:dd iconv_open:dss imaxabs:dd imaxdiv:ddd index:ssd
		inet_aton:dspd inet_ntoa:sd inet_ntop:sdppd inet_pton:ddsp initstate:sdpd inotify_add_watch:ddsd inotify_init:d inotify_init1:dd
		inotify_rm_watch:ddd ioctl:dddpd isatty:dd issetugid:d kill:ddd killpg:ddd labs:dd lchmod:dso lchown:dsdd ldiv:ddd link:dss
		linkat:ddsdsd lio_listio:ddpddpd listen:ddd llabs:dd lldiv:ddd lockf:dddd longjmp:vpdd lseek:dddd lstat:dspd lutimes:dspd
		madvise:dpdd malloc:pd malloc_usable_size:dp mallopt:ddd memalign:pdd memccpy:pppdd memchr:ppdd memcmp:dppd memcpy:pppd
		memfile:dpdd memmem:ppdpd memmove:pppd mempcpy:pppd memrchr:ppdd memset:ppdd mkdir:dso mkdirat:ddso mkdtemp:sp mkfifo:dso
		mkfifoat:ddso mknod:dsod mknodat:ddsod mkostemp:dpd mkostemps:dpdd mkstemp:dp mkstemps:dpd mktemp:sp mlock:dpd mlockall:dd
		mmap:ppddddd mount:dsssdp mprotect:dpdd mq_close:dd mq_getattr:ddpd mq_notify:ddpd mq_open:dsdpd mq_receive:ddpdp mq_send:ddsdd
		mq_setattr:ddpdpd mq_timedreceive:ddpdppd mq_timedsend:ddsddpd mq_unlink:ds msgctl:dddpd msgget:ddd msgrcv:ddpddd msgsnd:ddpdd
		msync:dpdd mtx_destroy:vpd mtx_init:dpdd mtx_lock:dpd mtx_timedlock:dpdpd mtx_trylock:dpd mtx_unlock:dpd munlock:dpd munlockall:d
		munmap:dpd nanosleep:dpdpd nice:dd open:dsdp openat:ddsdp opendir:ps pathconf:dsd pause:d pipe:dp pipe2:dpd poll:dpddd
		posix_close:ddd posix_devctl:dddpdp posix_fadvise:ddddd posix_fallocate:dddd posix_getdents:ddpdd posix_madvise:dpdd
		posix_memalign:dpdd posix_spawn:dpspdpdpp posix_spawn_file_actions_addchdir:dpds posix_spawn_file_actions_addchdir_np:dpds
		posix_spawn_file_actions_addclose:dpdd posix_spawn_file_actions_adddup2:dpddd posix_spawn_file_actions_addfchdir:dpdd
		posix_spawn_file_actions_addfchdir_np:dpdd posix_spawn_file_actions_addopen:dpddsdo posix_spawn_file_actions_destroy:dpd
		posix_spawn_file_actions_init:dpd posix_spawnattr_destroy:dpd posix_spawnattr_getflags:dpdp posix_spawnattr_getpgroup:dpdp
		posix_spawnattr_getschedparam:dpdpd posix_spawnattr_getschedpolicy:dpdp posix_spawnattr_getsigdefault:dpdpd
		posix_spawnattr_getsigmask:dpdpd posix_spawnattr_init:dpd posix_spawnattr_setflags:dpdd posix_spawnattr_setpgroup:dpdd
		posix_spawnattr_setschedparam:dpdpd posix_spawnattr_setschedpolicy:dpdd posix_spawnattr_setsigdefault:dpdpd
		posix_spawnattr_setsigmask:dpdpd ppoll:dpddpdpd prctl:ddp pread:ddpdd preadv:ddpddd prlimit:dddpdpd pselect:ddpdpdpdpdpd
		psiginfo:vpds psignal:vds pthread_attr_destroy:dpd pthread_attr_getdetachstate:dpdp pthread_attr_getguardsize:dpdp
		pthread_attr_getinheritsched:dpdp pthread_attr_getschedparam:dpdpd pthread_attr_getschedpolicy:dpdp pthread_attr_getscope:dpdp
		pthread_attr_getstack:dpdpp pthread_attr_getstacksize:dpdp pthread_attr_init:dpd pthread_attr_setdetachstate:dpdd
		pthread_attr_setguardsize:dpdd pthread_attr_setinheritsched:dpdd pthread_attr_setschedparam:dpdpd pthread_attr_setschedpolicy:dpdd
		pthread_attr_setscope:dpdd pthread_attr_setstack:dpdpd pthread_attr_setstacksize:dpdd pthread_barrier_destroy:dpd
		pthread_barrier_init:dpdpdd pthread_barrier_wait:dpd pthread_barrierattr_destroy:dpd pthread_barrierattr_getpshared:dpdp
		pthread_barrierattr_init:dpd pthread_barrierattr_setpshared:dpdd pthread_cancel:dd pthread_cond_broadcast:dpd
		pthread_cond_clockwait:dpdpddpd pthread_cond_destroy:dpd pthread_cond_init:dpdpd pthread_cond_signal:dpd
		pthread_cond_timedwait:dpdpdpd pthread_cond_wait:dpdpd pthread_condattr_destroy:dpd pthread_condattr_getclock:dpdp
		pthread_condattr_getpshared:dpdp pthread_condattr_init:dpd pthread_condattr_setclock:dpdd pthread_condattr_setpshared:dpdd
		pthread_create:dppdpp pthread_detach:dd pthread_equal:ddd pthread_getaffinity_np:dddp pthread_getattr_np:ddpd
		pthread_getcpuclockid:ddp pthread_getname_np:ddpd pthread_getschedparam:ddppd pthread_getspecific:pd pthread_join:ddp
		pthread_key_create:dpp pthread_key_delete:dd pthread_kill:ddd pthread_mutex_clocklock:dpddpd pthread_mutex_consistent:dpd
		pthread_mutex_destroy:dpd pthread_mutex_getprioceiling:dpdp pthread_mutex_init:dpdpd pthread_mutex_lock:dpd
		pthread_mutex_setprioceiling:dpddp pthread_mutex_timedlock:dpdpd pthread_mutex_trylock:dpd pthread_mutex_unlock:dpd
		pthread_mutexattr_destroy:dpd pthread_mutexattr_getprioceiling:dpdp pthread_mutexattr_getprotocol:dpdp
		pthread_mutexattr_getpshared:dpdp pthread_mutexattr_getrobust:dpdp pthread_mutexattr_gettype:dpdp pthread_mutexattr_init:dpd
		pthread_mutexattr_setprioceiling:dpdd pthread_mutexattr_setprotocol:dpdd pthread_mutexattr_setpshared:dpdd
		pthread_mutexattr_setrobust:dpdd pthread_mutexattr_settype:dpdd pthread_rwlock_clockrdlock:dpddpd
		pthread_rwlock_clockwrlock:dpddpd pthread_rwlock_destroy:dpd pthread_rwlock_init:dpdpd pthread_rwlock_rdlock:dpd
		pthread_rwlock_timedrdlock:dpdpd pthread_rwlock_timedwrlock:dpdpd pthread_rwlock_tryrdlock:dpd pthread_rwlock_trywrlock:dpd
		pthread_rwlock_unlock:dpd pthread_rwlock_wrlock:dpd pthread_rwlockattr_destroy:dpd pthread_rwlockattr_getpshared:dpdp
		pthread_rwlockattr_init:dpd pthread_rwlockattr_setpshared:dpdd pthread_self:d pthread_setaffinity_np:dddp
		pthread_setcancelstate:ddp pthread_setcanceltype:ddp pthread_setname_np:dds pthread_setschedparam:dddpd pthread_setschedprio:ddd
		pthread_setspecific:ddp pthread_sigmask:ddpdpd pthread_spin_destroy:dp pthread_spin_init:dpd pthread_spin_lock:dp
		pthread_spin_trylock:dp pthread_spin_unlock:dp pthread_timedjoin_np:ddppd pthread_tryjoin_np:ddp ptsname:sd ptsname_r:ddpd
		putenv:dp pwrite:ddpdd pwritev:ddpddd raise:dd rand:d rand_r:dp random:d read:ddpd readdir:ppd readdir_r:dppdpd readlink:dspd
		readlinkat:ddspd readv:ddpdd realloc:ppd reallocarray:ppdd realpath:ssp recv:ddpdd recvfrom:ddpddpp recvmmsg:ddpddddpd
		recvmsg:ddpddd remove:ds rename:dss renameat:ddsds rewinddir:vp rindex:ssd rmdir:ds sched_get_priority_max:dd
		sched_get_priority_min:dd sched_getaffinity:dddp sched_getcpu:d sched_getparam:ddpd sched_getscheduler:dd
		sched_rr_get_interval:ddpd sched_setaffinity:dddp sched_setparam:ddpd sched_setscheduler:dddpd sched_yield:d secure_getenv:ss
		seekdir:vpd select:ddpdpdpdpd sem_clockwait:dpddpd sem_close:dpd sem_destroy:dpd sem_getvalue:dpdp sem_init:dpddd sem_open:psdpd
		sem_post:dpd sem_timedwait:dpdpd sem_trywait:dpd sem_unlink:ds sem_wait:dpd semctl:ddddpd semget:dddd semop:ddpdd
		semtimedop:ddpddpd send:ddpdd sendmmsg:ddpdddd sendmsg:ddpddd sendto:ddpddpd setdomainname:dsd setegid:dd setenv:dssd seteuid:dd
		setgid:dd setgroups:ddp sethostname:dsd setitimer:ddpdpd setjmp:dpd setpgid:ddd setpgrp:d setpriority:dddd setregid:ddd
		setresgid:dddd setresuid:dddd setreuid:ddd setrlimit:ddpd setsid:d setsockopt:ddddpd setstate:sp settimeofday:dpdpd setuid:dd
		shutdown:ddd sig2str:ddp sigaction:ddpdpdd sigaddset:dpdd sigaltstack:dpdpd sigandset:dpdpdpd sigdelset:dpdd sigemptyset:dpd
		sigfillset:dpd sigisemptyset:dpd sigismember:dpdd siglongjmp:vpdd signal:ddd sigorset:dpdpdpd sigpending:dpd sigprocmask:ddpdpd
		sigqueue:dddd sigsetjmp:dpdd sigsuspend:dpd sigtimedwait:dpdpdpd sigwait:dpdp sigwaitinfo:dpdpd sleep:dd sockatmark:dd socket:dddd
		socketpair:ddddp srand:vd srandom:vd stat:dspd statfs:dspd statvfs:dspd stpcpy:sps stpncpy:spsd str2sig:dsp strcasecmp:dss
		strcasestr:sss strcat:sps strchr:ssd strchrnul:ssd strcmp:dss strcpy:sps strcspn:dss strdup:ss strerror:sd strerror_r:ddpd
		strlcat:dpsd strlcpy:dpsd strlen:ds strncasecmp:dssd strncat:spsd strncmp:dssd strncpy:spsd strndup:ssd strnlen:dsd strpbrk:sss
		strrchr:ssd strsep:sps strsignal:sd strspn:dss strstr:sss strtod:fsp strtof:fsp strtoimax:dspd strtok:sps strtok_r:spsp
		strtol:dspd strtold:fsp strtoll:dspd strtoul:dspd strtoull:dspd strtoumax:dspd strverscmp:dss swab:vppd symlink:dss symlinkat:dsds
		sync:v syncfs:dd sysconf:dd sysinfo:dpd tcdrain:dd tcflow:ddd tcflush:ddd tcgetattr:ddpd tcgetpgrp:dd tcgetsid:dd
		tcgetwinsize:ddpd tcsendbreak:ddd tcsetattr:dddpd tcsetpgrp:ddd tcsetwinsize:ddpd telldir:dp thrd_create:dpdp thrd_current:d
		thrd_detach:dd thrd_equal:ddd thrd_join:ddp thrd_sleep:dpdpd thrd_yield:v time:dp timegm:dpd timer_create:ddpdp timer_delete:dd
		timer_getoverrun:dd timer_gettime:ddpd timer_settime:dddpdpd timerfd_create:ddd timerfd_gettime:ddpd timerfd_settime:dddpdpd
		times:dpd timespec_get:dpdd truncate:dsd tss_create:dpd tss_delete:vd tss_get:pd tss_set:ddp ttyname:sd ttyname_r:ddpd umask:oo
		umount:ds umount2:dsd uname:dpd unlink:ds unlinkat:ddsd unlockpt:dd unsetenv:ds usleep:dd utime:dspd utimensat:ddspdd utimes:dspd
		versionsort:dpdpd vfork:d wait:dp wait3:dpdpd wait4:ddpdpd waitid:dddpdd waitpid:ddpd watchdir:ddd wcstod:fpp wcstof:fpp
		wcstoimax:dppd wcstol:dppd wcstold:fpp wcstoll:dppd wcstoul:dppd wcstoull:dppd wcstoumax:dppd write:ddpd writev:ddpdd
`) {
		name, format, _ := strings.Cut(word, ":")

		out[name] = format
	}

	return out
}()

type TracedImports struct {
	k       *Kernel
	classes uint32
}

func (t *TracedImports) function(imp *ImportMeta) any {
	f := t.k.function(imp)

	if f == nil || untracedCalls[imp.name] {
		return f
	}

	if _, code := f.(uintptr); code {
		return f
	}

	class := uint32(TRACE_SYSTEM)

	if libraryCalls[imp.name] {
		class = TRACE_LIBRARY
	}

	if t.classes&class == 0 {
		return f
	}

	real, name := reflect.ValueOf(f), imp.name

	return Closure{f: reflect.MakeFunc(real.Type(), func(args []reflect.Value) []reflect.Value {
		p := current().proc

		if p == nil {
			return real.Call(args)
		}

		p.failure = 0

		out := real.Call(args)

		p.traceCall(class, name, args, out, p.failure)

		return out
	}).Interface()}
}
