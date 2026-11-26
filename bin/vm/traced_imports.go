package main

import (
	"reflect"
	"strings"
)

var libraryCalls = func() map[string]bool {
	names := strings.Fields(`
		abs aligned_alloc bcmp bcopy bzero calloc div explicit_bzero ffs ffsl ffsll free getpagesize imaxabs imaxdiv index labs ldiv
		llabs lldiv malloc malloc_usable_size mallopt memalign memccpy memchr memcmp memcpy memmem memmove mempcpy memrchr memset
		posix_memalign realloc reallocarray rindex stpcpy stpncpy strcasecmp strcasestr strcat strchr strchrnul strcmp strcpy strcspn
		strdup strlcat strlcpy strlen strncasecmp strncat strncmp strncpy strndup strnlen strpbrk strrchr strsep strspn strstr strtok
		strtok_r swab
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
		_Exit:vd _Fork:d __errno_location:p __pthread_cancelled:dd __pthread_key_take:ppp __pthread_once_begin:dp __pthread_once_end:vp
		__sched_cpucount:ddp _exit:vd _longjmp:vpdd _setjmp:dpd abort:v abs:dd acct:ds adjtime:dpdpd aio_cancel:ddpd aio_error:dpd
		aio_fsync:ddpd aio_read:dpd aio_return:dpd aio_suspend:dpddpd aio_write:dpd aligned_alloc:pdd attach:dd bcmp:dppd bcopy:vppd
		bzero:vpd calloc:pdd clock_getcpuclockid:ddp clock_getres:ddpd clock_gettime:ddpd clock_nanosleep:dddpdpd clock_settime:ddpd
		close:dd cnd_broadcast:dpd cnd_destroy:vpd cnd_init:dpd cnd_signal:dpd cnd_timedwait:dpdpdpd cnd_wait:dpdpd
		copy_file_range:ddpdpdd ctl:ddcdd div:ddd dup:dd dup2:ddd dup3:dddd epoll_create:dd epoll_create1:dd epoll_ctl:ddddpd
		epoll_pwait:ddpdddpd epoll_wait:ddpddd eventfd:ddd eventfd_read:ddp eventfd_write:ddd execmode:dddd explicit_bzero:vpd fchdir:dd
		fchmod:ddo fchown:dddd fchroot:dd fcntl:dddpd fdatasync:dd fdid:ddp ffs:dd ffsl:dd ffsll:dd flock:ddd fpathconf:ddd free:vp
		fstat:ddpd fstatfs:ddpd fstatvfs:ddpd fsync:dd ftruncate:ddd futimens:ddpd get_avphys_pages:d get_nprocs:d get_nprocs_conf:d
		get_phys_pages:d getdtablesize:d getegid:d geteuid:d getgid:d getgroups:ddp gethostid:d getitimer:ddpd getloadavg:dpd
		getpagesize:d getpgid:dd getpgrp:d getpid:d getppid:d getpriority:ddd getrandom:dpdd getresgid:dppp getresuid:dppp getrlimit:ddpd
		getrusage:ddpd getsid:dd gettid:d getuid:d getumask:o iconv:ddpppp iconv_close:dd iconv_open:dss imaxabs:dd imaxdiv:ddd index:ssd
		issetugid:d kill:ddd labs:dd ldiv:ddd lio_listio:ddpddpd llabs:dd lldiv:ddd longjmp:vpdd lseek:dddd madvise:dpdd malloc:pd
		malloc_usable_size:dp mallopt:ddd memalign:pdd memccpy:pppdd memchr:ppdd memcmp:dppd memcpy:pppd memfile:dpdd memmem:ppdpd
		memmove:pppd mempcpy:pppd memrchr:ppdd memset:ppdd mlock:dpd mlockall:dd mmap:ppddddd mprotect:dpdd msync:dpdd mtx_destroy:vpd
		mtx_init:dpdd mtx_lock:dpd mtx_timedlock:dpdpd mtx_trylock:dpd mtx_unlock:dpd munlock:dpd munlockall:d munmap:dpd nanosleep:dpdpd
		pause:d pipe:dp pipe2:dpd poll:dpddd posix_fadvise:ddddd posix_fallocate:dddd posix_getdents:ddpdd posix_madvise:dpdd
		posix_memalign:dpdd ppoll:dpddpdpd prctl:ddp pread:ddpdd preadv:ddpddd prlimit:dddpdpd pselect:ddpdpdpdpdpd
		pthread_attr_destroy:dpd pthread_attr_getdetachstate:dpdp pthread_attr_getguardsize:dpdp pthread_attr_getinheritsched:dpdp
		pthread_attr_getschedparam:dpdpd pthread_attr_getschedpolicy:dpdp pthread_attr_getscope:dpdp pthread_attr_getstack:dpdpp
		pthread_attr_getstacksize:dpdp pthread_attr_init:dpd pthread_attr_setdetachstate:dpdd pthread_attr_setguardsize:dpdd
		pthread_attr_setinheritsched:dpdd pthread_attr_setschedparam:dpdpd pthread_attr_setschedpolicy:dpdd pthread_attr_setscope:dpdd
		pthread_attr_setstack:dpdpd pthread_attr_setstacksize:dpdd pthread_barrier_destroy:dpd pthread_barrier_init:dpdpdd
		pthread_barrier_wait:dpd pthread_barrierattr_destroy:dpd pthread_barrierattr_getpshared:dpdp pthread_barrierattr_init:dpd
		pthread_barrierattr_setpshared:dpdd pthread_cancel:dd pthread_cond_broadcast:dpd pthread_cond_clockwait:dpdpddpd
		pthread_cond_destroy:dpd pthread_cond_init:dpdpd pthread_cond_signal:dpd pthread_cond_timedwait:dpdpdpd pthread_cond_wait:dpdpd
		pthread_condattr_destroy:dpd pthread_condattr_getclock:dpdp pthread_condattr_getpshared:dpdp pthread_condattr_init:dpd
		pthread_condattr_setclock:dpdd pthread_condattr_setpshared:dpdd pthread_create:dppdpp pthread_detach:dd pthread_equal:ddd
		pthread_getaffinity_np:dddp pthread_getattr_np:ddpd pthread_getcpuclockid:ddp pthread_getname_np:ddpd pthread_getschedparam:ddppd
		pthread_getspecific:pd pthread_join:ddp pthread_key_create:dpp pthread_key_delete:dd pthread_kill:ddd
		pthread_mutex_clocklock:dpddpd pthread_mutex_consistent:dpd pthread_mutex_destroy:dpd pthread_mutex_getprioceiling:dpdp
		pthread_mutex_init:dpdpd pthread_mutex_lock:dpd pthread_mutex_setprioceiling:dpddp pthread_mutex_timedlock:dpdpd
		pthread_mutex_trylock:dpd pthread_mutex_unlock:dpd pthread_mutexattr_destroy:dpd pthread_mutexattr_getprioceiling:dpdp
		pthread_mutexattr_getprotocol:dpdp pthread_mutexattr_getpshared:dpdp pthread_mutexattr_getrobust:dpdp
		pthread_mutexattr_gettype:dpdp pthread_mutexattr_init:dpd pthread_mutexattr_setprioceiling:dpdd pthread_mutexattr_setprotocol:dpdd
		pthread_mutexattr_setpshared:dpdd pthread_mutexattr_setrobust:dpdd pthread_mutexattr_settype:dpdd
		pthread_rwlock_clockrdlock:dpddpd pthread_rwlock_clockwrlock:dpddpd pthread_rwlock_destroy:dpd pthread_rwlock_init:dpdpd
		pthread_rwlock_rdlock:dpd pthread_rwlock_timedrdlock:dpdpd pthread_rwlock_timedwrlock:dpdpd pthread_rwlock_tryrdlock:dpd
		pthread_rwlock_trywrlock:dpd pthread_rwlock_unlock:dpd pthread_rwlock_wrlock:dpd pthread_rwlockattr_destroy:dpd
		pthread_rwlockattr_getpshared:dpdp pthread_rwlockattr_init:dpd pthread_rwlockattr_setpshared:dpdd pthread_self:d
		pthread_setaffinity_np:dddp pthread_setcancelstate:ddp pthread_setcanceltype:ddp pthread_setname_np:dds
		pthread_setschedparam:dddpd pthread_setschedprio:ddd pthread_setspecific:ddp pthread_sigmask:ddpdpd pthread_spin_destroy:dp
		pthread_spin_init:dpd pthread_spin_lock:dp pthread_spin_trylock:dp pthread_spin_unlock:dp pthread_timedjoin_np:ddppd
		pthread_tryjoin_np:ddp pwrite:ddpdd pwritev:ddpddd read:ddpd readv:ddpdd realloc:ppd reallocarray:ppdd rindex:ssd
		sched_get_priority_max:dd sched_get_priority_min:dd sched_getaffinity:dddp sched_getcpu:d sched_getparam:ddpd
		sched_getscheduler:dd sched_rr_get_interval:ddpd sched_setaffinity:dddp sched_setparam:ddpd sched_setscheduler:dddpd sched_yield:d
		select:ddpdpdpdpd sem_clockwait:dpddpd sem_close:dpd sem_destroy:dpd sem_getvalue:dpdp sem_init:dpddd sem_open:psdpd sem_post:dpd
		sem_timedwait:dpdpd sem_trywait:dpd sem_unlink:ds sem_wait:dpd setdomainname:dsd setegid:dd seteuid:dd setgid:dd setgroups:ddp
		sethostname:dsd setitimer:ddpdpd setjmp:dpd setpgid:ddd setpriority:dddd setregid:ddd setresgid:dddd setresuid:dddd setreuid:ddd
		setrlimit:ddpd setsid:d settimeofday:dpdpd setuid:dd sigaction:ddpdpdd sigaltstack:dpdpd siglongjmp:vpdd sigpending:dpd
		sigprocmask:ddpdpd sigqueue:dddd sigqueueinfo:dddpd sigsetjmp:dpdd sigsuspend:dpd sigtimedwait:dpdpdpd sigwait:dpdp
		sigwaitinfo:dpdpd socketpair:ddddp stpcpy:sps stpncpy:spsd strcasecmp:dss strcasestr:sss strcat:sps strchr:ssd strchrnul:ssd
		strcmp:dss strcpy:sps strcspn:dss strdup:ss strlcat:dpsd strlcpy:dpsd strlen:ds strncasecmp:dssd strncat:spsd strncmp:dssd
		strncpy:spsd strndup:ssd strnlen:dsd strpbrk:sss strrchr:ssd strsep:sps strspn:dss strstr:sss strtok:sps strtok_r:spsp swab:vppd
		sync:v syncfs:dd sysconf:dd sysinfo:dpd thrd_create:dpdp thrd_current:d thrd_detach:dd thrd_equal:ddd thrd_join:ddp
		thrd_sleep:dpdpd thrd_yield:v timer_create:ddpdp timer_delete:dd timer_getoverrun:dd timer_gettime:ddpd timer_settime:dddpdpd
		timerfd_create:ddd timerfd_gettime:ddpd timerfd_settime:dddpdpd times:dpd timespec_get:dpdd tss_create:dpd tss_delete:vd
		tss_get:pd tss_set:ddp umask:oo uname:dpd vfork:d wait4:ddpdpd waitid:dddpdd write:ddpd writev:ddpdd
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
