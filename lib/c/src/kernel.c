/*
 * Every function lib/c's headers declare that the kernel implements, but
 * those that return twice, which their headers define __INLINE: a call of
 * the kernel's import of the same name, __kernel_<name> here, and nothing
 * else. The compiler sees through such a function: where a program calls
 * it, it calls the kernel's, or does there what the kernel's does; a
 * pointer to it reaches the kernel's. KERNEL declares the import and
 * defines the function, KERNEL_VOID and KERNEL_NORETURN those returning
 * nothing; a variadic function passes its va_list on, the pointer to its
 * variable arguments that the import took when the function was the
 * import.
 *
 * A structure the kernel reads or writes comes with its size, which is
 * its version: the import has the size after each pointer to one, and the
 * size of one it returns last (the _SIZED forms, whose import parameters
 * are their own). A layout only grows at its end, so the kernel can serve
 * each size it has known; it ends a process that passes another by
 * SIGSYS. DIR is the kernel's handle, of no size here.
 */
#include <arpa/inet.h>
#include <assert.h>
#include <ctype.h>
#include <devctl.h>
#include <dirent.h>
#include <fcntl.h>
#include <fnmatch.h>
#include <glob.h>
#include <grp.h>
#include <iconv.h>
#include <inttypes.h>
#include <langinfo.h>
#include <libintl.h>
#include <libgen.h>
#include <locale.h>
#include <malloc.h>
#include <net/if.h>
#include <netdb.h>
#include <poll.h>
#include <pthread.h>
#include <pty.h>
#include <pwd.h>
#include <regex.h>
#include <sched.h>
#include <semaphore.h>
#include <setjmp.h>
#include <signal.h>
#include <spawn.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/epoll.h>
#include <sys/eventfd.h>
#include <sys/execmode.h>
#include <sys/watchdir.h>
#include <sys/inotify.h>
#include <sys/timerfd.h>
#include <sys/file.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <sys/mount.h>
#include <sys/prctl.h>
#include <sys/resource.h>
#include <sys/select.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/statfs.h>
#include <sys/statvfs.h>
#include <sys/sysinfo.h>
#include <sys/time.h>
#include <sys/times.h>
#include <sys/uio.h>
#include <sys/utsname.h>
#include <sys/wait.h>
#include <syslog.h>
#include <termios.h>
#include <threads.h>
#include <time.h>
#include <unistd.h>
#include <utime.h>
#include <wchar.h>
#include <wctype.h>


#define KERNEL(ret, name, params, args) \
    ret __kernel_##name params __KERNEL(name); \
    ret name params { \
        return __kernel_##name args; \
    }

#define KERNEL_VOID(name, params, args) \
    void __kernel_##name params __KERNEL(name); \
    void name params { \
        __kernel_##name args; \
    }

#define KERNEL_NORETURN(name, params, args) \
    __NORETURN void __kernel_##name params __KERNEL(name); \
    __NORETURN void name params { \
        __kernel_##name args; \
    }

#define KERNEL_SIZED(ret, name, params, imported, args) \
    ret __kernel_##name imported __KERNEL(name); \
    ret name params { \
        return __kernel_##name args; \
    }

#define KERNEL_VOID_SIZED(name, params, imported, args) \
    void __kernel_##name imported __KERNEL(name); \
    void name params { \
        __kernel_##name args; \
    }

#define KERNEL_NORETURN_SIZED(name, params, imported, args) \
    __NORETURN void __kernel_##name imported __KERNEL(name); \
    __NORETURN void name params { \
        __kernel_##name args; \
    }

/* arpa/inet.h */
KERNEL(in_addr_t, inet_addr, (const char* a), (a))
KERNEL(in_addr_t, inet_network, (const char* a), (a))
KERNEL_SIZED(int, inet_aton, (const char* a, struct in_addr* b), (const char* a, struct in_addr* b, size_t b_size), (a, b, sizeof(struct in_addr)))
KERNEL(char*, inet_ntoa, (struct in_addr a), (a))
KERNEL(int, inet_pton, (int a, const char* __restrict b, void* __restrict c), (a, b, c))
KERNEL(const char*, inet_ntop, (int a, const void* __restrict b, char* __restrict c, socklen_t d), (a, b, c, d))
KERNEL(struct in_addr, inet_makeaddr, (in_addr_t a, in_addr_t b), (a, b))
KERNEL(in_addr_t, inet_lnaof, (struct in_addr a), (a))
KERNEL(in_addr_t, inet_netof, (struct in_addr a), (a))

/* assert.h */
KERNEL_NORETURN(__assert_fail, (const char* a, const char* b, int c, const char* d), (a, b, c, d))

/* ctype.h */
KERNEL(int, isalnum, (int a), (a))
KERNEL(int, isalpha, (int a), (a))
KERNEL(int, isascii, (int a), (a))
KERNEL(int, isblank, (int a), (a))
KERNEL(int, iscntrl, (int a), (a))
KERNEL(int, isdigit, (int a), (a))
KERNEL(int, isgraph, (int a), (a))
KERNEL(int, islower, (int a), (a))
KERNEL(int, isprint, (int a), (a))
KERNEL(int, ispunct, (int a), (a))
KERNEL(int, isspace, (int a), (a))
KERNEL(int, isupper, (int a), (a))
KERNEL(int, isxdigit, (int a), (a))
KERNEL(int, tolower, (int a), (a))
KERNEL(int, toupper, (int a), (a))
KERNEL(int, toascii, (int a), (a))

/* dirent.h */
KERNEL(DIR*, opendir, (const char* a), (a))
KERNEL(DIR*, fdopendir, (int a), (a))
KERNEL(int, closedir, (DIR* a), (a))
KERNEL_SIZED(struct dirent*, readdir, (DIR* a), (DIR* a, size_t size), (a, sizeof(struct dirent)))
KERNEL_SIZED(int, readdir_r, (DIR* __restrict a, struct dirent* __restrict b, struct dirent** __restrict c), (DIR* __restrict a, struct dirent* __restrict b, size_t b_size, struct dirent** __restrict c, size_t c_size), (a, b, sizeof(struct dirent), c, sizeof(struct dirent)))
KERNEL_VOID(rewinddir, (DIR* a), (a))
KERNEL_VOID(seekdir, (DIR* a, long b), (a, b))
KERNEL(long, telldir, (DIR* a), (a))
KERNEL(int, dirfd, (DIR* a), (a))
KERNEL_SIZED(int, alphasort, (const struct dirent** a, const struct dirent** b), (const struct dirent** a, size_t a_size, const struct dirent** b, size_t b_size), (a, sizeof(struct dirent), b, sizeof(struct dirent)))
KERNEL_SIZED(int, versionsort, (const struct dirent** a, const struct dirent** b), (const struct dirent** a, size_t a_size, const struct dirent** b, size_t b_size), (a, sizeof(struct dirent), b, sizeof(struct dirent)))
/* fcntl.h */
KERNEL(int, creat, (const char* a, mode_t b), (a, b))
KERNEL(int, posix_fadvise, (int a, off_t b, off_t c, int d), (a, b, c, d))
KERNEL(int, posix_fallocate, (int a, off_t b, off_t c), (a, b, c))

/* fnmatch.h */
KERNEL(int, fnmatch, (const char* a, const char* b, int c), (a, b, c))

/* glob.h */
KERNEL_SIZED(int, glob, (const char* __restrict a, int b, int (*c)(const char*, int), glob_t* __restrict d), (const char* __restrict a, int b, int (*c)(const char*, int), glob_t* __restrict d, size_t d_size), (a, b, c, d, sizeof(glob_t)))
KERNEL_VOID_SIZED(globfree, (glob_t* a), (glob_t* a, size_t a_size), (a, sizeof(glob_t)))
/* grp.h */
KERNEL_SIZED(struct group*, getgrnam, (const char* a), (const char* a, size_t size), (a, sizeof(struct group)))
KERNEL_SIZED(struct group*, getgrgid, (gid_t a), (gid_t a, size_t size), (a, sizeof(struct group)))
KERNEL_SIZED(int, getgrnam_r, (const char* a, struct group* b, char* c, size_t d, struct group** e), (const char* a, struct group* b, size_t b_size, char* c, size_t d, struct group** e, size_t e_size), (a, b, sizeof(struct group), c, d, e, sizeof(struct group)))
KERNEL_SIZED(int, getgrgid_r, (gid_t a, struct group* b, char* c, size_t d, struct group** e), (gid_t a, struct group* b, size_t b_size, char* c, size_t d, struct group** e, size_t e_size), (a, b, sizeof(struct group), c, d, e, sizeof(struct group)))
KERNEL_SIZED(struct group*, getgrent, (void), (size_t size), (sizeof(struct group)))
KERNEL_VOID(setgrent, (void), ())
KERNEL_VOID(endgrent, (void), ())
KERNEL(int, getgrouplist, (const char* a, gid_t b, gid_t* c, int* d), (a, b, c, d))
KERNEL(int, initgroups, (const char* a, gid_t b), (a, b))
KERNEL(int, setgroups, (size_t a, const gid_t* b), (a, b))

/* iconv.h */
KERNEL(iconv_t, iconv_open, (const char* a, const char* b), (a, b))
KERNEL(size_t, iconv, (iconv_t a, char** __restrict b, size_t* __restrict c, char** __restrict d, size_t* __restrict e), (a, b, c, d, e))
KERNEL(int, iconv_close, (iconv_t a), (a))

/* inttypes.h */
KERNEL(intmax_t, strtoimax, (const char* __restrict a, char** __restrict b, int c), (a, b, c))
KERNEL(uintmax_t, strtoumax, (const char* __restrict a, char** __restrict b, int c), (a, b, c))
KERNEL(intmax_t, wcstoimax, (const wchar_t* __restrict a, wchar_t** __restrict b, int c), (a, b, c))
KERNEL(uintmax_t, wcstoumax, (const wchar_t* __restrict a, wchar_t** __restrict b, int c), (a, b, c))
KERNEL(intmax_t, imaxabs, (intmax_t a), (a))
KERNEL(imaxdiv_t, imaxdiv, (intmax_t a, intmax_t b), (a, b))

/* langinfo.h */
KERNEL(char*, nl_langinfo, (nl_item a), (a))

/* libgen.h */
KERNEL(char*, basename, (char* a), (a))
KERNEL(char*, dirname, (char* a), (a))

/* locale.h */
KERNEL(char*, setlocale, (int a, const char* b), (a, b))
KERNEL(locale_t, newlocale, (int a, const char* b, locale_t c), (a, b, c))
KERNEL(locale_t, duplocale, (locale_t a), (a))
KERNEL_VOID(freelocale, (locale_t a), (a))
KERNEL(locale_t, uselocale, (locale_t a), (a))
KERNEL(const char*, getlocalename_l, (int a, locale_t b), (a, b))
KERNEL(char*, gettext_l, (const char* a, locale_t b), (a, b))
KERNEL(char*, dgettext_l, (const char* a, const char* b, locale_t c), (a, b, c))
KERNEL(char*, dcgettext_l, (const char* a, const char* b, int c, locale_t d), (a, b, c, d))
KERNEL(char*, ngettext_l, (const char* a, const char* b, unsigned long c, locale_t d), (a, b, c, d))
KERNEL(char*, dngettext_l, (const char* a, const char* b, const char* c, unsigned long d, locale_t e), (a, b, c, d, e))
KERNEL(char*, dcngettext_l, (const char* a, const char* b, const char* c, unsigned long d, int e, locale_t f), (a, b, c, d, e, f))
KERNEL_SIZED(struct lconv*, localeconv, (void), (size_t size), (sizeof(struct lconv)))
/* malloc.h */
KERNEL(void*, memalign, (size_t a, size_t b), (a, b))
KERNEL(size_t, malloc_usable_size, (void* a), (a))
KERNEL(int, mallopt, (int a, int b), (a, b))
KERNEL(int, malloc_trim, (size_t a), (a))

/* net/if.h */
KERNEL(unsigned, if_nametoindex, (const char* a), (a))
KERNEL(char*, if_indextoname, (unsigned a, char* b), (a, b))
KERNEL_SIZED(struct if_nameindex*, if_nameindex, (void), (size_t size), (sizeof(struct if_nameindex)))
KERNEL_VOID_SIZED(if_freenameindex, (struct if_nameindex* a), (struct if_nameindex* a, size_t a_size), (a, sizeof(struct if_nameindex)))
/* netdb.h */
KERNEL_SIZED(int, getaddrinfo, (const char* __restrict a, const char* __restrict b, const struct addrinfo* __restrict c, struct addrinfo** __restrict d), (const char* __restrict a, const char* __restrict b, const struct addrinfo* __restrict c, size_t c_size, struct addrinfo** __restrict d, size_t d_size), (a, b, c, sizeof(struct addrinfo), d, sizeof(struct addrinfo)))
KERNEL_VOID_SIZED(freeaddrinfo, (struct addrinfo* a), (struct addrinfo* a, size_t a_size), (a, sizeof(struct addrinfo)))
KERNEL(const char*, gai_strerror, (int a), (a))
KERNEL(int, getnameinfo, (const struct sockaddr* __restrict a, socklen_t b, char* __restrict c, socklen_t d, char* __restrict e, socklen_t f, int g), (a, b, c, d, e, f, g))
KERNEL_SIZED(struct hostent*, gethostbyname, (const char* a), (const char* a, size_t size), (a, sizeof(struct hostent)))
KERNEL_SIZED(struct hostent*, gethostbyname2, (const char* a, int b), (const char* a, int b, size_t size), (a, b, sizeof(struct hostent)))
KERNEL_SIZED(struct hostent*, gethostbyaddr, (const void* a, socklen_t b, int c), (const void* a, socklen_t b, int c, size_t size), (a, b, c, sizeof(struct hostent)))
KERNEL_VOID(sethostent, (int a), (a))
KERNEL_VOID(endhostent, (void), ())
KERNEL_SIZED(struct hostent*, gethostent, (void), (size_t size), (sizeof(struct hostent)))
KERNEL_SIZED(struct servent*, getservbyname, (const char* a, const char* b), (const char* a, const char* b, size_t size), (a, b, sizeof(struct servent)))
KERNEL_SIZED(struct servent*, getservbyport, (int a, const char* b), (int a, const char* b, size_t size), (a, b, sizeof(struct servent)))
KERNEL_SIZED(struct protoent*, getprotobyname, (const char* a), (const char* a, size_t size), (a, sizeof(struct protoent)))
KERNEL_SIZED(struct protoent*, getprotobynumber, (int a), (int a, size_t size), (a, sizeof(struct protoent)))
KERNEL_SIZED(struct netent*, getnetbyname, (const char* a), (const char* a, size_t size), (a, sizeof(struct netent)))
KERNEL_SIZED(struct netent*, getnetbyaddr, (uint32_t a, int b), (uint32_t a, int b, size_t size), (a, b, sizeof(struct netent)))
KERNEL_VOID(herror, (const char* a), (a))
KERNEL(const char*, hstrerror, (int a), (a))

/* poll.h */
KERNEL_SIZED(int, poll, (struct pollfd* a, nfds_t b, int c), (struct pollfd* a, size_t a_size, nfds_t b, int c), (a, sizeof(struct pollfd), b, c))
KERNEL_SIZED(int, ppoll, (struct pollfd* a, nfds_t b, const struct timespec* c, const sigset_t* d), (struct pollfd* a, size_t a_size, nfds_t b, const struct timespec* c, size_t c_size, const sigset_t* d, size_t d_size), (a, sizeof(struct pollfd), b, c, sizeof(struct timespec), d, sizeof(sigset_t)))
/* sys/epoll.h */
KERNEL(int, epoll_create, (int a), (a))
KERNEL(int, epoll_create1, (int a), (a))
KERNEL_SIZED(int, epoll_ctl, (int a, int b, int c, struct epoll_event* d), (int a, int b, int c, struct epoll_event* d, size_t d_size), (a, b, c, d, sizeof(struct epoll_event)))
KERNEL_SIZED(int, epoll_wait, (int a, struct epoll_event* b, int c, int d), (int a, struct epoll_event* b, size_t b_size, int c, int d), (a, b, sizeof(struct epoll_event), c, d))
KERNEL_SIZED(int, epoll_pwait, (int a, struct epoll_event* b, int c, int d, const sigset_t* e), (int a, struct epoll_event* b, size_t b_size, int c, int d, const sigset_t* e, size_t e_size), (a, b, sizeof(struct epoll_event), c, d, e, sizeof(sigset_t)))
/* sys/eventfd.h */
KERNEL(int, eventfd, (unsigned int a, int b), (a, b))
KERNEL(int, eventfd_read, (int a, eventfd_t* b), (a, b))
KERNEL(int, eventfd_write, (int a, eventfd_t b), (a, b))
/* sys/timerfd.h */
KERNEL(int, timerfd_create, (int a, int b), (a, b))
KERNEL_SIZED(int, timerfd_settime, (int a, int b, const struct itimerspec* c, struct itimerspec* d), (int a, int b, const struct itimerspec* c, size_t c_size, struct itimerspec* d, size_t d_size), (a, b, c, sizeof(struct itimerspec), d, sizeof(struct itimerspec)))
KERNEL_SIZED(int, timerfd_gettime, (int a, struct itimerspec* b), (int a, struct itimerspec* b, size_t b_size), (a, b, sizeof(struct itimerspec)))
/* sys/inotify.h */
KERNEL(int, inotify_init, (void), ())
KERNEL(int, inotify_init1, (int a), (a))
KERNEL(int, inotify_add_watch, (int a, const char* b, uint32_t c), (a, b, c))
KERNEL(int, inotify_rm_watch, (int a, int b), (a, b))
/* pthread.h */
KERNEL_SIZED(int, pthread_create, (pthread_t* __restrict a, const pthread_attr_t* __restrict b, void* (*c)(void*), void* __restrict d), (pthread_t* __restrict a, const pthread_attr_t* __restrict b, size_t b_size, void* (*c)(void*), void* __restrict d), (a, b, sizeof(pthread_attr_t), c, d))
KERNEL(int, pthread_join, (pthread_t a, void** b), (a, b))
KERNEL(int, pthread_detach, (pthread_t a), (a))
KERNEL(pthread_t, pthread_self, (void), ())
KERNEL(int, pthread_equal, (pthread_t a, pthread_t b), (a, b))
KERNEL(int, pthread_setcancelstate, (int a, int* b), (a, b))
KERNEL_SIZED(int, pthread_getschedparam, (pthread_t a, int* __restrict b, struct sched_param* __restrict c), (pthread_t a, int* __restrict b, struct sched_param* __restrict c, size_t c_size), (a, b, c, sizeof(struct sched_param)))
KERNEL_SIZED(int, pthread_setschedparam, (pthread_t a, int b, const struct sched_param* c), (pthread_t a, int b, const struct sched_param* c, size_t c_size), (a, b, c, sizeof(struct sched_param)))
KERNEL(int, pthread_setschedprio, (pthread_t a, int b), (a, b))
KERNEL(int, pthread_getconcurrency, (void), ())
KERNEL(int, pthread_setconcurrency, (int a), (a))
KERNEL(int, pthread_getcpuclockid, (pthread_t a, clockid_t* b), (a, b))
KERNEL_SIZED(int, pthread_attr_init, (pthread_attr_t* a), (pthread_attr_t* a, size_t a_size), (a, sizeof(pthread_attr_t)))
KERNEL_SIZED(int, pthread_attr_destroy, (pthread_attr_t* a), (pthread_attr_t* a, size_t a_size), (a, sizeof(pthread_attr_t)))
KERNEL_SIZED(int, pthread_attr_setdetachstate, (pthread_attr_t* a, int b), (pthread_attr_t* a, size_t a_size, int b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_getdetachstate, (const pthread_attr_t* a, int* b), (const pthread_attr_t* a, size_t a_size, int* b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_setstacksize, (pthread_attr_t* a, size_t b), (pthread_attr_t* a, size_t a_size, size_t b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_getstacksize, (const pthread_attr_t* __restrict a, size_t* __restrict b), (const pthread_attr_t* __restrict a, size_t a_size, size_t* __restrict b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_setguardsize, (pthread_attr_t* a, size_t b), (pthread_attr_t* a, size_t a_size, size_t b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_getguardsize, (const pthread_attr_t* __restrict a, size_t* __restrict b), (const pthread_attr_t* __restrict a, size_t a_size, size_t* __restrict b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_setscope, (pthread_attr_t* a, int b), (pthread_attr_t* a, size_t a_size, int b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_getscope, (const pthread_attr_t* __restrict a, int* __restrict b), (const pthread_attr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_setschedpolicy, (pthread_attr_t* a, int b), (pthread_attr_t* a, size_t a_size, int b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_getschedpolicy, (const pthread_attr_t* __restrict a, int* __restrict b), (const pthread_attr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_setinheritsched, (pthread_attr_t* a, int b), (pthread_attr_t* a, size_t a_size, int b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_getinheritsched, (const pthread_attr_t* __restrict a, int* __restrict b), (const pthread_attr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_attr_t), b))
KERNEL_SIZED(int, pthread_attr_setstack, (pthread_attr_t* a, void* b, size_t c), (pthread_attr_t* a, size_t a_size, void* b, size_t c), (a, sizeof(pthread_attr_t), b, c))
KERNEL_SIZED(int, pthread_attr_getstack, (const pthread_attr_t* __restrict a, void** __restrict b, size_t* __restrict c), (const pthread_attr_t* __restrict a, size_t a_size, void** __restrict b, size_t* __restrict c), (a, sizeof(pthread_attr_t), b, c))
KERNEL_SIZED(int, pthread_attr_setschedparam, (pthread_attr_t* __restrict a, const struct sched_param* __restrict b), (pthread_attr_t* __restrict a, size_t a_size, const struct sched_param* __restrict b, size_t b_size), (a, sizeof(pthread_attr_t), b, sizeof(struct sched_param)))
KERNEL_SIZED(int, pthread_attr_getschedparam, (const pthread_attr_t* __restrict a, struct sched_param* __restrict b), (const pthread_attr_t* __restrict a, size_t a_size, struct sched_param* __restrict b, size_t b_size), (a, sizeof(pthread_attr_t), b, sizeof(struct sched_param)))
KERNEL(int, pthread_key_create, (pthread_key_t* a, void (*b)(void*)), (a, b))
KERNEL(int, pthread_key_delete, (pthread_key_t a), (a))
KERNEL(void*, pthread_getspecific, (pthread_key_t a), (a))
KERNEL(int, pthread_setspecific, (pthread_key_t a, const void* b), (a, b))
KERNEL_SIZED(int, pthread_mutexattr_init, (pthread_mutexattr_t* a), (pthread_mutexattr_t* a, size_t a_size), (a, sizeof(pthread_mutexattr_t)))
KERNEL_SIZED(int, pthread_mutexattr_destroy, (pthread_mutexattr_t* a), (pthread_mutexattr_t* a, size_t a_size), (a, sizeof(pthread_mutexattr_t)))
KERNEL_SIZED(int, pthread_mutexattr_settype, (pthread_mutexattr_t* a, int b), (pthread_mutexattr_t* a, size_t a_size, int b), (a, sizeof(pthread_mutexattr_t), b))
KERNEL_SIZED(int, pthread_mutexattr_gettype, (const pthread_mutexattr_t* __restrict a, int* __restrict b), (const pthread_mutexattr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_mutexattr_t), b))
KERNEL_SIZED(int, pthread_mutexattr_setprotocol, (pthread_mutexattr_t* a, int b), (pthread_mutexattr_t* a, size_t a_size, int b), (a, sizeof(pthread_mutexattr_t), b))
KERNEL_SIZED(int, pthread_mutexattr_getprotocol, (const pthread_mutexattr_t* __restrict a, int* __restrict b), (const pthread_mutexattr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_mutexattr_t), b))
KERNEL_SIZED(int, pthread_mutexattr_setrobust, (pthread_mutexattr_t* a, int b), (pthread_mutexattr_t* a, size_t a_size, int b), (a, sizeof(pthread_mutexattr_t), b))
KERNEL_SIZED(int, pthread_mutexattr_getrobust, (const pthread_mutexattr_t* __restrict a, int* __restrict b), (const pthread_mutexattr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_mutexattr_t), b))
KERNEL_SIZED(int, pthread_mutexattr_setpshared, (pthread_mutexattr_t* a, int b), (pthread_mutexattr_t* a, size_t a_size, int b), (a, sizeof(pthread_mutexattr_t), b))
KERNEL_SIZED(int, pthread_mutexattr_getpshared, (const pthread_mutexattr_t* __restrict a, int* __restrict b), (const pthread_mutexattr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_mutexattr_t), b))
KERNEL_SIZED(int, pthread_mutexattr_setprioceiling, (pthread_mutexattr_t* a, int b), (pthread_mutexattr_t* a, size_t a_size, int b), (a, sizeof(pthread_mutexattr_t), b))
KERNEL_SIZED(int, pthread_mutexattr_getprioceiling, (const pthread_mutexattr_t* __restrict a, int* __restrict b), (const pthread_mutexattr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_mutexattr_t), b))
KERNEL_SIZED(int, pthread_mutex_init, (pthread_mutex_t* __restrict a, const pthread_mutexattr_t* __restrict b), (pthread_mutex_t* __restrict a, size_t a_size, const pthread_mutexattr_t* __restrict b, size_t b_size), (a, sizeof(pthread_mutex_t), b, sizeof(pthread_mutexattr_t)))
KERNEL_SIZED(int, pthread_mutex_destroy, (pthread_mutex_t* a), (pthread_mutex_t* a, size_t a_size), (a, sizeof(pthread_mutex_t)))
KERNEL_SIZED(int, pthread_mutex_lock, (pthread_mutex_t* a), (pthread_mutex_t* a, size_t a_size), (a, sizeof(pthread_mutex_t)))
KERNEL_SIZED(int, pthread_mutex_trylock, (pthread_mutex_t* a), (pthread_mutex_t* a, size_t a_size), (a, sizeof(pthread_mutex_t)))
KERNEL_SIZED(int, pthread_mutex_unlock, (pthread_mutex_t* a), (pthread_mutex_t* a, size_t a_size), (a, sizeof(pthread_mutex_t)))
KERNEL_SIZED(int, pthread_mutex_consistent, (pthread_mutex_t* a), (pthread_mutex_t* a, size_t a_size), (a, sizeof(pthread_mutex_t)))
KERNEL_SIZED(int, pthread_mutex_timedlock, (pthread_mutex_t* __restrict a, const struct timespec* __restrict b), (pthread_mutex_t* __restrict a, size_t a_size, const struct timespec* __restrict b, size_t b_size), (a, sizeof(pthread_mutex_t), b, sizeof(struct timespec)))
KERNEL_SIZED(int, pthread_mutex_clocklock, (pthread_mutex_t* __restrict a, clockid_t b, const struct timespec* __restrict c), (pthread_mutex_t* __restrict a, size_t a_size, clockid_t b, const struct timespec* __restrict c, size_t c_size), (a, sizeof(pthread_mutex_t), b, c, sizeof(struct timespec)))
KERNEL_SIZED(int, pthread_mutex_getprioceiling, (const pthread_mutex_t* __restrict a, int* __restrict b), (const pthread_mutex_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_mutex_t), b))
KERNEL_SIZED(int, pthread_mutex_setprioceiling, (pthread_mutex_t* __restrict a, int b, int* __restrict c), (pthread_mutex_t* __restrict a, size_t a_size, int b, int* __restrict c), (a, sizeof(pthread_mutex_t), b, c))
KERNEL_SIZED(int, pthread_condattr_init, (pthread_condattr_t* a), (pthread_condattr_t* a, size_t a_size), (a, sizeof(pthread_condattr_t)))
KERNEL_SIZED(int, pthread_condattr_destroy, (pthread_condattr_t* a), (pthread_condattr_t* a, size_t a_size), (a, sizeof(pthread_condattr_t)))
KERNEL_SIZED(int, pthread_condattr_setclock, (pthread_condattr_t* a, clockid_t b), (pthread_condattr_t* a, size_t a_size, clockid_t b), (a, sizeof(pthread_condattr_t), b))
KERNEL_SIZED(int, pthread_condattr_getclock, (const pthread_condattr_t* __restrict a, clockid_t* __restrict b), (const pthread_condattr_t* __restrict a, size_t a_size, clockid_t* __restrict b), (a, sizeof(pthread_condattr_t), b))
KERNEL_SIZED(int, pthread_condattr_setpshared, (pthread_condattr_t* a, int b), (pthread_condattr_t* a, size_t a_size, int b), (a, sizeof(pthread_condattr_t), b))
KERNEL_SIZED(int, pthread_condattr_getpshared, (const pthread_condattr_t* __restrict a, int* __restrict b), (const pthread_condattr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_condattr_t), b))
KERNEL_SIZED(int, pthread_cond_init, (pthread_cond_t* __restrict a, const pthread_condattr_t* __restrict b), (pthread_cond_t* __restrict a, size_t a_size, const pthread_condattr_t* __restrict b, size_t b_size), (a, sizeof(pthread_cond_t), b, sizeof(pthread_condattr_t)))
KERNEL_SIZED(int, pthread_cond_destroy, (pthread_cond_t* a), (pthread_cond_t* a, size_t a_size), (a, sizeof(pthread_cond_t)))
KERNEL_SIZED(int, pthread_cond_signal, (pthread_cond_t* a), (pthread_cond_t* a, size_t a_size), (a, sizeof(pthread_cond_t)))
KERNEL_SIZED(int, pthread_cond_broadcast, (pthread_cond_t* a), (pthread_cond_t* a, size_t a_size), (a, sizeof(pthread_cond_t)))
KERNEL_SIZED(int, pthread_cond_wait, (pthread_cond_t* __restrict a, pthread_mutex_t* __restrict b), (pthread_cond_t* __restrict a, size_t a_size, pthread_mutex_t* __restrict b, size_t b_size), (a, sizeof(pthread_cond_t), b, sizeof(pthread_mutex_t)))
KERNEL_SIZED(int, pthread_cond_timedwait, (pthread_cond_t* __restrict a, pthread_mutex_t* __restrict b, const struct timespec* __restrict c), (pthread_cond_t* __restrict a, size_t a_size, pthread_mutex_t* __restrict b, size_t b_size, const struct timespec* __restrict c, size_t c_size), (a, sizeof(pthread_cond_t), b, sizeof(pthread_mutex_t), c, sizeof(struct timespec)))
KERNEL_SIZED(int, pthread_cond_clockwait, (pthread_cond_t* __restrict a, pthread_mutex_t* __restrict b, clockid_t c, const struct timespec* __restrict d), (pthread_cond_t* __restrict a, size_t a_size, pthread_mutex_t* __restrict b, size_t b_size, clockid_t c, const struct timespec* __restrict d, size_t d_size), (a, sizeof(pthread_cond_t), b, sizeof(pthread_mutex_t), c, d, sizeof(struct timespec)))
KERNEL_SIZED(int, pthread_rwlockattr_init, (pthread_rwlockattr_t* a), (pthread_rwlockattr_t* a, size_t a_size), (a, sizeof(pthread_rwlockattr_t)))
KERNEL_SIZED(int, pthread_rwlockattr_destroy, (pthread_rwlockattr_t* a), (pthread_rwlockattr_t* a, size_t a_size), (a, sizeof(pthread_rwlockattr_t)))
KERNEL_SIZED(int, pthread_rwlockattr_setpshared, (pthread_rwlockattr_t* a, int b), (pthread_rwlockattr_t* a, size_t a_size, int b), (a, sizeof(pthread_rwlockattr_t), b))
KERNEL_SIZED(int, pthread_rwlockattr_getpshared, (const pthread_rwlockattr_t* __restrict a, int* __restrict b), (const pthread_rwlockattr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_rwlockattr_t), b))
KERNEL_SIZED(int, pthread_rwlock_init, (pthread_rwlock_t* __restrict a, const pthread_rwlockattr_t* __restrict b), (pthread_rwlock_t* __restrict a, size_t a_size, const pthread_rwlockattr_t* __restrict b, size_t b_size), (a, sizeof(pthread_rwlock_t), b, sizeof(pthread_rwlockattr_t)))
KERNEL_SIZED(int, pthread_rwlock_destroy, (pthread_rwlock_t* a), (pthread_rwlock_t* a, size_t a_size), (a, sizeof(pthread_rwlock_t)))
KERNEL_SIZED(int, pthread_rwlock_rdlock, (pthread_rwlock_t* a), (pthread_rwlock_t* a, size_t a_size), (a, sizeof(pthread_rwlock_t)))
KERNEL_SIZED(int, pthread_rwlock_tryrdlock, (pthread_rwlock_t* a), (pthread_rwlock_t* a, size_t a_size), (a, sizeof(pthread_rwlock_t)))
KERNEL_SIZED(int, pthread_rwlock_wrlock, (pthread_rwlock_t* a), (pthread_rwlock_t* a, size_t a_size), (a, sizeof(pthread_rwlock_t)))
KERNEL_SIZED(int, pthread_rwlock_trywrlock, (pthread_rwlock_t* a), (pthread_rwlock_t* a, size_t a_size), (a, sizeof(pthread_rwlock_t)))
KERNEL_SIZED(int, pthread_rwlock_unlock, (pthread_rwlock_t* a), (pthread_rwlock_t* a, size_t a_size), (a, sizeof(pthread_rwlock_t)))
KERNEL_SIZED(int, pthread_rwlock_timedrdlock, (pthread_rwlock_t* __restrict a, const struct timespec* __restrict b), (pthread_rwlock_t* __restrict a, size_t a_size, const struct timespec* __restrict b, size_t b_size), (a, sizeof(pthread_rwlock_t), b, sizeof(struct timespec)))
KERNEL_SIZED(int, pthread_rwlock_timedwrlock, (pthread_rwlock_t* __restrict a, const struct timespec* __restrict b), (pthread_rwlock_t* __restrict a, size_t a_size, const struct timespec* __restrict b, size_t b_size), (a, sizeof(pthread_rwlock_t), b, sizeof(struct timespec)))
KERNEL_SIZED(int, pthread_rwlock_clockrdlock, (pthread_rwlock_t* __restrict a, clockid_t b, const struct timespec* __restrict c), (pthread_rwlock_t* __restrict a, size_t a_size, clockid_t b, const struct timespec* __restrict c, size_t c_size), (a, sizeof(pthread_rwlock_t), b, c, sizeof(struct timespec)))
KERNEL_SIZED(int, pthread_rwlock_clockwrlock, (pthread_rwlock_t* __restrict a, clockid_t b, const struct timespec* __restrict c), (pthread_rwlock_t* __restrict a, size_t a_size, clockid_t b, const struct timespec* __restrict c, size_t c_size), (a, sizeof(pthread_rwlock_t), b, c, sizeof(struct timespec)))
KERNEL_SIZED(int, pthread_barrierattr_init, (pthread_barrierattr_t* a), (pthread_barrierattr_t* a, size_t a_size), (a, sizeof(pthread_barrierattr_t)))
KERNEL_SIZED(int, pthread_barrierattr_destroy, (pthread_barrierattr_t* a), (pthread_barrierattr_t* a, size_t a_size), (a, sizeof(pthread_barrierattr_t)))
KERNEL_SIZED(int, pthread_barrierattr_setpshared, (pthread_barrierattr_t* a, int b), (pthread_barrierattr_t* a, size_t a_size, int b), (a, sizeof(pthread_barrierattr_t), b))
KERNEL_SIZED(int, pthread_barrierattr_getpshared, (const pthread_barrierattr_t* __restrict a, int* __restrict b), (const pthread_barrierattr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(pthread_barrierattr_t), b))
KERNEL_SIZED(int, pthread_barrier_init, (pthread_barrier_t* __restrict a, const pthread_barrierattr_t* __restrict b, unsigned c), (pthread_barrier_t* __restrict a, size_t a_size, const pthread_barrierattr_t* __restrict b, size_t b_size, unsigned c), (a, sizeof(pthread_barrier_t), b, sizeof(pthread_barrierattr_t), c))
KERNEL_SIZED(int, pthread_barrier_destroy, (pthread_barrier_t* a), (pthread_barrier_t* a, size_t a_size), (a, sizeof(pthread_barrier_t)))
KERNEL_SIZED(int, pthread_barrier_wait, (pthread_barrier_t* a), (pthread_barrier_t* a, size_t a_size), (a, sizeof(pthread_barrier_t)))
KERNEL(int, pthread_spin_init, (pthread_spinlock_t* a, int b), (a, b))
KERNEL(int, pthread_spin_destroy, (pthread_spinlock_t* a), (a))
KERNEL(int, pthread_spin_lock, (pthread_spinlock_t* a), (a))
KERNEL(int, pthread_spin_trylock, (pthread_spinlock_t* a), (a))
KERNEL(int, pthread_spin_unlock, (pthread_spinlock_t* a), (a))
KERNEL(int, pthread_getaffinity_np, (pthread_t a, size_t b, cpu_set_t* c), (a, b, c))
KERNEL(int, pthread_setaffinity_np, (pthread_t a, size_t b, const cpu_set_t* c), (a, b, c))
KERNEL_SIZED(int, pthread_getattr_np, (pthread_t a, pthread_attr_t* b), (pthread_t a, pthread_attr_t* b, size_t b_size), (a, b, sizeof(pthread_attr_t)))
KERNEL_SIZED(int, pthread_getattr_default_np, (pthread_attr_t* a), (pthread_attr_t* a, size_t a_size), (a, sizeof(pthread_attr_t)))
KERNEL_SIZED(int, pthread_setattr_default_np, (const pthread_attr_t* a), (const pthread_attr_t* a, size_t a_size), (a, sizeof(pthread_attr_t)))
KERNEL(int, pthread_getname_np, (pthread_t a, char* b, size_t c), (a, b, c))
KERNEL(int, pthread_setname_np, (pthread_t a, const char* b), (a, b))
KERNEL(int, pthread_tryjoin_np, (pthread_t a, void** b), (a, b))
KERNEL_SIZED(int, pthread_timedjoin_np, (pthread_t a, void** b, const struct timespec* c), (pthread_t a, void** b, const struct timespec* c, size_t c_size), (a, b, c, sizeof(struct timespec)))
KERNEL(int, __pthread_once_begin, (pthread_once_t* a), (a))
KERNEL_VOID(__pthread_once_end, (pthread_once_t* a), (a))
KERNEL(int, __pthread_cancelled, (int a), (a))
KERNEL(void*, __pthread_key_take, (pthread_key_t* a, void (**b)(void*)), (a, b))

/* pty.h */
KERNEL_SIZED(int, openpty, (int* a, int* b, char* c, const struct termios* d, const struct winsize* e), (int* a, int* b, char* c, const struct termios* d, size_t d_size, const struct winsize* e, size_t e_size), (a, b, c, d, sizeof(struct termios), e, sizeof(struct winsize)))
/* pwd.h */
KERNEL_SIZED(struct passwd*, getpwnam, (const char* a), (const char* a, size_t size), (a, sizeof(struct passwd)))
KERNEL_SIZED(struct passwd*, getpwuid, (uid_t a), (uid_t a, size_t size), (a, sizeof(struct passwd)))
KERNEL_SIZED(int, getpwnam_r, (const char* a, struct passwd* b, char* c, size_t d, struct passwd** e), (const char* a, struct passwd* b, size_t b_size, char* c, size_t d, struct passwd** e, size_t e_size), (a, b, sizeof(struct passwd), c, d, e, sizeof(struct passwd)))
KERNEL_SIZED(int, getpwuid_r, (uid_t a, struct passwd* b, char* c, size_t d, struct passwd** e), (uid_t a, struct passwd* b, size_t b_size, char* c, size_t d, struct passwd** e, size_t e_size), (a, b, sizeof(struct passwd), c, d, e, sizeof(struct passwd)))
KERNEL_SIZED(struct passwd*, getpwent, (void), (size_t size), (sizeof(struct passwd)))
KERNEL_VOID(setpwent, (void), ())
KERNEL_VOID(endpwent, (void), ())

/* regex.h */
KERNEL_SIZED(int, regcomp, (regex_t* __restrict a, const char* __restrict b, int c), (regex_t* __restrict a, size_t a_size, const char* __restrict b, int c), (a, sizeof(regex_t), b, c))
KERNEL_SIZED(int, regexec, (const regex_t* __restrict a, const char* __restrict b, size_t c, regmatch_t* __restrict d, int e), (const regex_t* __restrict a, size_t a_size, const char* __restrict b, size_t c, regmatch_t* __restrict d, size_t d_size, int e), (a, sizeof(regex_t), b, c, d, sizeof(regmatch_t), e))
KERNEL_VOID_SIZED(regfree, (regex_t* a), (regex_t* a, size_t a_size), (a, sizeof(regex_t)))
KERNEL_SIZED(size_t, regerror, (int a, const regex_t* __restrict b, char* __restrict c, size_t d), (int a, const regex_t* __restrict b, size_t b_size, char* __restrict c, size_t d), (a, b, sizeof(regex_t), c, d))
/* sched.h */
KERNEL(int, sched_yield, (void), ())
KERNEL(int, sched_get_priority_max, (int a), (a))
KERNEL(int, sched_get_priority_min, (int a), (a))
KERNEL_SIZED(int, sched_getparam, (pid_t a, struct sched_param* b), (pid_t a, struct sched_param* b, size_t b_size), (a, b, sizeof(struct sched_param)))
KERNEL_SIZED(int, sched_setparam, (pid_t a, const struct sched_param* b), (pid_t a, const struct sched_param* b, size_t b_size), (a, b, sizeof(struct sched_param)))
KERNEL(int, sched_getscheduler, (pid_t a), (a))
KERNEL_SIZED(int, sched_setscheduler, (pid_t a, int b, const struct sched_param* c), (pid_t a, int b, const struct sched_param* c, size_t c_size), (a, b, c, sizeof(struct sched_param)))
KERNEL_SIZED(int, sched_rr_get_interval, (pid_t a, struct timespec* b), (pid_t a, struct timespec* b, size_t b_size), (a, b, sizeof(struct timespec)))
KERNEL(int, __sched_cpucount, (size_t a, const cpu_set_t* b), (a, b))
KERNEL(int, sched_getaffinity, (pid_t a, size_t b, cpu_set_t* c), (a, b, c))
KERNEL(int, sched_setaffinity, (pid_t a, size_t b, const cpu_set_t* c), (a, b, c))
KERNEL(int, sched_getcpu, (void), ())
/* semaphore.h */
KERNEL_SIZED(int, sem_init, (sem_t* a, int b, unsigned c), (sem_t* a, size_t a_size, int b, unsigned c), (a, sizeof(sem_t), b, c))
KERNEL_SIZED(int, sem_destroy, (sem_t* a), (sem_t* a, size_t a_size), (a, sizeof(sem_t)))
KERNEL_SIZED(int, sem_wait, (sem_t* a), (sem_t* a, size_t a_size), (a, sizeof(sem_t)))
KERNEL_SIZED(int, sem_trywait, (sem_t* a), (sem_t* a, size_t a_size), (a, sizeof(sem_t)))
KERNEL_SIZED(int, sem_post, (sem_t* a), (sem_t* a, size_t a_size), (a, sizeof(sem_t)))
KERNEL_SIZED(int, sem_close, (sem_t* a), (sem_t* a, size_t a_size), (a, sizeof(sem_t)))
KERNEL_SIZED(int, sem_timedwait, (sem_t* __restrict a, const struct timespec* __restrict b), (sem_t* __restrict a, size_t a_size, const struct timespec* __restrict b, size_t b_size), (a, sizeof(sem_t), b, sizeof(struct timespec)))
KERNEL_SIZED(int, sem_clockwait, (sem_t* __restrict a, clockid_t b, const struct timespec* __restrict c), (sem_t* __restrict a, size_t a_size, clockid_t b, const struct timespec* __restrict c, size_t c_size), (a, sizeof(sem_t), b, c, sizeof(struct timespec)))
KERNEL_SIZED(int, sem_getvalue, (sem_t* __restrict a, int* __restrict b), (sem_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(sem_t), b))
KERNEL(int, sem_unlink, (const char* a), (a))

/* setjmp.h */
KERNEL_NORETURN_SIZED(longjmp, (jmp_buf a, int b), (jmp_buf a, size_t a_size, int b), (a, sizeof(jmp_buf), b))
KERNEL_NORETURN_SIZED(_longjmp, (jmp_buf a, int b), (jmp_buf a, size_t a_size, int b), (a, sizeof(jmp_buf), b))
KERNEL_NORETURN_SIZED(siglongjmp, (sigjmp_buf a, int b), (sigjmp_buf a, size_t a_size, int b), (a, sizeof(sigjmp_buf), b))
/* signal.h */
KERNEL(sighandler_t, signal, (int a, sighandler_t b), (a, b))
KERNEL(sighandler_t, bsd_signal, (int a, sighandler_t b), (a, b))
KERNEL_SIZED(int, sigaction, (int a, const struct sigaction* __restrict b, struct sigaction* __restrict c), (int a, const struct sigaction* __restrict b, size_t b_size, struct sigaction* __restrict c, size_t c_size, size_t info_size), (a, b, sizeof(struct sigaction), c, sizeof(struct sigaction), sizeof(siginfo_t)))
KERNEL_SIZED(int, sigprocmask, (int a, const sigset_t* __restrict b, sigset_t* __restrict c), (int a, const sigset_t* __restrict b, size_t b_size, sigset_t* __restrict c, size_t c_size), (a, b, sizeof(sigset_t), c, sizeof(sigset_t)))
KERNEL_SIZED(int, pthread_sigmask, (int a, const sigset_t* __restrict b, sigset_t* __restrict c), (int a, const sigset_t* __restrict b, size_t b_size, sigset_t* __restrict c, size_t c_size), (a, b, sizeof(sigset_t), c, sizeof(sigset_t)))
KERNEL_SIZED(int, sigsuspend, (const sigset_t* a), (const sigset_t* a, size_t a_size), (a, sizeof(sigset_t)))
KERNEL_SIZED(int, sigpending, (sigset_t* a), (sigset_t* a, size_t a_size), (a, sizeof(sigset_t)))
KERNEL_SIZED(int, sigwait, (const sigset_t* __restrict a, int* __restrict b), (const sigset_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(sigset_t), b))
KERNEL_SIZED(int, sigwaitinfo, (const sigset_t* __restrict a, siginfo_t* __restrict b), (const sigset_t* __restrict a, size_t a_size, siginfo_t* __restrict b, size_t b_size), (a, sizeof(sigset_t), b, sizeof(siginfo_t)))
KERNEL_SIZED(int, sigtimedwait, (const sigset_t* __restrict a, siginfo_t* __restrict b, const struct timespec* __restrict c), (const sigset_t* __restrict a, size_t a_size, siginfo_t* __restrict b, size_t b_size, const struct timespec* __restrict c, size_t c_size), (a, sizeof(sigset_t), b, sizeof(siginfo_t), c, sizeof(struct timespec)))
KERNEL(int, sigqueue, (pid_t a, int b, union sigval c), (a, b, c))
KERNEL(int, sig2str, (int a, char* b), (a, b))
KERNEL(int, str2sig, (const char* __restrict a, int* __restrict b), (a, b))
KERNEL(int, siginterrupt, (int a, int b), (a, b))
KERNEL(int, sighold, (int a), (a))
KERNEL(int, sigrelse, (int a), (a))
KERNEL(int, sigignore, (int a), (a))
KERNEL(int, sigpause, (int a), (a))
KERNEL(sighandler_t, sigset, (int a, sighandler_t b), (a, b))
KERNEL_SIZED(int, sigaltstack, (const stack_t* __restrict a, stack_t* __restrict b), (const stack_t* __restrict a, size_t a_size, stack_t* __restrict b, size_t b_size), (a, sizeof(stack_t), b, sizeof(stack_t)))
KERNEL_SIZED(int, sigemptyset, (sigset_t* a), (sigset_t* a, size_t a_size), (a, sizeof(sigset_t)))
KERNEL_SIZED(int, sigfillset, (sigset_t* a), (sigset_t* a, size_t a_size), (a, sizeof(sigset_t)))
KERNEL_SIZED(int, sigaddset, (sigset_t* a, int b), (sigset_t* a, size_t a_size, int b), (a, sizeof(sigset_t), b))
KERNEL_SIZED(int, sigdelset, (sigset_t* a, int b), (sigset_t* a, size_t a_size, int b), (a, sizeof(sigset_t), b))
KERNEL_SIZED(int, sigismember, (const sigset_t* a, int b), (const sigset_t* a, size_t a_size, int b), (a, sizeof(sigset_t), b))
KERNEL_SIZED(int, sigisemptyset, (const sigset_t* a), (const sigset_t* a, size_t a_size), (a, sizeof(sigset_t)))
KERNEL_SIZED(int, sigorset, (sigset_t* a, const sigset_t* b, const sigset_t* c), (sigset_t* a, size_t a_size, const sigset_t* b, size_t b_size, const sigset_t* c, size_t c_size), (a, sizeof(sigset_t), b, sizeof(sigset_t), c, sizeof(sigset_t)))
KERNEL_SIZED(int, sigandset, (sigset_t* a, const sigset_t* b, const sigset_t* c), (sigset_t* a, size_t a_size, const sigset_t* b, size_t b_size, const sigset_t* c, size_t c_size), (a, sizeof(sigset_t), b, sizeof(sigset_t), c, sizeof(sigset_t)))
KERNEL(int, kill, (pid_t a, int b), (a, b))
KERNEL(int, killpg, (pid_t a, int b), (a, b))
KERNEL(int, raise, (int a), (a))
KERNEL_VOID(psignal, (int a, const char* b), (a, b))
KERNEL_VOID_SIZED(psiginfo, (const siginfo_t* a, const char* b), (const siginfo_t* a, size_t a_size, const char* b), (a, sizeof(siginfo_t), b))
KERNEL(int, pthread_kill, (pthread_t a, int b), (a, b))
/* stdio.h */
KERNEL(int, remove, (const char* a), (a))
KERNEL(int, rename, (const char* a, const char* b), (a, b))
KERNEL(int, renameat, (int a, const char* b, int c, const char* d), (a, b, c, d))
KERNEL(char*, ctermid, (char* a), (a))
KERNEL(char*, cuserid, (char* a), (a))
KERNEL(char*, __dtoa, (double a, int b, int c, int* d, int* e, char** f), (a, b, c, d, e, f))
KERNEL(char*, __ldtoa, (long double* a, int b, int c, int* d, int* e, char** f), (a, b, c, d, e, f))
KERNEL(char*, __hdtoa, (double a, const char* b, int c, int* d, int* e, char** f), (a, b, c, d, e, f))
KERNEL(char*, __hldtoa, (long double a, const char* b, int c, int* d, int* e, char** f), (a, b, c, d, e, f))

/* stdlib.h */
KERNEL(void*, reallocarray, (void* a, size_t b, size_t c), (a, b, c))
KERNEL(void*, aligned_alloc, (size_t a, size_t b), (a, b))
KERNEL(int, posix_memalign, (void** a, size_t b, size_t c), (a, b, c))
KERNEL(void*, valloc, (size_t a), (a))
KERNEL_NORETURN(_Exit, (int a), (a))
KERNEL_NORETURN(abort, (void), ())
KERNEL(char*, getenv, (const char* a), (a))
KERNEL(char*, secure_getenv, (const char* a), (a))
KERNEL(int, setenv, (const char* a, const char* b, int c), (a, b, c))
KERNEL(int, unsetenv, (const char* a), (a))
KERNEL(int, putenv, (char* a), (a))
KERNEL(int, clearenv, (void), ())
KERNEL(int, system, (const char* a), (a))
KERNEL(char*, realpath, (const char* __restrict a, char* __restrict b), (a, b))
KERNEL(char*, canonicalize_file_name, (const char* a), (a))
KERNEL(char*, mktemp, (char* a), (a))
KERNEL(int, mkstemp, (char* a), (a))
KERNEL(int, mkostemp, (char* a, int b), (a, b))
KERNEL(int, mkstemps, (char* a, int b), (a, b))
KERNEL(int, mkostemps, (char* a, int b, int c), (a, b, c))
KERNEL_VOID(setkey, (const char* a), (a))
KERNEL(char*, mkdtemp, (char* a), (a))
KERNEL(long, a64l, (const char* a), (a))
KERNEL(char*, l64a, (long a), (a))
KERNEL(char*, gcvt, (double a, int b, char* c), (a, b, c))
KERNEL(double, strtod, (const char* __restrict a, char** __restrict b), (a, b))
KERNEL(float, strtof, (const char* __restrict a, char** __restrict b), (a, b))
KERNEL(long double, strtold, (const char* __restrict a, char** __restrict b), (a, b))
KERNEL(double, wcstod, (const wchar_t* __restrict a, wchar_t** __restrict b), (a, b))
KERNEL(float, wcstof, (const wchar_t* __restrict a, wchar_t** __restrict b), (a, b))
KERNEL(long double, wcstold, (const wchar_t* __restrict a, wchar_t** __restrict b), (a, b))
KERNEL(float, strtof_l, (const char* __restrict a, char** __restrict b, locale_t c), (a, b, c))
KERNEL(double, strtod_l, (const char* __restrict a, char** __restrict b, locale_t c), (a, b, c))
KERNEL(long double, strtold_l, (const char* __restrict a, char** __restrict b, locale_t c), (a, b, c))
KERNEL(long, strtol, (const char* __restrict a, char** __restrict b, int c), (a, b, c))
KERNEL(unsigned long, strtoul, (const char* __restrict a, char** __restrict b, int c), (a, b, c))
KERNEL(long long, strtoll, (const char* __restrict a, char** __restrict b, int c), (a, b, c))
KERNEL(unsigned long long, strtoull, (const char* __restrict a, char** __restrict b, int c), (a, b, c))
KERNEL(long, wcstol, (const wchar_t* __restrict a, wchar_t** __restrict b, int c), (a, b, c))
KERNEL(unsigned long, wcstoul, (const wchar_t* __restrict a, wchar_t** __restrict b, int c), (a, b, c))
KERNEL(long long, wcstoll, (const wchar_t* __restrict a, wchar_t** __restrict b, int c), (a, b, c))
KERNEL(unsigned long long, wcstoull, (const wchar_t* __restrict a, wchar_t** __restrict b, int c), (a, b, c))
KERNEL(int, atoi, (const char* a), (a))
KERNEL(long, atol, (const char* a), (a))
KERNEL(long long, atoll, (const char* a), (a))
KERNEL(double, atof, (const char* a), (a))
KERNEL(int, abs, (int a), (a))
KERNEL(long, labs, (long a), (a))
KERNEL(long long, llabs, (long long a), (a))
KERNEL(void*, malloc, (size_t a), (a))
KERNEL(void*, calloc, (size_t a, size_t b), (a, b))
KERNEL(void*, realloc, (void* a, size_t b), (a, b))
KERNEL_VOID(free, (void* a), (a))
KERNEL(div_t, div, (int a, int b), (a, b))
KERNEL(ldiv_t, ldiv, (long a, long b), (a, b))
KERNEL(lldiv_t, lldiv, (long long a, long long b), (a, b))
KERNEL(int, rand, (void), ())
KERNEL_VOID(srand, (unsigned a), (a))
KERNEL(int, rand_r, (unsigned* a), (a))
KERNEL(long, random, (void), ())
KERNEL_VOID(srandom, (unsigned a), (a))
KERNEL(char*, initstate, (unsigned a, char* b, size_t c), (a, b, c))
KERNEL(char*, setstate, (char* a), (a))
KERNEL(int, posix_openpt, (int a), (a))
KERNEL(int, grantpt, (int a), (a))
KERNEL(int, unlockpt, (int a), (a))
KERNEL(char*, ptsname, (int a), (a))
KERNEL(int, ptsname_r, (int a, char* b, size_t c), (a, b, c))
KERNEL(int, getloadavg, (double* a, int b), (a, b))
KERNEL(int, getsubopt, (char** a, char* const* b, char** c), (a, b, c))

/* string.h */
KERNEL(void*, memcpy, (void* __restrict a, const void* __restrict b, size_t c), (a, b, c))
KERNEL(void*, memmove, (void* a, const void* b, size_t c), (a, b, c))
KERNEL(void*, memset, (void* a, int b, size_t c), (a, b, c))
KERNEL(int, memcmp, (const void* a, const void* b, size_t c), (a, b, c))
KERNEL(void*, memchr, (const void* a, int b, size_t c), (a, b, c))
KERNEL(size_t, strlen, (const char* a), (a))
KERNEL(char*, strcpy, (char* __restrict a, const char* __restrict b), (a, b))
KERNEL(char*, strcat, (char* __restrict a, const char* __restrict b), (a, b))
KERNEL(int, strcmp, (const char* a, const char* b), (a, b))
KERNEL(size_t, strspn, (const char* a, const char* b), (a, b))
KERNEL(size_t, strcspn, (const char* a, const char* b), (a, b))
KERNEL(void*, memrchr, (const void* a, int b, size_t c), (a, b, c))
KERNEL(void*, memmem, (const void* a, size_t b, const void* c, size_t d), (a, b, c, d))
KERNEL(void*, mempcpy, (void* __restrict a, const void* __restrict b, size_t c), (a, b, c))
KERNEL(void*, memccpy, (void* __restrict a, const void* __restrict b, int c, size_t d), (a, b, c, d))
KERNEL(void*, rawmemchr, (const void* a, int b), (a, b))
KERNEL(size_t, strnlen, (const char* a, size_t b), (a, b))
KERNEL(char*, strncpy, (char* __restrict a, const char* __restrict b, size_t c), (a, b, c))
KERNEL(char*, stpcpy, (char* __restrict a, const char* __restrict b), (a, b))
KERNEL(char*, stpncpy, (char* __restrict a, const char* __restrict b, size_t c), (a, b, c))
KERNEL(char*, strncat, (char* __restrict a, const char* __restrict b, size_t c), (a, b, c))
KERNEL(size_t, strlcpy, (char* a, const char* b, size_t c), (a, b, c))
KERNEL(size_t, strlcat, (char* a, const char* b, size_t c), (a, b, c))
KERNEL(int, strncmp, (const char* a, const char* b, size_t c), (a, b, c))
KERNEL(int, strcoll, (const char* a, const char* b), (a, b))
KERNEL(size_t, strxfrm, (char* __restrict a, const char* __restrict b, size_t c), (a, b, c))
KERNEL(int, strverscmp, (const char* a, const char* b), (a, b))
KERNEL(char*, strchr, (const char* a, int b), (a, b))
KERNEL(char*, strrchr, (const char* a, int b), (a, b))
KERNEL(char*, strchrnul, (const char* a, int b), (a, b))
KERNEL(char*, strstr, (const char* a, const char* b), (a, b))
KERNEL(char*, strcasestr, (const char* a, const char* b), (a, b))
KERNEL(char*, strpbrk, (const char* a, const char* b), (a, b))
KERNEL(char*, strtok, (char* __restrict a, const char* __restrict b), (a, b))
KERNEL(char*, strtok_r, (char* __restrict a, const char* __restrict b, char** __restrict c), (a, b, c))
KERNEL(char*, strsep, (char** a, const char* b), (a, b))
KERNEL(char*, strdup, (const char* a), (a))
KERNEL(char*, strndup, (const char* a, size_t b), (a, b))
KERNEL(char*, strerror, (int a), (a))
KERNEL(char*, strerror_l, (int a, locale_t b), (a, b))
KERNEL(int, strerror_r, (int a, char* b, size_t c), (a, b, c))
KERNEL(char*, strsignal, (int a), (a))
KERNEL_VOID(explicit_bzero, (void* a, size_t b), (a, b))

/* strings.h */
KERNEL(int, bcmp, (const void* a, const void* b, size_t c), (a, b, c))
KERNEL_VOID(bcopy, (const void* a, void* b, size_t c), (a, b, c))
KERNEL_VOID(bzero, (void* a, size_t b), (a, b))
KERNEL(char*, index, (const char* a, int b), (a, b))
KERNEL(char*, rindex, (const char* a, int b), (a, b))
KERNEL(int, ffs, (int a), (a))
KERNEL(int, ffsl, (long a), (a))
KERNEL(int, ffsll, (long long a), (a))
KERNEL(int, strcasecmp, (const char* a, const char* b), (a, b))
KERNEL(int, strncasecmp, (const char* a, const char* b, size_t c), (a, b, c))
KERNEL(int, strcasecmp_l, (const char* a, const char* b, locale_t c), (a, b, c))
KERNEL(int, strncasecmp_l, (const char* a, const char* b, size_t c, locale_t d), (a, b, c, d))

/* sys/file.h */
KERNEL(int, flock, (int a, int b), (a, b))

/* sys/mman.h */
KERNEL(void*, mmap, (void* a, size_t b, int c, int d, int e, off_t f), (a, b, c, d, e, f))
KERNEL(int, munmap, (void* a, size_t b), (a, b))
KERNEL(int, mprotect, (void* a, size_t b, int c), (a, b, c))
KERNEL(int, msync, (void* a, size_t b, int c), (a, b, c))
KERNEL(int, madvise, (void* a, size_t b, int c), (a, b, c))
KERNEL(int, memfd_create, (void* a, size_t b, int c), (a, b, c))
KERNEL(int, mlock, (const void* a, size_t b), (a, b))
KERNEL(int, munlock, (const void* a, size_t b), (a, b))
KERNEL(int, mlockall, (int a), (a))
KERNEL(int, munlockall, (void), ())

/* sys/resource.h */
KERNEL_SIZED(int, getrlimit, (int a, struct rlimit* b), (int a, struct rlimit* b, size_t b_size), (a, b, sizeof(struct rlimit)))
KERNEL_SIZED(int, setrlimit, (int a, const struct rlimit* b), (int a, const struct rlimit* b, size_t b_size), (a, b, sizeof(struct rlimit)))
KERNEL_SIZED(int, prlimit, (pid_t a, int b, const struct rlimit* c, struct rlimit* d), (pid_t a, int b, const struct rlimit* c, size_t c_size, struct rlimit* d, size_t d_size), (a, b, c, sizeof(struct rlimit), d, sizeof(struct rlimit)))
KERNEL_SIZED(int, getrusage, (int a, struct rusage* b), (int a, struct rusage* b, size_t b_size), (a, b, sizeof(struct rusage)))
KERNEL(int, getpriority, (int a, id_t b), (a, b))
KERNEL(int, setpriority, (int a, id_t b, int c), (a, b, c))

/* sys/select.h */
KERNEL_SIZED(int, select, (int a, fd_set* __restrict b, fd_set* __restrict c, fd_set* __restrict d, struct timeval* __restrict e), (int a, fd_set* __restrict b, size_t b_size, fd_set* __restrict c, size_t c_size, fd_set* __restrict d, size_t d_size, struct timeval* __restrict e, size_t e_size), (a, b, sizeof(fd_set), c, sizeof(fd_set), d, sizeof(fd_set), e, sizeof(struct timeval)))
KERNEL_SIZED(int, pselect, (int a, fd_set* __restrict b, fd_set* __restrict c, fd_set* __restrict d, const struct timespec* __restrict e, const sigset_t* __restrict f), (int a, fd_set* __restrict b, size_t b_size, fd_set* __restrict c, size_t c_size, fd_set* __restrict d, size_t d_size, const struct timespec* __restrict e, size_t e_size, const sigset_t* __restrict f, size_t f_size), (a, b, sizeof(fd_set), c, sizeof(fd_set), d, sizeof(fd_set), e, sizeof(struct timespec), f, sizeof(sigset_t)))
/* sys/socket.h */
KERNEL(int, socket, (int a, int b, int c), (a, b, c))
KERNEL(int, socketpair, (int a, int b, int c, int d[2]), (a, b, c, d))
KERNEL(int, bind, (int a, const struct sockaddr* b, socklen_t c), (a, b, c))
KERNEL(int, connect, (int a, const struct sockaddr* b, socklen_t c), (a, b, c))
KERNEL(int, listen, (int a, int b), (a, b))
KERNEL(int, accept, (int a, struct sockaddr* __restrict b, socklen_t* __restrict c), (a, b, c))
KERNEL(int, accept4, (int a, struct sockaddr* __restrict b, socklen_t* __restrict c, int d), (a, b, c, d))
KERNEL(int, getsockname, (int a, struct sockaddr* __restrict b, socklen_t* __restrict c), (a, b, c))
KERNEL(int, getpeername, (int a, struct sockaddr* __restrict b, socklen_t* __restrict c), (a, b, c))
KERNEL(int, getsockopt, (int a, int b, int c, void* __restrict d, socklen_t* __restrict e), (a, b, c, d, e))
KERNEL(int, setsockopt, (int a, int b, int c, const void* d, socklen_t e), (a, b, c, d, e))
KERNEL(ssize_t, send, (int a, const void* b, size_t c, int d), (a, b, c, d))
KERNEL(ssize_t, sendto, (int a, const void* b, size_t c, int d, const struct sockaddr* e, socklen_t f), (a, b, c, d, e, f))
KERNEL_SIZED(ssize_t, sendmsg, (int a, const struct msghdr* b, int c), (int a, const struct msghdr* b, size_t b_size, size_t iov_size, int c), (a, b, sizeof(struct msghdr), sizeof(struct iovec), c))
KERNEL(ssize_t, recv, (int a, void* b, size_t c, int d), (a, b, c, d))
KERNEL(ssize_t, recvfrom, (int a, void* __restrict b, size_t c, int d, struct sockaddr* __restrict e, socklen_t* __restrict f), (a, b, c, d, e, f))
KERNEL_SIZED(ssize_t, recvmsg, (int a, struct msghdr* b, int c), (int a, struct msghdr* b, size_t b_size, size_t iov_size, int c), (a, b, sizeof(struct msghdr), sizeof(struct iovec), c))
KERNEL(int, shutdown, (int a, int b), (a, b))
KERNEL(int, sockatmark, (int a), (a))

/* sys/stat.h */
KERNEL_SIZED(int, stat, (const char* __restrict a, struct stat* __restrict b), (const char* __restrict a, struct stat* __restrict b, size_t b_size), (a, b, sizeof(struct stat)))
KERNEL_SIZED(int, fstat, (int a, struct stat* b), (int a, struct stat* b, size_t b_size), (a, b, sizeof(struct stat)))
KERNEL_SIZED(int, lstat, (const char* __restrict a, struct stat* __restrict b), (const char* __restrict a, struct stat* __restrict b, size_t b_size), (a, b, sizeof(struct stat)))
KERNEL_SIZED(int, fstatat, (int a, const char* __restrict b, struct stat* __restrict c, int d), (int a, const char* __restrict b, struct stat* __restrict c, size_t c_size, int d), (a, b, c, sizeof(struct stat), d))
KERNEL(int, chmod, (const char* a, mode_t b), (a, b))
KERNEL(int, lchmod, (const char* a, mode_t b), (a, b))
KERNEL(int, fchmod, (int a, mode_t b), (a, b))
KERNEL(int, fchmodat, (int a, const char* b, mode_t c, int d), (a, b, c, d))
KERNEL(int, mkdir, (const char* a, mode_t b), (a, b))
KERNEL(int, mkdirat, (int a, const char* b, mode_t c), (a, b, c))
KERNEL(int, mkfifo, (const char* a, mode_t b), (a, b))
KERNEL(int, mkfifoat, (int a, const char* b, mode_t c), (a, b, c))
KERNEL(int, mknod, (const char* a, mode_t b, dev_t c), (a, b, c))
KERNEL(int, mknodat, (int a, const char* b, mode_t c, dev_t d), (a, b, c, d))
KERNEL(mode_t, umask, (mode_t a), (a))
KERNEL_SIZED(int, utimensat, (int a, const char* b, const struct timespec c[2], int d), (int a, const char* b, const struct timespec c[2], size_t c_size, int d), (a, b, c, sizeof(struct timespec), d))
KERNEL_SIZED(int, futimens, (int a, const struct timespec b[2]), (int a, const struct timespec b[2], size_t b_size), (a, b, sizeof(struct timespec)))
/* sys/statfs.h */
KERNEL_SIZED(int, statfs, (const char* a, struct statfs* b), (const char* a, struct statfs* b, size_t b_size), (a, b, sizeof(struct statfs)))
KERNEL_SIZED(int, fstatfs, (int a, struct statfs* b), (int a, struct statfs* b, size_t b_size), (a, b, sizeof(struct statfs)))
/* sys/statvfs.h */
KERNEL_SIZED(int, statvfs, (const char* __restrict a, struct statvfs* __restrict b), (const char* __restrict a, struct statvfs* __restrict b, size_t b_size), (a, b, sizeof(struct statvfs)))
KERNEL_SIZED(int, fstatvfs, (int a, struct statvfs* b), (int a, struct statvfs* b, size_t b_size), (a, b, sizeof(struct statvfs)))
/* sys/sysinfo.h */
KERNEL_SIZED(int, sysinfo, (struct sysinfo* a), (struct sysinfo* a, size_t a_size), (a, sizeof(struct sysinfo)))
KERNEL(int, get_nprocs_conf, (void), ())
KERNEL(int, get_nprocs, (void), ())
KERNEL(long, get_phys_pages, (void), ())
KERNEL(long, get_avphys_pages, (void), ())

/* sys/time.h */
KERNEL_SIZED(int, gettimeofday, (struct timeval* __restrict a, void* __restrict b), (struct timeval* __restrict a, size_t a_size, void* __restrict b), (a, sizeof(struct timeval), b))
KERNEL_SIZED(int, settimeofday, (const struct timeval* a, const struct timezone* b), (const struct timeval* a, size_t a_size, const struct timezone* b, size_t b_size), (a, sizeof(struct timeval), b, sizeof(struct timezone)))
KERNEL_SIZED(int, getitimer, (int a, struct itimerval* b), (int a, struct itimerval* b, size_t b_size), (a, b, sizeof(struct itimerval)))
KERNEL_SIZED(int, setitimer, (int a, const struct itimerval* __restrict b, struct itimerval* __restrict c), (int a, const struct itimerval* __restrict b, size_t b_size, struct itimerval* __restrict c, size_t c_size), (a, b, sizeof(struct itimerval), c, sizeof(struct itimerval)))
KERNEL_SIZED(int, utimes, (const char* a, const struct timeval b[2]), (const char* a, const struct timeval b[2], size_t b_size), (a, b, sizeof(struct timeval)))
KERNEL_SIZED(int, lutimes, (const char* a, const struct timeval b[2]), (const char* a, const struct timeval b[2], size_t b_size), (a, b, sizeof(struct timeval)))
KERNEL_SIZED(int, futimes, (int a, const struct timeval b[2]), (int a, const struct timeval b[2], size_t b_size), (a, b, sizeof(struct timeval)))
KERNEL_SIZED(int, adjtime, (const struct timeval* a, struct timeval* b), (const struct timeval* a, size_t a_size, struct timeval* b, size_t b_size), (a, sizeof(struct timeval), b, sizeof(struct timeval)))
/* sys/times.h */
KERNEL_SIZED(clock_t, times, (struct tms* a), (struct tms* a, size_t a_size), (a, sizeof(struct tms)))
/* sys/uio.h */
KERNEL_SIZED(ssize_t, readv, (int a, const struct iovec* b, int c), (int a, const struct iovec* b, size_t b_size, int c), (a, b, sizeof(struct iovec), c))
KERNEL_SIZED(ssize_t, writev, (int a, const struct iovec* b, int c), (int a, const struct iovec* b, size_t b_size, int c), (a, b, sizeof(struct iovec), c))
KERNEL_SIZED(ssize_t, preadv, (int a, const struct iovec* b, int c, off_t d), (int a, const struct iovec* b, size_t b_size, int c, off_t d), (a, b, sizeof(struct iovec), c, d))
KERNEL_SIZED(ssize_t, pwritev, (int a, const struct iovec* b, int c, off_t d), (int a, const struct iovec* b, size_t b_size, int c, off_t d), (a, b, sizeof(struct iovec), c, d))
/* sys/utsname.h */
KERNEL_SIZED(int, uname, (struct utsname* a), (struct utsname* a, size_t a_size), (a, sizeof(struct utsname)))
/* sys/wait.h */
KERNEL(pid_t, wait, (int* a), (a))
KERNEL(pid_t, waitpid, (pid_t a, int* b, int c), (a, b, c))
KERNEL_SIZED(pid_t, wait3, (int* a, int b, struct rusage* c), (int* a, int b, struct rusage* c, size_t c_size), (a, b, c, sizeof(struct rusage)))
KERNEL_SIZED(pid_t, wait4, (pid_t a, int* b, int c, struct rusage* d), (pid_t a, int* b, int c, struct rusage* d, size_t d_size), (a, b, c, d, sizeof(struct rusage)))
KERNEL_SIZED(int, waitid, (idtype_t a, id_t b, siginfo_t* c, int d), (idtype_t a, id_t b, siginfo_t* c, size_t c_size, int d), (a, b, c, sizeof(siginfo_t), d))
/* syslog.h */
KERNEL_VOID(openlog, (const char* a, int b, int c), (a, b, c))
KERNEL_VOID(closelog, (void), ())
KERNEL(int, setlogmask, (int a), (a))

/* termios.h */
KERNEL_SIZED(int, tcgetattr, (int a, struct termios* b), (int a, struct termios* b, size_t b_size), (a, b, sizeof(struct termios)))
KERNEL_SIZED(int, tcsetattr, (int a, int b, const struct termios* c), (int a, int b, const struct termios* c, size_t c_size), (a, b, c, sizeof(struct termios)))
KERNEL(int, tcsendbreak, (int a, int b), (a, b))
KERNEL(int, tcdrain, (int a), (a))
KERNEL(int, tcflush, (int a, int b), (a, b))
KERNEL(int, tcflow, (int a, int b), (a, b))
KERNEL(pid_t, tcgetsid, (int a), (a))
KERNEL_SIZED(int, tcgetwinsize, (int a, struct winsize* b), (int a, struct winsize* b, size_t b_size), (a, b, sizeof(struct winsize)))
KERNEL_SIZED(int, tcsetwinsize, (int a, const struct winsize* b), (int a, const struct winsize* b, size_t b_size), (a, b, sizeof(struct winsize)))
KERNEL_SIZED(speed_t, cfgetospeed, (const struct termios* a), (const struct termios* a, size_t a_size), (a, sizeof(struct termios)))
KERNEL_SIZED(speed_t, cfgetispeed, (const struct termios* a), (const struct termios* a, size_t a_size), (a, sizeof(struct termios)))
KERNEL_SIZED(int, cfsetospeed, (struct termios* a, speed_t b), (struct termios* a, size_t a_size, speed_t b), (a, sizeof(struct termios), b))
KERNEL_SIZED(int, cfsetispeed, (struct termios* a, speed_t b), (struct termios* a, size_t a_size, speed_t b), (a, sizeof(struct termios), b))
KERNEL_SIZED(int, cfsetspeed, (struct termios* a, speed_t b), (struct termios* a, size_t a_size, speed_t b), (a, sizeof(struct termios), b))
KERNEL_VOID_SIZED(cfmakeraw, (struct termios* a), (struct termios* a, size_t a_size), (a, sizeof(struct termios)))
/* threads.h */
KERNEL(int, thrd_create, (thrd_t* a, thrd_start_t b, void* c), (a, b, c))
KERNEL(int, thrd_join, (thrd_t a, int* b), (a, b))
KERNEL(int, thrd_detach, (thrd_t a), (a))
KERNEL(thrd_t, thrd_current, (void), ())
KERNEL(int, thrd_equal, (thrd_t a, thrd_t b), (a, b))
KERNEL_SIZED(int, thrd_sleep, (const struct timespec* a, struct timespec* b), (const struct timespec* a, size_t a_size, struct timespec* b, size_t b_size), (a, sizeof(struct timespec), b, sizeof(struct timespec)))
KERNEL_VOID(thrd_yield, (void), ())
KERNEL_SIZED(int, mtx_init, (mtx_t* a, int b), (mtx_t* a, size_t a_size, int b), (a, sizeof(mtx_t), b))
KERNEL_VOID_SIZED(mtx_destroy, (mtx_t* a), (mtx_t* a, size_t a_size), (a, sizeof(mtx_t)))
KERNEL_SIZED(int, mtx_lock, (mtx_t* a), (mtx_t* a, size_t a_size), (a, sizeof(mtx_t)))
KERNEL_SIZED(int, mtx_trylock, (mtx_t* a), (mtx_t* a, size_t a_size), (a, sizeof(mtx_t)))
KERNEL_SIZED(int, mtx_unlock, (mtx_t* a), (mtx_t* a, size_t a_size), (a, sizeof(mtx_t)))
KERNEL_SIZED(int, mtx_timedlock, (mtx_t* __restrict a, const struct timespec* __restrict b), (mtx_t* __restrict a, size_t a_size, const struct timespec* __restrict b, size_t b_size), (a, sizeof(mtx_t), b, sizeof(struct timespec)))
KERNEL_SIZED(int, cnd_init, (cnd_t* a), (cnd_t* a, size_t a_size), (a, sizeof(cnd_t)))
KERNEL_VOID_SIZED(cnd_destroy, (cnd_t* a), (cnd_t* a, size_t a_size), (a, sizeof(cnd_t)))
KERNEL_SIZED(int, cnd_broadcast, (cnd_t* a), (cnd_t* a, size_t a_size), (a, sizeof(cnd_t)))
KERNEL_SIZED(int, cnd_signal, (cnd_t* a), (cnd_t* a, size_t a_size), (a, sizeof(cnd_t)))
KERNEL_SIZED(int, cnd_timedwait, (cnd_t* __restrict a, mtx_t* __restrict b, const struct timespec* __restrict c), (cnd_t* __restrict a, size_t a_size, mtx_t* __restrict b, size_t b_size, const struct timespec* __restrict c, size_t c_size), (a, sizeof(cnd_t), b, sizeof(mtx_t), c, sizeof(struct timespec)))
KERNEL_SIZED(int, cnd_wait, (cnd_t* a, mtx_t* b), (cnd_t* a, size_t a_size, mtx_t* b, size_t b_size), (a, sizeof(cnd_t), b, sizeof(mtx_t)))
KERNEL(int, tss_create, (tss_t* a, tss_dtor_t b), (a, b))
KERNEL_VOID(tss_delete, (tss_t a), (a))
KERNEL(int, tss_set, (tss_t a, void* b), (a, b))
KERNEL(void*, tss_get, (tss_t a), (a))

/* time.h */
KERNEL(time_t, time, (time_t* a), (a))
KERNEL(clock_t, clock, (void), ())
KERNEL(double, difftime, (time_t a, time_t b), (a, b))
KERNEL_SIZED(time_t, mktime, (struct tm* a), (struct tm* a, size_t a_size), (a, sizeof(struct tm)))
KERNEL_SIZED(time_t, timegm, (struct tm* a), (struct tm* a, size_t a_size), (a, sizeof(struct tm)))
KERNEL_SIZED(struct tm*, gmtime, (const time_t* a), (const time_t* a, size_t size), (a, sizeof(struct tm)))
KERNEL_SIZED(struct tm*, gmtime_r, (const time_t* __restrict a, struct tm* __restrict b), (const time_t* __restrict a, struct tm* __restrict b, size_t b_size, size_t size), (a, b, sizeof(struct tm), sizeof(struct tm)))
KERNEL_SIZED(struct tm*, localtime, (const time_t* a), (const time_t* a, size_t size), (a, sizeof(struct tm)))
KERNEL_SIZED(struct tm*, localtime_r, (const time_t* __restrict a, struct tm* __restrict b), (const time_t* __restrict a, struct tm* __restrict b, size_t b_size, size_t size), (a, b, sizeof(struct tm), sizeof(struct tm)))
KERNEL_SIZED(char*, asctime, (const struct tm* a), (const struct tm* a, size_t a_size), (a, sizeof(struct tm)))
KERNEL_SIZED(char*, asctime_r, (const struct tm* __restrict a, char* __restrict b), (const struct tm* __restrict a, size_t a_size, char* __restrict b), (a, sizeof(struct tm), b))
KERNEL(char*, ctime, (const time_t* a), (a))
KERNEL(char*, ctime_r, (const time_t* a, char* b), (a, b))
KERNEL_SIZED(size_t, strftime, (char* __restrict a, size_t b, const char* __restrict c, const struct tm* __restrict d), (char* __restrict a, size_t b, const char* __restrict c, const struct tm* __restrict d, size_t d_size), (a, b, c, d, sizeof(struct tm)))
KERNEL_SIZED(char*, strptime, (const char* __restrict a, const char* __restrict b, struct tm* __restrict c), (const char* __restrict a, const char* __restrict b, struct tm* __restrict c, size_t c_size), (a, b, c, sizeof(struct tm)))
KERNEL_VOID(tzset, (void), ())
KERNEL_SIZED(int, nanosleep, (const struct timespec* a, struct timespec* b), (const struct timespec* a, size_t a_size, struct timespec* b, size_t b_size), (a, sizeof(struct timespec), b, sizeof(struct timespec)))
KERNEL_SIZED(int, clock_gettime, (clockid_t a, struct timespec* b), (clockid_t a, struct timespec* b, size_t b_size), (a, b, sizeof(struct timespec)))
KERNEL_SIZED(int, clock_settime, (clockid_t a, const struct timespec* b), (clockid_t a, const struct timespec* b, size_t b_size), (a, b, sizeof(struct timespec)))
KERNEL_SIZED(int, clock_getres, (clockid_t a, struct timespec* b), (clockid_t a, struct timespec* b, size_t b_size), (a, b, sizeof(struct timespec)))
KERNEL_SIZED(int, timespec_get, (struct timespec* a, int b), (struct timespec* a, size_t a_size, int b), (a, sizeof(struct timespec), b))
KERNEL_SIZED(int, timer_create, (clockid_t a, struct sigevent* __restrict b, timer_t* __restrict c), (clockid_t a, struct sigevent* __restrict b, size_t b_size, timer_t* __restrict c), (a, b, sizeof(struct sigevent), c))
KERNEL(int, timer_delete, (timer_t a), (a))
KERNEL_SIZED(int, timer_settime, (timer_t a, int b, const struct itimerspec* __restrict c, struct itimerspec* __restrict d), (timer_t a, int b, const struct itimerspec* __restrict c, size_t c_size, struct itimerspec* __restrict d, size_t d_size), (a, b, c, sizeof(struct itimerspec), d, sizeof(struct itimerspec)))
KERNEL_SIZED(int, timer_gettime, (timer_t a, struct itimerspec* b), (timer_t a, struct itimerspec* b, size_t b_size), (a, b, sizeof(struct itimerspec)))
KERNEL(int, timer_getoverrun, (timer_t a), (a))
KERNEL(int, clock_getcpuclockid, (pid_t a, clockid_t* b), (a, b))
KERNEL_SIZED(int, clock_nanosleep, (clockid_t a, int b, const struct timespec* c, struct timespec* d), (clockid_t a, int b, const struct timespec* c, size_t c_size, struct timespec* d, size_t d_size), (a, b, c, sizeof(struct timespec), d, sizeof(struct timespec)))
KERNEL(int, stime, (const time_t* a), (a))

/* unistd.h */
KERNEL(ssize_t, read, (int a, void* b, size_t c), (a, b, c))
KERNEL(ssize_t, write, (int a, const void* b, size_t c), (a, b, c))
KERNEL(ssize_t, pread, (int a, void* b, size_t c, off_t d), (a, b, c, d))
KERNEL(ssize_t, pwrite, (int a, const void* b, size_t c, off_t d), (a, b, c, d))
KERNEL(ssize_t, copy_file_range, (int a, off_t* b, int c, off_t* d, size_t e, unsigned f), (a, b, c, d, e, f))
KERNEL(off_t, lseek, (int a, off_t b, int c), (a, b, c))
KERNEL(int, close, (int a), (a))
KERNEL(int, posix_close, (int a, int b), (a, b))
KERNEL(int, brk, (void* a), (a))
KERNEL(void*, sbrk, (intptr_t a), (a))
KERNEL(int, issetugid, (void), ())
KERNEL(int, dup, (int a), (a))
KERNEL(int, dup2, (int a, int b), (a, b))
KERNEL(int, dup3, (int a, int b, int c), (a, b, c))
KERNEL(int, pipe, (int a[2]), (a))
KERNEL(int, pipe2, (int a[2], int b), (a, b))
KERNEL(int, fsync, (int a), (a))
KERNEL(int, fdatasync, (int a), (a))
KERNEL_VOID(sync, (void), ())
KERNEL(int, syncfs, (int a), (a))
KERNEL(int, ftruncate, (int a, off_t b), (a, b))
KERNEL(int, truncate, (const char* a, off_t b), (a, b))
KERNEL(int, lockf, (int a, int b, off_t c), (a, b, c))
KERNEL(int, execve, (const char* a, char* const b[], char* const c[]), (a, b, c))
KERNEL(int, execv, (const char* a, char* const b[]), (a, b))
KERNEL(int, execvp, (const char* a, char* const b[]), (a, b))
KERNEL(int, execvpe, (const char* a, char* const b[], char* const c[]), (a, b, c))
KERNEL(int, fexecve, (int a, char* const b[], char* const c[]), (a, b, c))
KERNEL(ssize_t, posix_getdents, (int a, void* b, size_t c, int d), (a, b, c, d))
KERNEL(int, posix_devctl, (int a, int b, void* __restrict c, size_t d, int* __restrict e), (a, b, c, d, e))
KERNEL_SIZED(int, posix_spawn, (pid_t* __restrict a, const char* __restrict b, const posix_spawn_file_actions_t* c, const posix_spawnattr_t* __restrict d, char* const e[], char* const f[]), (pid_t* __restrict a, const char* __restrict b, const posix_spawn_file_actions_t* c, size_t c_size, const posix_spawnattr_t* __restrict d, size_t d_size, char* const e[], char* const f[]), (a, b, c, sizeof(posix_spawn_file_actions_t), d, sizeof(posix_spawnattr_t), e, f))
KERNEL_SIZED(int, posix_spawnp, (pid_t* __restrict a, const char* __restrict b, const posix_spawn_file_actions_t* c, const posix_spawnattr_t* __restrict d, char* const e[], char* const f[]), (pid_t* __restrict a, const char* __restrict b, const posix_spawn_file_actions_t* c, size_t c_size, const posix_spawnattr_t* __restrict d, size_t d_size, char* const e[], char* const f[]), (a, b, c, sizeof(posix_spawn_file_actions_t), d, sizeof(posix_spawnattr_t), e, f))
KERNEL_SIZED(int, posix_spawn_file_actions_init, (posix_spawn_file_actions_t* a), (posix_spawn_file_actions_t* a, size_t a_size), (a, sizeof(posix_spawn_file_actions_t)))
KERNEL_SIZED(int, posix_spawn_file_actions_destroy, (posix_spawn_file_actions_t* a), (posix_spawn_file_actions_t* a, size_t a_size), (a, sizeof(posix_spawn_file_actions_t)))
KERNEL_SIZED(int, posix_spawn_file_actions_addopen, (posix_spawn_file_actions_t* __restrict a, int b, const char* __restrict c, int d, mode_t e), (posix_spawn_file_actions_t* __restrict a, size_t a_size, int b, const char* __restrict c, int d, mode_t e), (a, sizeof(posix_spawn_file_actions_t), b, c, d, e))
KERNEL_SIZED(int, posix_spawn_file_actions_addclose, (posix_spawn_file_actions_t* a, int b), (posix_spawn_file_actions_t* a, size_t a_size, int b), (a, sizeof(posix_spawn_file_actions_t), b))
KERNEL_SIZED(int, posix_spawn_file_actions_adddup2, (posix_spawn_file_actions_t* a, int b, int c), (posix_spawn_file_actions_t* a, size_t a_size, int b, int c), (a, sizeof(posix_spawn_file_actions_t), b, c))
KERNEL_SIZED(int, posix_spawn_file_actions_addchdir, (posix_spawn_file_actions_t* __restrict a, const char* __restrict b), (posix_spawn_file_actions_t* __restrict a, size_t a_size, const char* __restrict b), (a, sizeof(posix_spawn_file_actions_t), b))
KERNEL_SIZED(int, posix_spawn_file_actions_addfchdir, (posix_spawn_file_actions_t* a, int b), (posix_spawn_file_actions_t* a, size_t a_size, int b), (a, sizeof(posix_spawn_file_actions_t), b))
KERNEL_SIZED(int, posix_spawn_file_actions_addchdir_np, (posix_spawn_file_actions_t* __restrict a, const char* __restrict b), (posix_spawn_file_actions_t* __restrict a, size_t a_size, const char* __restrict b), (a, sizeof(posix_spawn_file_actions_t), b))
KERNEL_SIZED(int, posix_spawn_file_actions_addfchdir_np, (posix_spawn_file_actions_t* a, int b), (posix_spawn_file_actions_t* a, size_t a_size, int b), (a, sizeof(posix_spawn_file_actions_t), b))
KERNEL_SIZED(int, posix_spawnattr_init, (posix_spawnattr_t* a), (posix_spawnattr_t* a, size_t a_size), (a, sizeof(posix_spawnattr_t)))
KERNEL_SIZED(int, posix_spawnattr_destroy, (posix_spawnattr_t* a), (posix_spawnattr_t* a, size_t a_size), (a, sizeof(posix_spawnattr_t)))
KERNEL_SIZED(int, posix_spawnattr_getflags, (const posix_spawnattr_t* __restrict a, short* __restrict b), (const posix_spawnattr_t* __restrict a, size_t a_size, short* __restrict b), (a, sizeof(posix_spawnattr_t), b))
KERNEL_SIZED(int, posix_spawnattr_setflags, (posix_spawnattr_t* a, short b), (posix_spawnattr_t* a, size_t a_size, short b), (a, sizeof(posix_spawnattr_t), b))
KERNEL_SIZED(int, posix_spawnattr_getpgroup, (const posix_spawnattr_t* __restrict a, pid_t* __restrict b), (const posix_spawnattr_t* __restrict a, size_t a_size, pid_t* __restrict b), (a, sizeof(posix_spawnattr_t), b))
KERNEL_SIZED(int, posix_spawnattr_setpgroup, (posix_spawnattr_t* a, pid_t b), (posix_spawnattr_t* a, size_t a_size, pid_t b), (a, sizeof(posix_spawnattr_t), b))
KERNEL_SIZED(int, posix_spawnattr_getsigdefault, (const posix_spawnattr_t* __restrict a, sigset_t* __restrict b), (const posix_spawnattr_t* __restrict a, size_t a_size, sigset_t* __restrict b, size_t b_size), (a, sizeof(posix_spawnattr_t), b, sizeof(sigset_t)))
KERNEL_SIZED(int, posix_spawnattr_setsigdefault, (posix_spawnattr_t* __restrict a, const sigset_t* __restrict b), (posix_spawnattr_t* __restrict a, size_t a_size, const sigset_t* __restrict b, size_t b_size), (a, sizeof(posix_spawnattr_t), b, sizeof(sigset_t)))
KERNEL_SIZED(int, posix_spawnattr_getsigmask, (const posix_spawnattr_t* __restrict a, sigset_t* __restrict b), (const posix_spawnattr_t* __restrict a, size_t a_size, sigset_t* __restrict b, size_t b_size), (a, sizeof(posix_spawnattr_t), b, sizeof(sigset_t)))
KERNEL_SIZED(int, posix_spawnattr_setsigmask, (posix_spawnattr_t* __restrict a, const sigset_t* __restrict b), (posix_spawnattr_t* __restrict a, size_t a_size, const sigset_t* __restrict b, size_t b_size), (a, sizeof(posix_spawnattr_t), b, sizeof(sigset_t)))
KERNEL_SIZED(int, posix_spawnattr_getschedpolicy, (const posix_spawnattr_t* __restrict a, int* __restrict b), (const posix_spawnattr_t* __restrict a, size_t a_size, int* __restrict b), (a, sizeof(posix_spawnattr_t), b))
KERNEL_SIZED(int, posix_spawnattr_setschedpolicy, (posix_spawnattr_t* a, int b), (posix_spawnattr_t* a, size_t a_size, int b), (a, sizeof(posix_spawnattr_t), b))
KERNEL_SIZED(int, posix_spawnattr_getschedparam, (const posix_spawnattr_t* __restrict a, struct sched_param* __restrict b), (const posix_spawnattr_t* __restrict a, size_t a_size, struct sched_param* __restrict b, size_t b_size), (a, sizeof(posix_spawnattr_t), b, sizeof(struct sched_param)))
KERNEL_SIZED(int, posix_spawnattr_setschedparam, (posix_spawnattr_t* __restrict a, const struct sched_param* __restrict b), (posix_spawnattr_t* __restrict a, size_t a_size, const struct sched_param* __restrict b, size_t b_size), (a, sizeof(posix_spawnattr_t), b, sizeof(struct sched_param)))
KERNEL_NORETURN(_exit, (int a), (a))
KERNEL(pid_t, getpid, (void), ())
KERNEL(pid_t, getppid, (void), ())
KERNEL(pid_t, getpgrp, (void), ())
KERNEL(pid_t, getpgid, (pid_t a), (a))
KERNEL(int, setpgid, (pid_t a, pid_t b), (a, b))
KERNEL(int, setpgrp, (void), ())
KERNEL(pid_t, getsid, (pid_t a), (a))
KERNEL(pid_t, setsid, (void), ())
KERNEL(pid_t, tcgetpgrp, (int a), (a))
KERNEL(int, tcsetpgrp, (int a, pid_t b), (a, b))
KERNEL(uid_t, getuid, (void), ())
KERNEL(uid_t, geteuid, (void), ())
KERNEL(gid_t, getgid, (void), ())
KERNEL(gid_t, getegid, (void), ())
KERNEL(int, setuid, (uid_t a), (a))
KERNEL(int, seteuid, (uid_t a), (a))
KERNEL(int, setgid, (gid_t a), (a))
KERNEL(int, setegid, (gid_t a), (a))
KERNEL(int, setreuid, (uid_t a, uid_t b), (a, b))
KERNEL(int, setregid, (gid_t a, gid_t b), (a, b))
KERNEL(int, getresuid, (uid_t* a, uid_t* b, uid_t* c), (a, b, c))
KERNEL(int, getresgid, (gid_t* a, gid_t* b, gid_t* c), (a, b, c))
KERNEL(int, setresuid, (uid_t a, uid_t b, uid_t c), (a, b, c))
KERNEL(int, setresgid, (gid_t a, gid_t b, gid_t c), (a, b, c))
KERNEL(int, getgroups, (int a, gid_t b[]), (a, b))
KERNEL(int, access, (const char* a, int b), (a, b))
KERNEL(int, faccessat, (int a, const char* b, int c, int d), (a, b, c, d))
KERNEL(int, euidaccess, (const char* a, int b), (a, b))
KERNEL(int, eaccess, (const char* a, int b), (a, b))
KERNEL(int, chdir, (const char* a), (a))
KERNEL(int, fchdir, (int a), (a))
KERNEL(int, chroot, (const char* a), (a))
KERNEL(int, attach, (int a), (a))
KERNEL(int, watchdir, (int a, int b), (a, b))
KERNEL(char*, getcwd, (char* a, size_t b), (a, b))
KERNEL(char*, get_current_dir_name, (void), ())
KERNEL(int, unlink, (const char* a), (a))
KERNEL(int, unlinkat, (int a, const char* b, int c), (a, b, c))
KERNEL(int, rmdir, (const char* a), (a))
KERNEL(int, mount, (const char* a, const char* b, const char* c, unsigned long d, const void* e), (a, b, c, d, e))
KERNEL(int, umount, (const char* a), (a))
KERNEL(int, umount2, (const char* a, int b), (a, b))
KERNEL(int, execmode, (unsigned a, unsigned b, int c), (a, b, c))
KERNEL(int, link, (const char* a, const char* b), (a, b))
KERNEL(int, linkat, (int a, const char* b, int c, const char* d, int e), (a, b, c, d, e))
KERNEL(int, symlink, (const char* a, const char* b), (a, b))
KERNEL(int, symlinkat, (const char* a, int b, const char* c), (a, b, c))
KERNEL(ssize_t, readlink, (const char* __restrict a, char* __restrict b, size_t c), (a, b, c))
KERNEL(ssize_t, readlinkat, (int a, const char* __restrict b, char* __restrict c, size_t d), (a, b, c, d))
KERNEL(int, chown, (const char* a, uid_t b, gid_t c), (a, b, c))
KERNEL(int, fchown, (int a, uid_t b, gid_t c), (a, b, c))
KERNEL(int, lchown, (const char* a, uid_t b, gid_t c), (a, b, c))
KERNEL(int, fchownat, (int a, const char* b, uid_t c, gid_t d, int e), (a, b, c, d, e))
KERNEL(int, isatty, (int a), (a))
KERNEL(char*, ttyname, (int a), (a))
KERNEL(int, ttyname_r, (int a, char* b, size_t c), (a, b, c))
KERNEL(char*, getlogin, (void), ())
KERNEL(int, getlogin_r, (char* a, size_t b), (a, b))
KERNEL(int, gethostname, (char* a, size_t b), (a, b))
KERNEL(int, getentropy, (void* a, size_t b), (a, b))
KERNEL(int, sethostname, (const char* a, size_t b), (a, b))
KERNEL(int, getdomainname, (char* a, size_t b), (a, b))
KERNEL(int, setdomainname, (const char* a, size_t b), (a, b))
KERNEL(long, gethostid, (void), ())
KERNEL(int, sethostid, (long a), (a))
KERNEL(char*, getusershell, (void), ())
KERNEL_VOID(setusershell, (void), ())
KERNEL_VOID(endusershell, (void), ())
KERNEL(char*, crypt, (const char* a, const char* b), (a, b))
KERNEL_VOID(encrypt, (char* a, int b), (a, b))
KERNEL(char*, getpass, (const char* a), (a))
KERNEL(int, daemon, (int a, int b), (a, b))
KERNEL(int, vhangup, (void), ())
KERNEL(int, acct, (const char* a), (a))
KERNEL(unsigned, sleep, (unsigned a), (a))
KERNEL(int, usleep, (useconds_t a), (a))
KERNEL(unsigned, alarm, (unsigned a), (a))
KERNEL(useconds_t, ualarm, (useconds_t a, useconds_t b), (a, b))
KERNEL(int, pause, (void), ())
KERNEL(int, nice, (int a), (a))
KERNEL(long, sysconf, (int a), (a))
KERNEL(long, pathconf, (const char* a, int b), (a, b))
KERNEL(long, fpathconf, (int a, int b), (a, b))
KERNEL(size_t, confstr, (int a, char* b, size_t c), (a, b, c))
KERNEL(int, getpagesize, (void), ())
KERNEL(int, getdtablesize, (void), ())
KERNEL_VOID(swab, (const void* __restrict a, void* __restrict b, ssize_t c), (a, b, c))
KERNEL(pid_t, gettid, (void), ())

/* utime.h */
KERNEL_SIZED(int, utime, (const char* a, const struct utimbuf* b), (const char* a, const struct utimbuf* b, size_t b_size), (a, b, sizeof(struct utimbuf)))
/* wchar.h */
KERNEL(size_t, wcslen, (const wchar_t* a), (a))
KERNEL(size_t, wcsnlen, (const wchar_t* a, size_t b), (a, b))
KERNEL(size_t, wcslcpy, (wchar_t* __restrict a, const wchar_t* __restrict b, size_t c), (a, b, c))
KERNEL(size_t, wcslcat, (wchar_t* __restrict a, const wchar_t* __restrict b, size_t c), (a, b, c))
KERNEL(wchar_t*, wcscpy, (wchar_t* __restrict a, const wchar_t* __restrict b), (a, b))
KERNEL(wchar_t*, wcsncpy, (wchar_t* __restrict a, const wchar_t* __restrict b, size_t c), (a, b, c))
KERNEL(wchar_t*, wcscat, (wchar_t* __restrict a, const wchar_t* __restrict b), (a, b))
KERNEL(wchar_t*, wcsncat, (wchar_t* __restrict a, const wchar_t* __restrict b, size_t c), (a, b, c))
KERNEL(wchar_t*, wcpcpy, (wchar_t* __restrict a, const wchar_t* __restrict b), (a, b))
KERNEL(wchar_t*, wcpncpy, (wchar_t* __restrict a, const wchar_t* __restrict b, size_t c), (a, b, c))
KERNEL(int, wcscasecmp, (const wchar_t* a, const wchar_t* b), (a, b))
KERNEL(int, wcsncasecmp, (const wchar_t* a, const wchar_t* b, size_t c), (a, b, c))
KERNEL(int, wcscasecmp_l, (const wchar_t* a, const wchar_t* b, locale_t c), (a, b, c))
KERNEL(int, wcsncasecmp_l, (const wchar_t* a, const wchar_t* b, size_t c, locale_t d), (a, b, c, d))
KERNEL(wchar_t*, wcswcs, (const wchar_t* a, const wchar_t* b), (a, b))
KERNEL(int, wcscmp, (const wchar_t* a, const wchar_t* b), (a, b))
KERNEL(int, wcsncmp, (const wchar_t* a, const wchar_t* b, size_t c), (a, b, c))
KERNEL(int, wcscoll, (const wchar_t* a, const wchar_t* b), (a, b))
KERNEL(wchar_t*, wcschr, (const wchar_t* a, wchar_t b), (a, b))
KERNEL(wchar_t*, wcsrchr, (const wchar_t* a, wchar_t b), (a, b))
KERNEL(wchar_t*, wcsstr, (const wchar_t* __restrict a, const wchar_t* __restrict b), (a, b))
KERNEL(wchar_t*, wcspbrk, (const wchar_t* a, const wchar_t* b), (a, b))
KERNEL(size_t, wcsspn, (const wchar_t* a, const wchar_t* b), (a, b))
KERNEL(size_t, wcscspn, (const wchar_t* a, const wchar_t* b), (a, b))
KERNEL(wchar_t*, wcstok, (wchar_t* __restrict a, const wchar_t* __restrict b, wchar_t** __restrict c), (a, b, c))
KERNEL(size_t, wcsxfrm, (wchar_t* __restrict a, const wchar_t* __restrict b, size_t c), (a, b, c))
KERNEL_SIZED(size_t, wcsftime, (wchar_t* __restrict a, size_t b, const wchar_t* __restrict c, const struct tm* __restrict d), (wchar_t* __restrict a, size_t b, const wchar_t* __restrict c, const struct tm* __restrict d, size_t d_size), (a, b, c, d, sizeof(struct tm)))
KERNEL_SIZED(size_t, wcsftime_l, (wchar_t* __restrict a, size_t b, const wchar_t* __restrict c, const struct tm* __restrict d, locale_t e), (wchar_t* __restrict a, size_t b, const wchar_t* __restrict c, const struct tm* __restrict d, size_t d_size, locale_t e), (a, b, c, d, sizeof(struct tm), e))
KERNEL(wchar_t*, wcsdup, (const wchar_t* a), (a))
KERNEL(wchar_t*, wmemchr, (const wchar_t* a, wchar_t b, size_t c), (a, b, c))
KERNEL(int, wmemcmp, (const wchar_t* a, const wchar_t* b, size_t c), (a, b, c))
KERNEL(wchar_t*, wmemcpy, (wchar_t* __restrict a, const wchar_t* __restrict b, size_t c), (a, b, c))
KERNEL(wchar_t*, wmemmove, (wchar_t* a, const wchar_t* b, size_t c), (a, b, c))
KERNEL(wchar_t*, wmemset, (wchar_t* a, wchar_t b, size_t c), (a, b, c))
KERNEL(int, wcwidth, (wchar_t a), (a))
KERNEL(int, wcswidth, (const wchar_t* a, size_t b), (a, b))

/* wctype.h */
KERNEL(int, iswalnum, (wint_t a), (a))
KERNEL(int, iswalpha, (wint_t a), (a))
KERNEL(int, iswblank, (wint_t a), (a))
KERNEL(int, iswcntrl, (wint_t a), (a))
KERNEL(int, iswdigit, (wint_t a), (a))
KERNEL(int, iswgraph, (wint_t a), (a))
KERNEL(int, iswlower, (wint_t a), (a))
KERNEL(int, iswprint, (wint_t a), (a))
KERNEL(int, iswpunct, (wint_t a), (a))
KERNEL(int, iswspace, (wint_t a), (a))
KERNEL(int, iswupper, (wint_t a), (a))
KERNEL(int, iswxdigit, (wint_t a), (a))
KERNEL(int, iswctype, (wint_t a, wctype_t b), (a, b))
KERNEL(wctype_t, wctype, (const char* a), (a))
KERNEL(wint_t, towlower, (wint_t a), (a))
KERNEL(wint_t, towupper, (wint_t a), (a))
KERNEL(wint_t, towctrans, (wint_t a, wctrans_t b), (a, b))
KERNEL(wctrans_t, wctrans, (const char* a), (a))
KERNEL(wint_t, towctrans_l, (wint_t a, wctrans_t b, locale_t c), (a, b, c))
KERNEL(wctrans_t, wctrans_l, (const char* a, locale_t b), (a, b))

int __pthread_cancelled(int);

/*
 * Of a cancellation that may end the thread at once: after the kernel's
 * call, a cancellation pending that may act now ends it, by pthread_exit,
 * which runs what the thread pushed (pthread.c).
 */
int __kernel_pthread_cancel(pthread_t) __KERNEL(pthread_cancel);
int __kernel_pthread_setcanceltype(int, int*) __KERNEL(pthread_setcanceltype);

int pthread_cancel(pthread_t thread) {
    int e = __kernel_pthread_cancel(thread);
    if (e == 0 && thread == pthread_self() && __pthread_cancelled(1)) {
        pthread_exit(PTHREAD_CANCELED);
    }
    return e;
}

int pthread_setcanceltype(int type, int* old) {
    int e = __kernel_pthread_setcanceltype(type, old);
    if (e == 0 && type == PTHREAD_CANCEL_ASYNCHRONOUS) {
        pthread_testcancel();
    }
    return e;
}

/* The variadic ones: the import takes the va_list. */
int __kernel_open(const char*, int, va_list) __KERNEL(open);
int __kernel_openat(int, const char*, int, va_list) __KERNEL(openat);
int __kernel_fcntl(int, int, va_list, size_t) __KERNEL(fcntl);
int __kernel_ioctl(int, unsigned long, va_list, size_t) __KERNEL(ioctl);
int __kernel_prctl(int, va_list) __KERNEL(prctl);
int __kernel_execl(const char*, const char*, va_list) __KERNEL(execl);
int __kernel_execlp(const char*, const char*, va_list) __KERNEL(execlp);
int __kernel_execle(const char*, const char*, va_list) __KERNEL(execle);
sem_t* __kernel_sem_open(const char*, int, va_list, size_t) __KERNEL(sem_open);

int open(const char* path, int flags, ...) {
    va_list ap;
    va_start(ap, flags);
    int r = __kernel_open(path, flags, ap);
    va_end(ap);
    return r;
}

sem_t* sem_open(const char* name, int flags, ...) {
    va_list ap;
    va_start(ap, flags);
    sem_t* r = __kernel_sem_open(name, flags, ap, sizeof(sem_t));
    va_end(ap);
    return r;
}

int openat(int dirfd, const char* path, int flags, ...) {
    va_list ap;
    va_start(ap, flags);
    int r = __kernel_openat(dirfd, path, flags, ap);
    va_end(ap);
    return r;
}

int fcntl(int fd, int cmd, ...) {
    va_list ap;
    va_start(ap, cmd);
    // The size of the structure the command passes, or 0.
    size_t size = cmd == F_GETLK || cmd == F_SETLK || cmd == F_SETLKW ? sizeof(struct flock) : 0;
    int r = __kernel_fcntl(fd, cmd, ap, size);
    va_end(ap);
    return r;
}

int ioctl(int fd, unsigned long request, ...) {
    va_list ap;
    va_start(ap, request);
    // The size of the structure the request passes, or 0.
    size_t size = 0;
    switch (request) {
    case TCGETS:
    case TCSETS:
    case TCSETSW:
    case TCSETSF:
        size = sizeof(struct termios);
        break;
    case TIOCGWINSZ:
    case TIOCSWINSZ:
        size = sizeof(struct winsize);
        break;
    }
    int r = __kernel_ioctl(fd, request, ap, size);
    va_end(ap);
    return r;
}

int prctl(int option, ...) {
    va_list ap;
    va_start(ap, option);
    int r = __kernel_prctl(option, ap);
    va_end(ap);
    return r;
}

int execl(const char* path, const char* arg, ...) {
    va_list ap;
    va_start(ap, arg);
    int r = __kernel_execl(path, arg, ap);
    va_end(ap);
    return r;
}

int execlp(const char* file, const char* arg, ...) {
    va_list ap;
    va_start(ap, arg);
    int r = __kernel_execlp(file, arg, ap);
    va_end(ap);
    return r;
}

int execle(const char* path, const char* arg, ...) {
    va_list ap;
    va_start(ap, arg);
    int r = __kernel_execle(path, arg, ap);
    va_end(ap);
    return r;
}
