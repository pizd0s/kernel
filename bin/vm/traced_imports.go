//go:build linux && amd64

package main

import (
	"reflect"
	"strings"
)

var libraryCalls = func() map[string]bool {
	names := strings.Fields(`
		abs aligned_alloc alphasort asctime asctime_r basename bcmp bcopy bzero calloc cfgetispeed cfgetospeed cfmakeraw cfsetispeed
		cfsetospeed cfsetspeed clearenv crypt ctime ctime_r difftime dirname div explicit_bzero ffs ffsl ffsll fnmatch free freeaddrinfo
		gai_strerror getenv getpagesize getsubopt globfree gmtime gmtime_r hstrerror if_freenameindex imaxabs imaxdiv index inet_addr
		inet_aton inet_lnaof inet_makeaddr inet_netof inet_network inet_ntoa inet_ntop inet_pton initstate isalnum isalpha isascii isblank
		iscntrl isdigit isgraph islower isprint ispunct isspace isupper iswalnum iswalpha iswblank iswcntrl iswctype iswdigit iswgraph
		iswlower iswprint iswpunct iswspace iswupper iswxdigit isxdigit labs ldiv llabs lldiv localeconv localtime localtime_r malloc
		malloc_trim malloc_usable_size mallopt memalign memccpy memchr memcmp memcpy memmem memmove mempcpy memrchr memset mktime
		nl_langinfo posix_memalign putenv rand rand_r random rawmemchr realloc reallocarray regcomp regerror regexec regfree rindex
		secure_getenv setenv setlocale setstate sigaddset sigandset sigdelset sigemptyset sigfillset sigisemptyset sigismember sigorset
		srand srandom stpcpy stpncpy strcasecmp strcasestr strcat strchr strchrnul strcmp strcoll strcpy strcspn strdup strerror
		strerror_r strftime strlcat strlcpy strlen strncasecmp strncat strncmp strncpy strndup strnlen strpbrk strptime strrchr strsep
		strsignal strspn strstr strtok strtok_r strverscmp strxfrm swab timegm toascii tolower toupper towctrans towlower towupper tzset
		unsetenv valloc versionsort wcscat wcschr wcscmp wcscoll wcscpy wcscspn wcsdup wcsftime wcslen wcsncmp wcsncpy wcsnlen wcspbrk
		wcsrchr wcsspn wcsstr wcstok wcswidth wcsxfrm wctrans wctype wcwidth wmemchr wmemcmp wmemcpy wmemmove wmemset
	`)

	out := make(map[string]bool, len(names))

	for _, name := range names {
		out[name] = true
	}

	return out
}()

var untracedCalls = map[string]bool{
	"setjmp": true, "_setjmp": true, "sigsetjmp": true, "longjmp": true, "_longjmp": true, "siglongjmp": true,
	"vfork": true, "fork": true, "forkpty": true, "__kernel_start": true, "__kernel_program": true, "__assert_fail": true,
}

var callFormats = func() map[string]string {
	out := map[string]string{}

	for _, word := range strings.Fields(`
		_Exit:vd __assert_fail:vssds _exit:vd _longjmp:vpdd _setjmp:dpd abort:v abs:dd accept:ddpp accept4:ddppd access:dsd acct:ds
		adjtime:dpdpd alarm:dd aligned_alloc:pdd alphasort:dpdpd asctime:spd asctime_r:spdp basename:sp bcmp:dppd bcopy:vppd bind:ddpd
		bsd_signal:ddd bzero:vpd calloc:pdd canonicalize_file_name:ss cfgetispeed:dpd cfgetospeed:dpd cfmakeraw:vpd cfsetispeed:dpdd
		cfsetospeed:dpdd cfsetspeed:dpdd chdir:ds chmod:dso chown:dsdd chroot:ds clearenv:d clock:d clock_getres:ddpd clock_gettime:ddpd
		clock_nanosleep:dddpdpd clock_settime:ddpd close:dd closedir:dp closelog:v confstr:ddpd connect:ddpd copy_file_range:ddpdpdd
		creat:dso crypt:sss ctermid:sp ctime:sp ctime_r:spp daemon:ddd difftime:fdd dirfd:dp dirname:sp div:ddd dup:dd dup2:ddd dup3:dddd
		eaccess:dsd endgrent:v endhostent:v endpwent:v endusershell:v epoll_create:dd epoll_create1:dd epoll_ctl:ddddpd
		epoll_pwait:ddpdddpd epoll_wait:ddpddd euidaccess:dsd execl:dssp execle:dssp execlp:dssp execmode:dddd execv:dsp execve:dspp
		execvp:dsp execvpe:dspp explicit_bzero:vpd faccessat:ddsdd fchdir:dd fchmod:ddo fchmodat:ddsod fchown:dddd fchownat:ddsddd
		fcntl:dddpd fdatasync:dd fdopendir:pd fexecve:ddpp ffs:dd ffsl:dd ffsll:dd flock:ddd fnmatch:dssd fork:d forkpty:dpppdpd
		fpathconf:ddd free:vp freeaddrinfo:vpd fstat:ddpd fstatat:ddspdd fstatfs:ddpd fstatvfs:ddpd fsync:dd ftruncate:ddd futimens:ddpd
		futimes:ddpd gai_strerror:sd get_avphys_pages:d get_current_dir_name:s get_nprocs:d get_nprocs_conf:d get_phys_pages:d
		getaddrinfo:dsspdpd getcwd:spd getdomainname:dpd getdtablesize:d getegid:d getentropy:dpd getenv:ss geteuid:d getgid:d getgrent:pd
		getgrgid:pdd getgrgid_r:ddpdpdpd getgrnam:psd getgrnam_r:dspdpdpd getgrouplist:dsdpp getgroups:ddp gethostbyaddr:ppddd
		gethostbyname:psd gethostbyname2:psdd gethostent:pd gethostid:d gethostname:dpd getitimer:ddpd getloadavg:dpd getlogin:s
		getlogin_r:dpd getnameinfo:dpdpdpdd getnetbyaddr:pddd getnetbyname:psd getpagesize:d getpass:ss getpeername:ddpp getpgid:dd
		getpgrp:d getpid:d getppid:d getpriority:ddd getprotobyname:psd getprotobynumber:pdd getpwent:pd getpwnam:psd getpwnam_r:dspdpdpd
		getpwuid:pdd getpwuid_r:ddpdpdpd getresgid:dppp getresuid:dppp getrlimit:ddpd getrusage:ddpd getservbyname:pssd getservbyport:pdsd
		getsid:dd getsockname:ddpp getsockopt:ddddpp getsubopt:dppp gettimeofday:dpdp getuid:d getusershell:s glob:dsdppd globfree:vpd
		gmtime:ppd gmtime_r:pppdd grantpt:dd herror:vs hstrerror:sd if_freenameindex:vpd if_indextoname:sdp if_nameindex:pd
		if_nametoindex:ds imaxabs:dd imaxdiv:ddd index:ssd inet_addr:ds inet_aton:dspd inet_lnaof:dd inet_makeaddr:ddd inet_netof:dd
		inet_network:ds inet_ntoa:sd inet_ntop:sdppd inet_pton:ddsp initgroups:dsd initstate:sdpd ioctl:dddpd isalnum:dd isalpha:dd
		isascii:dd isatty:dd isblank:dd iscntrl:dd isdigit:dd isgraph:dd islower:dd isprint:dd ispunct:dd isspace:dd isupper:dd
		iswalnum:dd iswalpha:dd iswblank:dd iswcntrl:dd iswctype:ddd iswdigit:dd iswgraph:dd iswlower:dd iswprint:dd iswpunct:dd
		iswspace:dd iswupper:dd iswxdigit:dd isxdigit:dd kill:ddd killpg:ddd labs:dd lchown:dsdd ldiv:ddd link:dss linkat:ddsdsd
		listen:ddd llabs:dd lldiv:ddd localeconv:pd localtime:ppd localtime_r:pppdd lockf:dddd longjmp:vpdd lseek:dddd lstat:dspd
		lutimes:dspd madvise:dpdd malloc:pd malloc_trim:dd malloc_usable_size:dp mallopt:ddd memalign:pdd memccpy:pppdd memchr:ppdd
		memcmp:dppd memcpy:pppd memfd_create:dpdd memmem:ppdpd memmove:pppd mempcpy:pppd memrchr:ppdd memset:ppdd mkdir:dso mkdirat:ddso
		mkdtemp:sp mkfifo:dso mkfifoat:ddso mknod:dsod mknodat:ddsod mkostemp:dpd mkstemp:dp mkstemps:dpd mktemp:sp mktime:dpd mlock:dpd
		mlockall:dd mmap:ppddddd mount:dsssdp mprotect:dpdd msync:dpdd munlock:dpd munlockall:d munmap:dpd nanosleep:dpdpd nice:dd
		nl_langinfo:sd open:dsdp openat:ddsdp opendir:ps openlog:vsdd openpty:dppppdpd pathconf:dsd pause:d pipe:dp pipe2:dpd poll:dpddd
		posix_fadvise:ddddd posix_fallocate:dddd posix_memalign:dpdd posix_openpt:dd ppoll:dpddpdpd prctl:ddp pread:ddpdd preadv:ddpddd
		prlimit:dddpdpd pselect:ddpdpdpdpdpd psiginfo:vpds psignal:vds pthread_sigmask:ddpdpd ptsname:sd ptsname_r:ddpd putenv:dp
		pwrite:ddpdd pwritev:ddpddd raise:dd rand:d rand_r:dp random:d rawmemchr:ppd read:ddpd readdir:ppd readdir_r:dppdpd readlink:dspd
		readlinkat:ddspd readv:ddpdd realloc:ppd reallocarray:ppdd realpath:ssp recv:ddpdd recvfrom:ddpddpp recvmsg:ddpddd regcomp:dpdsd
		regerror:ddpdpd regexec:dpdsdpdd regfree:vpd remove:ds rename:dss renameat:ddsds rewinddir:vp rindex:ssd rmdir:ds
		sched_get_priority_max:dd sched_get_priority_min:dd sched_getparam:ddpd sched_getscheduler:dd sched_rr_get_interval:ddpd
		sched_setparam:ddpd sched_setscheduler:dddpd sched_yield:d secure_getenv:ss seekdir:vpd select:ddpdpdpdpd send:ddpdd
		sendmsg:ddpddd sendto:ddpddpd setdomainname:dsd setegid:dd setenv:dssd seteuid:dd setgid:dd setgrent:v setgroups:ddp sethostent:vd
		sethostid:dd sethostname:dsd setitimer:ddpdpd setjmp:dpd setlocale:sds setlogmask:dd setpgid:ddd setpgrp:d setpriority:dddd
		setpwent:v setregid:ddd setresgid:dddd setresuid:dddd setreuid:ddd setrlimit:ddpd setsid:d setsockopt:ddddpd setstate:sp
		settimeofday:dpdpd setuid:dd setusershell:v shutdown:ddd sigaction:ddpdpdd sigaddset:dpdd sigaltstack:dpdpd sigandset:dpdpdpd
		sigdelset:dpdd sigemptyset:dpd sigfillset:dpd siginterrupt:ddd sigisemptyset:dpd sigismember:dpdd siglongjmp:vpdd signal:ddd
		sigorset:dpdpdpd sigpending:dpd sigprocmask:ddpdpd sigqueue:dddd sigsetjmp:dpdd sigsuspend:dpd sigtimedwait:dpdpdpd sigwait:dpdp
		sigwaitinfo:dpdpd sleep:dd sockatmark:dd socket:dddd socketpair:ddddp srand:vd srandom:vd stat:dspd statfs:dspd statvfs:dspd
		stime:dp stpcpy:sps stpncpy:spsd strcasecmp:dss strcasestr:sss strcat:sps strchr:ssd strchrnul:ssd strcmp:dss strcoll:dss
		strcpy:sps strcspn:dss strdup:ss strerror:sd strerror_r:ddpd strftime:dpdspd strlcat:dpsd strlcpy:dpsd strlen:ds strncasecmp:dssd
		strncat:spsd strncmp:dssd strncpy:spsd strndup:ssd strnlen:dsd strpbrk:sss strptime:ssspd strrchr:ssd strsep:sps strsignal:sd
		strspn:dss strstr:sss strtok:sps strtok_r:spsp strverscmp:dss strxfrm:dpsd swab:vppd symlink:dss symlinkat:dsds sync:v syncfs:dd
		sysconf:dd sysinfo:dpd system:ds tcdrain:dd tcflow:ddd tcflush:ddd tcgetattr:ddpd tcgetpgrp:dd tcgetsid:dd tcsendbreak:ddd
		tcsetattr:dddpd tcsetpgrp:ddd telldir:dp time:dp timegm:dpd times:dpd toascii:dd tolower:dd toupper:dd towctrans:ddd towlower:dd
		towupper:dd truncate:dsd ttyname:sd ttyname_r:ddpd tzset:v ualarm:ddd umask:oo umount:ds umount2:dsd uname:dpd unlink:ds
		unlinkat:ddsd unlockpt:dd unsetenv:ds usleep:dd utime:dspd utimensat:ddspdd utimes:dspd valloc:pd versionsort:dpdpd vfork:d
		vhangup:d wait:dp wait3:dpdpd wait4:ddpdpd waitid:dddpdd waitpid:ddpd wcscat:ppp wcschr:ppd wcscmp:dpp wcscoll:dpp wcscpy:ppp
		wcscspn:dpp wcsdup:pp wcsftime:dpdppd wcslen:dp wcsncmp:dppd wcsncpy:pppd wcsnlen:dpd wcspbrk:ppp wcsrchr:ppd wcsspn:dpp
		wcsstr:ppp wcstok:pppp wcswidth:dpd wcsxfrm:dppd wctrans:ds wctype:ds wcwidth:dd wmemchr:ppdd wmemcmp:dppd wmemcpy:pppd
		wmemmove:pppd wmemset:ppdd write:ddpd writev:ddpdd
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
