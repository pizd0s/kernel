/*
 * Every function lib/c's headers declare, but those that return twice,
 * which their headers define __INLINE: a call of the kernel's import of
 * the same name, __kernel_<name> here, and nothing else. The kernel has
 * each in its Go or in its libc.wasm (lib/libc), which nothing here tells
 * apart. The compiler sees through such a function: where a program calls
 * it, it calls the kernel's, or does there what the kernel's does; a
 * pointer to it reaches the kernel's. KERNEL declares the import and
 * defines the function, KERNEL_VOID and KERNEL_NORETURN those returning
 * nothing; a variadic function passes its va_list on, the pointer to its
 * variable arguments that the import took when the function was the
 * import.
 *
 * A variable of POSIX's is the program's (crt.c), whose place the kernel
 * gets at the program's start (kernel.h).
 *
 * A structure the kernel reads or writes comes with its size, which is
 * its version: the import has the size after each pointer to one, and the
 * size of one it returns last (the _SIZED forms, whose import parameters
 * are their own). A layout only grows at its end, so the kernel can serve
 * each size it has known; it ends a process that passes another by
 * SIGSYS. DIR is libc.wasm's, of no size here.
 */
#include <aio.h>
#include <arpa/inet.h>
#include <arpa/nameser.h>
#include <assert.h>
#include <ctype.h>
#include <devctl.h>
#include <dirent.h>
#include <fcntl.h>
#include <fmtmsg.h>
#include <fnmatch.h>
#include <glob.h>
#include <grp.h>
#include <iconv.h>
#include <ifaddrs.h>
#include <inttypes.h>
#include <langinfo.h>
#include <libintl.h>
#include <mqueue.h>
#include <ndbm.h>
#include <net/ethernet.h>
#include <libgen.h>
#include <locale.h>
#include <malloc.h>
#include <net/if.h>
#include <netdb.h>
#include <nl_types.h>
#include <poll.h>
#include <pthread.h>
#include <pty.h>
#include <pwd.h>
#include <regex.h>
#include <resolv.h>
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
#include <sys/ipc.h>
#include <sys/msg.h>
#include <sys/sem.h>
#include <sys/timerfd.h>
#include <sys/file.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <sys/mount.h>
#include <sys/prctl.h>
#include <sys/random.h>
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
#include <utmpx.h>
#include <wordexp.h>
#include <utime.h>
#include <wchar.h>
#include <wctype.h>
#include <complex.h>
#include <dlfcn.h>
#include <err.h>
#include <fenv.h>
#include <ftw.h>
#include <getopt.h>
#include <math.h>
#include <mntent.h>
#include <monetary.h>
#include <search.h>
#include <stdio_ext.h>
#include <uchar.h>


/*
 * The forwarders are weak: kernel.o is one object, which a program takes
 * whole for any of them, and a function of the program's own of one's name
 * is the program's, as with a static C library. libc.wasm's copy has them
 * strong, WEAK empty, so that its link time optimization puts them in its
 * calls; a function of its own of a forwarder's name is linked first and
 * is the one (lib/libc/make.py).
 */
#ifndef WEAK
#define WEAK __attribute__((__weak__))
#endif

#define KERNEL(ret, name, params, args) \
    ret __kernel_##name params __KERNEL(name); \
    WEAK ret name params { \
        return __kernel_##name args; \
    }

#define KERNEL_VOID(name, params, args) \
    void __kernel_##name params __KERNEL(name); \
    WEAK void name params { \
        __kernel_##name args; \
    }

#define KERNEL_NORETURN(name, params, args) \
    __NORETURN void __kernel_##name params __KERNEL(name); \
    WEAK __NORETURN void name params { \
        __kernel_##name args; \
    }

#define KERNEL_SIZED(ret, name, params, imported, args) \
    ret __kernel_##name imported __KERNEL(name); \
    WEAK ret name params { \
        return __kernel_##name args; \
    }

#define KERNEL_VOID_SIZED(name, params, imported, args) \
    void __kernel_##name imported __KERNEL(name); \
    WEAK void name params { \
        __kernel_##name args; \
    }

#define KERNEL_NORETURN_SIZED(name, params, imported, args) \
    __NORETURN void __kernel_##name imported __KERNEL(name); \
    WEAK __NORETURN void name params { \
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

/* complex.h */
// creal and cimag are the header's macros too: the functions are these.
#undef creal
#undef crealf
#undef creall
#undef cimag
#undef cimagf
#undef cimagl

KERNEL(double, cabs, (double _Complex a), (a))
KERNEL(float, cabsf, (float _Complex a), (a))
KERNEL(long double, cabsl, (long double _Complex a), (a))
KERNEL(double _Complex, cacos, (double _Complex a), (a))
KERNEL(float _Complex, cacosf, (float _Complex a), (a))
KERNEL(double _Complex, cacosh, (double _Complex a), (a))
KERNEL(float _Complex, cacoshf, (float _Complex a), (a))
KERNEL(long double _Complex, cacoshl, (long double _Complex a), (a))
KERNEL(long double _Complex, cacosl, (long double _Complex a), (a))
KERNEL(double, carg, (double _Complex a), (a))
KERNEL(float, cargf, (float _Complex a), (a))
KERNEL(long double, cargl, (long double _Complex a), (a))
KERNEL(double _Complex, casin, (double _Complex a), (a))
KERNEL(float _Complex, casinf, (float _Complex a), (a))
KERNEL(double _Complex, casinh, (double _Complex a), (a))
KERNEL(float _Complex, casinhf, (float _Complex a), (a))
KERNEL(long double _Complex, casinhl, (long double _Complex a), (a))
KERNEL(long double _Complex, casinl, (long double _Complex a), (a))
KERNEL(double _Complex, catan, (double _Complex a), (a))
KERNEL(float _Complex, catanf, (float _Complex a), (a))
KERNEL(double _Complex, catanh, (double _Complex a), (a))
KERNEL(float _Complex, catanhf, (float _Complex a), (a))
KERNEL(long double _Complex, catanhl, (long double _Complex a), (a))
KERNEL(long double _Complex, catanl, (long double _Complex a), (a))
KERNEL(double _Complex, ccos, (double _Complex a), (a))
KERNEL(float _Complex, ccosf, (float _Complex a), (a))
KERNEL(double _Complex, ccosh, (double _Complex a), (a))
KERNEL(float _Complex, ccoshf, (float _Complex a), (a))
KERNEL(long double _Complex, ccoshl, (long double _Complex a), (a))
KERNEL(long double _Complex, ccosl, (long double _Complex a), (a))
KERNEL(double _Complex, cexp, (double _Complex a), (a))
KERNEL(float _Complex, cexpf, (float _Complex a), (a))
KERNEL(long double _Complex, cexpl, (long double _Complex a), (a))
KERNEL(double, cimag, (double _Complex a), (a))
KERNEL(float, cimagf, (float _Complex a), (a))
KERNEL(long double, cimagl, (long double _Complex a), (a))
KERNEL(double _Complex, clog, (double _Complex a), (a))
KERNEL(float _Complex, clogf, (float _Complex a), (a))
KERNEL(long double _Complex, clogl, (long double _Complex a), (a))
KERNEL(double _Complex, conj, (double _Complex a), (a))
KERNEL(float _Complex, conjf, (float _Complex a), (a))
KERNEL(long double _Complex, conjl, (long double _Complex a), (a))
KERNEL(double _Complex, cpow, (double _Complex a, double _Complex b), (a, b))
KERNEL(float _Complex, cpowf, (float _Complex a, float _Complex b), (a, b))
KERNEL(long double _Complex, cpowl, (long double _Complex a, long double _Complex b), (a, b))
KERNEL(double _Complex, cproj, (double _Complex a), (a))
KERNEL(float _Complex, cprojf, (float _Complex a), (a))
KERNEL(long double _Complex, cprojl, (long double _Complex a), (a))
KERNEL(double, creal, (double _Complex a), (a))
KERNEL(float, crealf, (float _Complex a), (a))
KERNEL(long double, creall, (long double _Complex a), (a))
KERNEL(double _Complex, csin, (double _Complex a), (a))
KERNEL(float _Complex, csinf, (float _Complex a), (a))
KERNEL(double _Complex, csinh, (double _Complex a), (a))
KERNEL(float _Complex, csinhf, (float _Complex a), (a))
KERNEL(long double _Complex, csinhl, (long double _Complex a), (a))
KERNEL(long double _Complex, csinl, (long double _Complex a), (a))
KERNEL(double _Complex, csqrt, (double _Complex a), (a))
KERNEL(float _Complex, csqrtf, (float _Complex a), (a))
KERNEL(long double _Complex, csqrtl, (long double _Complex a), (a))
KERNEL(double _Complex, ctan, (double _Complex a), (a))
KERNEL(float _Complex, ctanf, (float _Complex a), (a))
KERNEL(double _Complex, ctanh, (double _Complex a), (a))
KERNEL(float _Complex, ctanhf, (float _Complex a), (a))
KERNEL(long double _Complex, ctanhl, (long double _Complex a), (a))
KERNEL(long double _Complex, ctanl, (long double _Complex a), (a))

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
KERNEL(int, isalnum_l, (int a, locale_t b), (a, b))
KERNEL(int, isalpha_l, (int a, locale_t b), (a, b))
KERNEL(int, isblank_l, (int a, locale_t b), (a, b))
KERNEL(int, iscntrl_l, (int a, locale_t b), (a, b))
KERNEL(int, isdigit_l, (int a, locale_t b), (a, b))
KERNEL(int, isgraph_l, (int a, locale_t b), (a, b))
KERNEL(int, islower_l, (int a, locale_t b), (a, b))
KERNEL(int, isprint_l, (int a, locale_t b), (a, b))
KERNEL(int, ispunct_l, (int a, locale_t b), (a, b))
KERNEL(int, isspace_l, (int a, locale_t b), (a, b))
KERNEL(int, isupper_l, (int a, locale_t b), (a, b))
KERNEL(int, isxdigit_l, (int a, locale_t b), (a, b))
KERNEL(int, tolower_l, (int a, locale_t b), (a, b))
KERNEL(int, toupper_l, (int a, locale_t b), (a, b))

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
KERNEL_SIZED(int, scandir, (const char* a, struct dirent*** b, int (*c)(const struct dirent*), int (*d)(const struct dirent**, const struct dirent**)), (const char* a, struct dirent*** b, size_t b_size, int (*c)(const struct dirent*), int (*d)(const struct dirent**, const struct dirent**)), (a, b, sizeof(struct dirent), c, d))

/* dlfcn.h */
KERNEL_SIZED(int, dladdr, (const void* a, Dl_info* b), (const void* a, Dl_info* b, size_t b_size), (a, b, sizeof(Dl_info)))
KERNEL(int, dlclose, (void* a), (a))
KERNEL(char*, dlerror, (void), ())
KERNEL(void*, dlopen, (const char* a, int b), (a, b))
KERNEL(void*, dlsym, (void* __restrict a, const char* __restrict b), (a, b))

/* err.h */
KERNEL_NORETURN(verr, (int a, const char* b, va_list c), (a, b, c))
KERNEL_NORETURN(verrx, (int a, const char* b, va_list c), (a, b, c))
KERNEL_VOID(vwarn, (const char* a, va_list b), (a, b))
KERNEL_VOID(vwarnx, (const char* a, va_list b), (a, b))

/* errno.h */
KERNEL(int*, __errno_location, (void), ())

/* fcntl.h */
KERNEL(int, creat, (const char* a, mode_t b), (a, b))
KERNEL(int, posix_fadvise, (int a, off_t b, off_t c, int d), (a, b, c, d))
KERNEL(int, posix_fallocate, (int a, off_t b, off_t c), (a, b, c))

/* fenv.h */
KERNEL(int, feclearexcept, (int a), (a))
KERNEL_SIZED(int, fegetenv, (fenv_t* a), (fenv_t* a, size_t a_size), (a, sizeof(fenv_t)))
KERNEL_SIZED(int, fegetexceptflag, (fexcept_t* a, int b), (fexcept_t* a, size_t a_size, int b), (a, sizeof(fexcept_t), b))
KERNEL(int, fegetround, (void), ())
KERNEL_SIZED(int, feholdexcept, (fenv_t* a), (fenv_t* a, size_t a_size), (a, sizeof(fenv_t)))
KERNEL(int, feraiseexcept, (int a), (a))
KERNEL_SIZED(int, fesetenv, (const fenv_t* a), (const fenv_t* a, size_t a_size), (a, sizeof(fenv_t)))
KERNEL_SIZED(int, fesetexceptflag, (const fexcept_t* a, int b), (const fexcept_t* a, size_t a_size, int b), (a, sizeof(fexcept_t), b))
KERNEL(int, fesetround, (int a), (a))
KERNEL(int, fetestexcept, (int a), (a))
KERNEL_SIZED(int, feupdateenv, (const fenv_t* a), (const fenv_t* a, size_t a_size), (a, sizeof(fenv_t)))

/* fnmatch.h */
KERNEL(int, fnmatch, (const char* a, const char* b, int c), (a, b, c))

/* ftw.h */
KERNEL(int, ftw, (const char* a, int (*b)(const char*, const struct stat*, int), int c), (a, b, c))
KERNEL(int, nftw, (const char* a, int (*b)(const char*, const struct stat*, int, struct FTW*), int c, int d), (a, b, c, d))

/* getopt.h, unistd.h */
KERNEL(int, getopt, (int a, char* const b[], const char* c), (a, b, c))
KERNEL_SIZED(int, getopt_long, (int a, char* const* b, const char* c, const struct option* d, int* e), (int a, char* const* b, const char* c, const struct option* d, size_t d_size, int* e), (a, b, c, d, sizeof(struct option), e))
KERNEL_SIZED(int, getopt_long_only, (int a, char* const* b, const char* c, const struct option* d, int* e), (int a, char* const* b, const char* c, const struct option* d, size_t d_size, int* e), (a, b, c, d, sizeof(struct option), e))

/* glob.h */
KERNEL_SIZED(int, glob, (const char* __restrict a, int b, int (*c)(const char*, int), glob_t* __restrict d), (const char* __restrict a, int b, int (*c)(const char*, int), glob_t* __restrict d, size_t d_size), (a, b, c, d, sizeof(glob_t)))
// The kernel's glob, of the home directory a pattern's ~ is, which libc.wasm's glob finds.
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
KERNEL(char*, nl_langinfo_l, (nl_item a, locale_t b), (a, b))

/* libgen.h */
KERNEL(char*, basename, (char* a), (a))
KERNEL(char*, dirname, (char* a), (a))

/* libintl.h */
KERNEL(char*, bind_textdomain_codeset, (const char* a, const char* b), (a, b))
KERNEL(char*, bindtextdomain, (const char* a, const char* b), (a, b))
KERNEL(char*, dcgettext, (const char* a, const char* b, int c), (a, b, c))
KERNEL(char*, dcngettext, (const char* a, const char* b, const char* c, unsigned long d, int e), (a, b, c, d, e))
KERNEL(char*, dgettext, (const char* a, const char* b), (a, b))
KERNEL(char*, dngettext, (const char* a, const char* b, const char* c, unsigned long d), (a, b, c, d))
KERNEL(char*, gettext, (const char* a), (a))
KERNEL(char*, ngettext, (const char* a, const char* b, unsigned long c), (a, b, c))
KERNEL(char*, textdomain, (const char* a), (a))

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

/* math.h */
KERNEL(double, acos, (double a), (a))
KERNEL(float, acosf, (float a), (a))
KERNEL(double, acosh, (double a), (a))
KERNEL(float, acoshf, (float a), (a))
KERNEL(long double, acoshl, (long double a), (a))
KERNEL(long double, acosl, (long double a), (a))
KERNEL(double, asin, (double a), (a))
KERNEL(float, asinf, (float a), (a))
KERNEL(double, asinh, (double a), (a))
KERNEL(float, asinhf, (float a), (a))
KERNEL(long double, asinhl, (long double a), (a))
KERNEL(long double, asinl, (long double a), (a))
KERNEL(double, atan, (double a), (a))
KERNEL(double, atan2, (double a, double b), (a, b))
KERNEL(float, atan2f, (float a, float b), (a, b))
KERNEL(long double, atan2l, (long double a, long double b), (a, b))
KERNEL(float, atanf, (float a), (a))
KERNEL(double, atanh, (double a), (a))
KERNEL(float, atanhf, (float a), (a))
KERNEL(long double, atanhl, (long double a), (a))
KERNEL(long double, atanl, (long double a), (a))
KERNEL(double, cbrt, (double a), (a))
KERNEL(float, cbrtf, (float a), (a))
KERNEL(long double, cbrtl, (long double a), (a))
KERNEL(double, ceil, (double a), (a))
KERNEL(float, ceilf, (float a), (a))
KERNEL(long double, ceill, (long double a), (a))
KERNEL(double, copysign, (double a, double b), (a, b))
KERNEL(float, copysignf, (float a, float b), (a, b))
KERNEL(long double, copysignl, (long double a, long double b), (a, b))
KERNEL(double, cos, (double a), (a))
KERNEL(float, cosf, (float a), (a))
KERNEL(double, cosh, (double a), (a))
KERNEL(float, coshf, (float a), (a))
KERNEL(long double, coshl, (long double a), (a))
KERNEL(long double, cosl, (long double a), (a))
KERNEL(double, erf, (double a), (a))
KERNEL(double, erfc, (double a), (a))
KERNEL(float, erfcf, (float a), (a))
KERNEL(long double, erfcl, (long double a), (a))
KERNEL(float, erff, (float a), (a))
KERNEL(long double, erfl, (long double a), (a))
KERNEL(double, exp, (double a), (a))
KERNEL(double, exp10, (double a), (a))
KERNEL(float, exp10f, (float a), (a))
KERNEL(long double, exp10l, (long double a), (a))
KERNEL(double, exp2, (double a), (a))
KERNEL(float, exp2f, (float a), (a))
KERNEL(long double, exp2l, (long double a), (a))
KERNEL(float, expf, (float a), (a))
KERNEL(long double, expl, (long double a), (a))
KERNEL(double, expm1, (double a), (a))
KERNEL(float, expm1f, (float a), (a))
KERNEL(long double, expm1l, (long double a), (a))
KERNEL(double, fabs, (double a), (a))
KERNEL(float, fabsf, (float a), (a))
KERNEL(long double, fabsl, (long double a), (a))
KERNEL(double, fdim, (double a, double b), (a, b))
KERNEL(float, fdimf, (float a, float b), (a, b))
KERNEL(long double, fdiml, (long double a, long double b), (a, b))
KERNEL(double, floor, (double a), (a))
KERNEL(float, floorf, (float a), (a))
KERNEL(long double, floorl, (long double a), (a))
KERNEL(double, fma, (double a, double b, double c), (a, b, c))
KERNEL(float, fmaf, (float a, float b, float c), (a, b, c))
KERNEL(long double, fmal, (long double a, long double b, long double c), (a, b, c))
KERNEL(double, fmax, (double a, double b), (a, b))
KERNEL(float, fmaxf, (float a, float b), (a, b))
KERNEL(long double, fmaxl, (long double a, long double b), (a, b))
KERNEL(double, fmin, (double a, double b), (a, b))
KERNEL(float, fminf, (float a, float b), (a, b))
KERNEL(long double, fminl, (long double a, long double b), (a, b))
KERNEL(double, fmod, (double a, double b), (a, b))
KERNEL(float, fmodf, (float a, float b), (a, b))
KERNEL(long double, fmodl, (long double a, long double b), (a, b))
KERNEL(double, frexp, (double a, int* b), (a, b))
KERNEL(float, frexpf, (float a, int* b), (a, b))
KERNEL(long double, frexpl, (long double a, int* b), (a, b))
KERNEL(double, hypot, (double a, double b), (a, b))
KERNEL(float, hypotf, (float a, float b), (a, b))
KERNEL(long double, hypotl, (long double a, long double b), (a, b))
KERNEL(int, ilogb, (double a), (a))
KERNEL(int, ilogbf, (float a), (a))
KERNEL(int, ilogbl, (long double a), (a))
KERNEL(double, j0, (double a), (a))
KERNEL(float, j0f, (float a), (a))
KERNEL(double, j1, (double a), (a))
KERNEL(float, j1f, (float a), (a))
KERNEL(double, jn, (int a, double b), (a, b))
KERNEL(float, jnf, (int a, float b), (a, b))
KERNEL(double, ldexp, (double a, int b), (a, b))
KERNEL(float, ldexpf, (float a, int b), (a, b))
KERNEL(long double, ldexpl, (long double a, int b), (a, b))
KERNEL(double, lgamma_r, (double a, int* b), (a, b))
KERNEL(float, lgammaf_r, (float a, int* b), (a, b))
KERNEL(long double, lgammal_r, (long double a, int* b), (a, b))
KERNEL(long long, llrint, (double a), (a))
KERNEL(long long, llrintf, (float a), (a))
KERNEL(long long, llrintl, (long double a), (a))
KERNEL(long long, llround, (double a), (a))
KERNEL(long long, llroundf, (float a), (a))
KERNEL(long long, llroundl, (long double a), (a))
KERNEL(double, log, (double a), (a))
KERNEL(double, log10, (double a), (a))
KERNEL(float, log10f, (float a), (a))
KERNEL(long double, log10l, (long double a), (a))
KERNEL(double, log1p, (double a), (a))
KERNEL(float, log1pf, (float a), (a))
KERNEL(long double, log1pl, (long double a), (a))
KERNEL(double, log2, (double a), (a))
KERNEL(float, log2f, (float a), (a))
KERNEL(long double, log2l, (long double a), (a))
KERNEL(double, logb, (double a), (a))
KERNEL(float, logbf, (float a), (a))
KERNEL(long double, logbl, (long double a), (a))
KERNEL(float, logf, (float a), (a))
KERNEL(long double, logl, (long double a), (a))
KERNEL(long, lrint, (double a), (a))
KERNEL(long, lrintf, (float a), (a))
KERNEL(long, lrintl, (long double a), (a))
KERNEL(long, lround, (double a), (a))
KERNEL(long, lroundf, (float a), (a))
KERNEL(long, lroundl, (long double a), (a))
KERNEL(double, modf, (double a, double* b), (a, b))
KERNEL(float, modff, (float a, float* b), (a, b))
KERNEL(long double, modfl, (long double a, long double* b), (a, b))
KERNEL(double, nan, (const char* a), (a))
KERNEL(float, nanf, (const char* a), (a))
KERNEL(long double, nanl, (const char* a), (a))
KERNEL(double, nearbyint, (double a), (a))
KERNEL(float, nearbyintf, (float a), (a))
KERNEL(long double, nearbyintl, (long double a), (a))
KERNEL(double, nextafter, (double a, double b), (a, b))
KERNEL(float, nextafterf, (float a, float b), (a, b))
KERNEL(long double, nextafterl, (long double a, long double b), (a, b))
KERNEL(double, nexttoward, (double a, long double b), (a, b))
KERNEL(float, nexttowardf, (float a, long double b), (a, b))
KERNEL(long double, nexttowardl, (long double a, long double b), (a, b))
KERNEL(double, pow, (double a, double b), (a, b))
KERNEL(float, powf, (float a, float b), (a, b))
KERNEL(long double, powl, (long double a, long double b), (a, b))
KERNEL(double, remainder, (double a, double b), (a, b))
KERNEL(float, remainderf, (float a, float b), (a, b))
KERNEL(long double, remainderl, (long double a, long double b), (a, b))
KERNEL(double, remquo, (double a, double b, int* c), (a, b, c))
KERNEL(float, remquof, (float a, float b, int* c), (a, b, c))
KERNEL(long double, remquol, (long double a, long double b, int* c), (a, b, c))
KERNEL(double, rint, (double a), (a))
KERNEL(float, rintf, (float a), (a))
KERNEL(long double, rintl, (long double a), (a))
KERNEL(double, round, (double a), (a))
KERNEL(float, roundf, (float a), (a))
KERNEL(long double, roundl, (long double a), (a))
KERNEL(double, scalbln, (double a, long b), (a, b))
KERNEL(float, scalblnf, (float a, long b), (a, b))
KERNEL(long double, scalblnl, (long double a, long b), (a, b))
KERNEL(double, scalbn, (double a, int b), (a, b))
KERNEL(float, scalbnf, (float a, int b), (a, b))
KERNEL(long double, scalbnl, (long double a, int b), (a, b))
KERNEL(double, sin, (double a), (a))
KERNEL_VOID(sincos, (double a, double* b, double* c), (a, b, c))
KERNEL_VOID(sincosf, (float a, float* b, float* c), (a, b, c))
KERNEL_VOID(sincosl, (long double a, long double* b, long double* c), (a, b, c))
KERNEL(float, sinf, (float a), (a))
KERNEL(double, sinh, (double a), (a))
KERNEL(float, sinhf, (float a), (a))
KERNEL(long double, sinhl, (long double a), (a))
KERNEL(long double, sinl, (long double a), (a))
KERNEL(double, sqrt, (double a), (a))
KERNEL(float, sqrtf, (float a), (a))
KERNEL(long double, sqrtl, (long double a), (a))
KERNEL(double, tan, (double a), (a))
KERNEL(float, tanf, (float a), (a))
KERNEL(double, tanh, (double a), (a))
KERNEL(float, tanhf, (float a), (a))
KERNEL(long double, tanhl, (long double a), (a))
KERNEL(long double, tanl, (long double a), (a))
KERNEL(double, tgamma, (double a), (a))
KERNEL(float, tgammaf, (float a), (a))
KERNEL(long double, tgammal, (long double a), (a))
KERNEL(double, trunc, (double a), (a))
KERNEL(float, truncf, (float a), (a))
KERNEL(long double, truncl, (long double a), (a))
KERNEL(double, y0, (double a), (a))
KERNEL(float, y0f, (float a), (a))
KERNEL(double, y1, (double a), (a))
KERNEL(float, y1f, (float a), (a))
KERNEL(double, yn, (int a, double b), (a, b))
KERNEL(float, ynf, (int a, float b), (a, b))
KERNEL(double, lgamma, (double a), (a))
KERNEL(float, lgammaf, (float a), (a))
KERNEL(long double, lgammal, (long double a), (a))

/* mntent.h */
KERNEL_SIZED(int, addmntent, (FILE* a, const struct mntent* b), (FILE* a, const struct mntent* b, size_t b_size), (a, b, sizeof(struct mntent)))
KERNEL(int, endmntent, (FILE* a), (a))
KERNEL_SIZED(struct mntent*, getmntent, (FILE* a), (FILE* a, size_t size), (a, sizeof(struct mntent)))
KERNEL_SIZED(struct mntent*, getmntent_r, (FILE* a, struct mntent* b, char* c, int d), (FILE* a, struct mntent* b, size_t b_size, char* c, int d, size_t size), (a, b, sizeof(struct mntent), c, d, sizeof(struct mntent)))
KERNEL_SIZED(char*, hasmntopt, (const struct mntent* a, const char* b), (const struct mntent* a, size_t a_size, const char* b), (a, sizeof(struct mntent), b))
KERNEL(FILE*, setmntent, (const char* a, const char* b), (a, b))

/* net/if.h */
KERNEL(unsigned, if_nametoindex, (const char* a), (a))
KERNEL(char*, if_indextoname, (unsigned a, char* b), (a, b))
KERNEL_SIZED(struct if_nameindex*, if_nameindex, (void), (size_t size), (sizeof(struct if_nameindex)))
KERNEL_VOID_SIZED(if_freenameindex, (struct if_nameindex* a), (struct if_nameindex* a, size_t a_size), (a, sizeof(struct if_nameindex)))
KERNEL_SIZED(int, getifaddrs, (struct ifaddrs** a), (struct ifaddrs** a, size_t size), (a, sizeof(struct ifaddrs)))
KERNEL_VOID(freeifaddrs, (struct ifaddrs* a), (a))

/* resolv.h, arpa/nameser.h */
KERNEL_SIZED(struct __res_state*, __res_state, (void), (size_t size), (sizeof(struct __res_state)))
KERNEL(int, res_init, (void), ())
KERNEL(int, res_query, (const char* a, int b, int c, unsigned char* d, int e), (a, b, c, d, e))
KERNEL(int, res_querydomain, (const char* a, const char* b, int c, int d, unsigned char* e, int f), (a, b, c, d, e, f))
KERNEL(int, res_search, (const char* a, int b, int c, unsigned char* d, int e), (a, b, c, d, e))
KERNEL(int, res_mkquery, (int a, const char* b, int c, int d, const unsigned char* e, int f, const unsigned char* g, unsigned char* h, int i), (a, b, c, d, e, f, g, h, i))
KERNEL(int, res_send, (const unsigned char* a, int b, unsigned char* c, int d), (a, b, c, d))
KERNEL_SIZED(int, res_ninit, (res_state a), (res_state a, size_t a_size), (a, sizeof(struct __res_state)))
KERNEL_VOID_SIZED(res_nclose, (res_state a), (res_state a, size_t a_size), (a, sizeof(struct __res_state)))
KERNEL_SIZED(int, res_nquery, (res_state a, const char* b, int c, int d, unsigned char* e, int f), (res_state a, size_t a_size, const char* b, int c, int d, unsigned char* e, int f), (a, sizeof(struct __res_state), b, c, d, e, f))
KERNEL_SIZED(int, res_nquerydomain, (res_state a, const char* b, const char* c, int d, int e, unsigned char* f, int g), (res_state a, size_t a_size, const char* b, const char* c, int d, int e, unsigned char* f, int g), (a, sizeof(struct __res_state), b, c, d, e, f, g))
KERNEL_SIZED(int, res_nsearch, (res_state a, const char* b, int c, int d, unsigned char* e, int f), (res_state a, size_t a_size, const char* b, int c, int d, unsigned char* e, int f), (a, sizeof(struct __res_state), b, c, d, e, f))
KERNEL_SIZED(int, res_nmkquery, (res_state a, int b, const char* c, int d, int e, const unsigned char* f, int g, const unsigned char* h, unsigned char* i, int j), (res_state a, size_t a_size, int b, const char* c, int d, int e, const unsigned char* f, int g, const unsigned char* h, unsigned char* i, int j), (a, sizeof(struct __res_state), b, c, d, e, f, g, h, i, j))
KERNEL_SIZED(int, res_nsend, (res_state a, const unsigned char* b, int c, unsigned char* d, int e), (res_state a, size_t a_size, const unsigned char* b, int c, unsigned char* d, int e), (a, sizeof(struct __res_state), b, c, d, e))
KERNEL(int, dn_comp, (const char* a, unsigned char* b, int c, unsigned char** d, unsigned char** e), (a, b, c, d, e))
KERNEL(int, dn_expand, (const unsigned char* a, const unsigned char* b, const unsigned char* c, char* d, int e), (a, b, c, d, e))
KERNEL(int, dn_skipname, (const unsigned char* a, const unsigned char* b), (a, b))
KERNEL_SIZED(int, ns_initparse, (const unsigned char* a, int b, ns_msg* c), (const unsigned char* a, int b, ns_msg* c, size_t c_size), (a, b, c, sizeof(ns_msg)))
KERNEL_SIZED(int, ns_parserr, (ns_msg* a, ns_sect b, int c, ns_rr* d), (ns_msg* a, size_t a_size, ns_sect b, int c, ns_rr* d, size_t d_size), (a, sizeof(ns_msg), b, c, d, sizeof(ns_rr)))
KERNEL(int, ns_skiprr, (const unsigned char* a, const unsigned char* b, ns_sect c, int d), (a, b, c, d))
KERNEL(int, ns_name_uncompress, (const unsigned char* a, const unsigned char* b, const unsigned char* c, char* d, size_t e), (a, b, c, d, e))

/* nl_types.h */
KERNEL(nl_catd, catopen, (const char* a, int b), (a, b))
KERNEL(char*, catgets, (nl_catd a, int b, int c, const char* d), (a, b, c, d))
KERNEL(int, catclose, (nl_catd a), (a))

/* ndbm.h */
KERNEL(DBM*, dbm_open, (const char* a, int b, mode_t c), (a, b, c))
KERNEL_VOID(dbm_close, (DBM* a), (a))
KERNEL(int, dbm_error, (DBM* a), (a))
KERNEL(int, dbm_clearerr, (DBM* a), (a))
/* A datum goes to the kernel and comes back as its pointer and size. */
int __kernel_dbm_store(DBM*, const void*, size_t, const void*, size_t, int) __KERNEL(dbm_store);
int __kernel_dbm_delete(DBM*, const void*, size_t) __KERNEL(dbm_delete);
void* __kernel_dbm_fetch(DBM*, const void*, size_t, size_t*) __KERNEL(dbm_fetch);
void* __kernel_dbm_firstkey(DBM*, size_t*) __KERNEL(dbm_firstkey);
void* __kernel_dbm_nextkey(DBM*, size_t*) __KERNEL(dbm_nextkey);

WEAK int dbm_store(DBM* db, datum key, datum content, int mode) {
    return __kernel_dbm_store(db, key.dptr, key.dsize, content.dptr, content.dsize, mode);
}

WEAK int dbm_delete(DBM* db, datum key) {
    return __kernel_dbm_delete(db, key.dptr, key.dsize);
}

WEAK datum dbm_fetch(DBM* db, datum key) {
    datum content;
    content.dptr = __kernel_dbm_fetch(db, key.dptr, key.dsize, &content.dsize);
    return content;
}

WEAK datum dbm_firstkey(DBM* db) {
    datum key;
    key.dptr = __kernel_dbm_firstkey(db, &key.dsize);
    return key;
}

WEAK datum dbm_nextkey(DBM* db) {
    datum key;
    key.dptr = __kernel_dbm_nextkey(db, &key.dsize);
    return key;
}

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
KERNEL_VOID(setservent, (int a), (a))
KERNEL_VOID(endservent, (void), ())
KERNEL_SIZED(struct servent*, getservent, (void), (size_t size), (sizeof(struct servent)))
KERNEL_VOID(setprotoent, (int a), (a))
KERNEL_VOID(endprotoent, (void), ())
KERNEL_SIZED(struct protoent*, getprotoent, (void), (size_t size), (sizeof(struct protoent)))
KERNEL_VOID(setnetent, (int a), (a))
KERNEL_VOID(endnetent, (void), ())
KERNEL_SIZED(struct netent*, getnetent, (void), (size_t size), (sizeof(struct netent)))
KERNEL_SIZED(int, gethostbyname_r, (const char* __restrict a, struct hostent* __restrict b, char* __restrict c, size_t d, struct hostent** __restrict e, int* __restrict f), (const char* __restrict a, struct hostent* __restrict b, size_t b_size, char* __restrict c, size_t d, struct hostent** __restrict e, size_t e_size, int* __restrict f), (a, b, sizeof(struct hostent), c, d, e, sizeof(struct hostent), f))
KERNEL_SIZED(int, gethostbyname2_r, (const char* __restrict a, int b, struct hostent* __restrict c, char* __restrict d, size_t e, struct hostent** __restrict f, int* __restrict g), (const char* __restrict a, int b, struct hostent* __restrict c, size_t c_size, char* __restrict d, size_t e, struct hostent** __restrict f, size_t f_size, int* __restrict g), (a, b, c, sizeof(struct hostent), d, e, f, sizeof(struct hostent), g))
KERNEL_SIZED(int, gethostbyaddr_r, (const void* __restrict a, socklen_t b, int c, struct hostent* __restrict d, char* __restrict e, size_t f, struct hostent** __restrict g, int* __restrict h), (const void* __restrict a, socklen_t b, int c, struct hostent* __restrict d, size_t d_size, char* __restrict e, size_t f, struct hostent** __restrict g, size_t g_size, int* __restrict h), (a, b, c, d, sizeof(struct hostent), e, f, g, sizeof(struct hostent), h))
KERNEL_SIZED(int, getservbyname_r, (const char* __restrict a, const char* __restrict b, struct servent* __restrict c, char* __restrict d, size_t e, struct servent** __restrict f), (const char* __restrict a, const char* __restrict b, struct servent* __restrict c, size_t c_size, char* __restrict d, size_t e, struct servent** __restrict f, size_t f_size), (a, b, c, sizeof(struct servent), d, e, f, sizeof(struct servent)))
KERNEL_SIZED(int, getservbyport_r, (int a, const char* __restrict b, struct servent* __restrict c, char* __restrict d, size_t e, struct servent** __restrict f), (int a, const char* __restrict b, struct servent* __restrict c, size_t c_size, char* __restrict d, size_t e, struct servent** __restrict f, size_t f_size), (a, b, c, sizeof(struct servent), d, e, f, sizeof(struct servent)))
KERNEL_SIZED(struct servent*, getservbyport, (int a, const char* b), (int a, const char* b, size_t size), (a, b, sizeof(struct servent)))
KERNEL_SIZED(struct protoent*, getprotobyname, (const char* a), (const char* a, size_t size), (a, sizeof(struct protoent)))
KERNEL_SIZED(struct protoent*, getprotobynumber, (int a), (int a, size_t size), (a, sizeof(struct protoent)))
KERNEL_SIZED(struct netent*, getnetbyname, (const char* a), (const char* a, size_t size), (a, sizeof(struct netent)))
KERNEL_SIZED(struct netent*, getnetbyaddr, (uint32_t a, int b), (uint32_t a, int b, size_t size), (a, b, sizeof(struct netent)))
KERNEL(const char*, hstrerror, (int a), (a))

/* poll.h */
KERNEL_SIZED(int, poll, (struct pollfd* a, nfds_t b, int c), (struct pollfd* a, size_t a_size, nfds_t b, int c), (a, sizeof(struct pollfd), b, c))
KERNEL_SIZED(int, ppoll, (struct pollfd* a, nfds_t b, const struct timespec* c, const sigset_t* d), (struct pollfd* a, size_t a_size, nfds_t b, const struct timespec* c, size_t c_size, const sigset_t* d, size_t d_size), (a, sizeof(struct pollfd), b, c, sizeof(struct timespec), d, sizeof(sigset_t)))

/* search.h */
KERNEL(int, hcreate, (size_t a), (a))
KERNEL_SIZED(int, hcreate_r, (size_t a, struct hsearch_data* b), (size_t a, struct hsearch_data* b, size_t b_size), (a, b, sizeof(struct hsearch_data)))
KERNEL_VOID(hdestroy, (void), ())
KERNEL_VOID_SIZED(hdestroy_r, (struct hsearch_data* a), (struct hsearch_data* a, size_t a_size), (a, sizeof(struct hsearch_data)))
KERNEL_VOID(insque, (void* a, void* b), (a, b))
KERNEL(void*, lfind, (const void* a, const void* b, size_t* c, size_t d, int (*e)(const void*, const void*)), (a, b, c, d, e))
KERNEL(void*, lsearch, (const void* a, void* b, size_t* c, size_t d, int (*e)(const void*, const void*)), (a, b, c, d, e))
KERNEL_VOID(remque, (void* a), (a))
KERNEL(void*, tdelete, (const void* __restrict a, posix_tnode** __restrict b, int (*c)(const void*, const void*)), (a, b, c))
KERNEL_VOID(tdestroy, (void* a, void (*b)(void*)), (a, b))
KERNEL(posix_tnode*, tfind, (const void* a, posix_tnode* const* b, int (*c)(const void*, const void*)), (a, b, c))
KERNEL(posix_tnode*, tsearch, (const void* a, posix_tnode** b, int (*c)(const void*, const void*)), (a, b, c))
KERNEL_VOID(twalk, (const posix_tnode* a, void (*b)(const posix_tnode*, VISIT, int)), (a, b))

/* An ENTRY goes to the kernel by its place. */
ENTRY* __kernel_hsearch(const ENTRY*, size_t, ACTION) __KERNEL(hsearch);
int __kernel_hsearch_r(const ENTRY*, size_t, ACTION, ENTRY**, struct hsearch_data*, size_t) __KERNEL(hsearch_r);

WEAK ENTRY* hsearch(ENTRY item, ACTION action) {
    return __kernel_hsearch(&item, sizeof item, action);
}

WEAK int hsearch_r(ENTRY item, ACTION action, ENTRY** found, struct hsearch_data* table) {
    return __kernel_hsearch_r(&item, sizeof item, action, found, table, sizeof(struct hsearch_data));
}

/* stdio_ext.h */
KERNEL(size_t, __fbufsize, (FILE* a), (a))
KERNEL(int, __flbf, (FILE* a), (a))
KERNEL(size_t, __fpending, (FILE* a), (a))
KERNEL(int, __fpurge, (FILE* a), (a))
KERNEL(int, __freadable, (FILE* a), (a))
KERNEL(size_t, __freadahead, (FILE* a), (a))
KERNEL(int, __freading, (FILE* a), (a))
KERNEL(const char*, __freadptr, (FILE* a, size_t* b), (a, b))
KERNEL_VOID(__freadptrinc, (FILE* a, size_t b), (a, b))
KERNEL_VOID(__fseterr, (FILE* a), (a))
KERNEL(int, __fsetlocking, (FILE* a, int b), (a, b))
KERNEL(int, __fwritable, (FILE* a), (a))
KERNEL(int, __fwriting, (FILE* a), (a))
KERNEL_VOID(_flushlbf, (void), ())

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
KERNEL(int, pthread_getname_np, (pthread_t a, char* b, size_t c), (a, b, c))
KERNEL(int, pthread_setname_np, (pthread_t a, const char* b), (a, b))
KERNEL(int, pthread_tryjoin_np, (pthread_t a, void** b), (a, b))
KERNEL_SIZED(int, pthread_timedjoin_np, (pthread_t a, void** b, const struct timespec* c), (pthread_t a, void** b, const struct timespec* c, size_t c_size), (a, b, c, sizeof(struct timespec)))
KERNEL(int, __pthread_once_begin, (pthread_once_t* a), (a))
KERNEL_VOID(__pthread_once_end, (pthread_once_t* a), (a))
KERNEL(int, __pthread_cancelled, (int a), (a))
KERNEL(void*, __pthread_key_take, (pthread_key_t* a, void (**b)(void*)), (a, b))
KERNEL_VOID_SIZED(_pthread_cleanup_pop, (struct __ptcb* a, int b), (struct __ptcb* a, size_t a_size, int b), (a, sizeof(struct __ptcb), b))
KERNEL_VOID_SIZED(_pthread_cleanup_push, (struct __ptcb* a, void (*b)(void*), void* c), (struct __ptcb* a, size_t a_size, void (*b)(void*), void* c), (a, sizeof(struct __ptcb), b, c))
KERNEL(int, pthread_atfork, (void (*a)(void), void (*b)(void), void (*c)(void)), (a, b, c))
KERNEL_NORETURN(pthread_exit, (void* a), (a))
KERNEL(int, pthread_once, (pthread_once_t* a, void (*b)(void)), (a, b))
KERNEL_VOID(pthread_testcancel, (void), ())

/* pty.h */
KERNEL_SIZED(int, openpty, (int* a, int* b, char* c, const struct termios* d, const struct winsize* e), (int* a, int* b, char* c, const struct termios* d, size_t d_size, const struct winsize* e, size_t e_size), (a, b, c, d, sizeof(struct termios), e, sizeof(struct winsize)))
KERNEL_SIZED(pid_t, forkpty, (int* a, char* b, const struct termios* c, const struct winsize* d), (int* a, char* b, const struct termios* c, size_t c_size, const struct winsize* d, size_t d_size), (a, b, c, sizeof(struct termios), d, sizeof(struct winsize)))

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
KERNEL_VOID(clearerr, (FILE* a), (a))
KERNEL_VOID(clearerr_unlocked, (FILE* a), (a))
KERNEL(int, fclose, (FILE* a), (a))
KERNEL(FILE*, fdopen, (int a, const char* b), (a, b))
KERNEL(int, feof, (FILE* a), (a))
KERNEL(int, feof_unlocked, (FILE* a), (a))
KERNEL(int, ferror, (FILE* a), (a))
KERNEL(int, ferror_unlocked, (FILE* a), (a))
KERNEL(int, fflush, (FILE* a), (a))
KERNEL(int, fflush_unlocked, (FILE* a), (a))
KERNEL(int, fgetc, (FILE* a), (a))
KERNEL(int, fgetc_unlocked, (FILE* a), (a))
KERNEL(char*, fgetln, (FILE* a, size_t* b), (a, b))
KERNEL(int, fgetpos, (FILE* __restrict a, fpos_t* __restrict b), (a, b))
KERNEL(char*, fgets, (char* __restrict a, int b, FILE* __restrict c), (a, b, c))
KERNEL(char*, fgets_unlocked, (char* a, int b, FILE* c), (a, b, c))
KERNEL(int, fileno, (FILE* a), (a))
KERNEL(int, fileno_unlocked, (FILE* a), (a))
KERNEL_VOID(flockfile, (FILE* a), (a))
KERNEL(FILE*, fmemopen, (void* __restrict a, size_t b, const char* __restrict c), (a, b, c))
KERNEL(FILE*, fopen, (const char* a, const char* b), (a, b))
KERNEL(int, fpurge, (FILE* a), (a))
KERNEL(int, fputc, (int a, FILE* b), (a, b))
KERNEL(int, fputc_unlocked, (int a, FILE* b), (a, b))
KERNEL(int, fputs, (const char* __restrict a, FILE* __restrict b), (a, b))
KERNEL(int, fputs_unlocked, (const char* a, FILE* b), (a, b))
KERNEL(size_t, fread, (void* a, size_t b, size_t c, FILE* d), (a, b, c, d))
KERNEL(size_t, fread_unlocked, (void* a, size_t b, size_t c, FILE* d), (a, b, c, d))
KERNEL(FILE*, freopen, (const char* __restrict a, const char* __restrict b, FILE* __restrict c), (a, b, c))
KERNEL(int, fseek, (FILE* a, long b, int c), (a, b, c))
KERNEL(int, fseeko, (FILE* a, off_t b, int c), (a, b, c))
KERNEL(int, fsetpos, (FILE* a, const fpos_t* b), (a, b))
KERNEL(long, ftell, (FILE* a), (a))
KERNEL(off_t, ftello, (FILE* a), (a))
KERNEL(int, ftrylockfile, (FILE* a), (a))
KERNEL_VOID(funlockfile, (FILE* a), (a))
KERNEL(FILE*, funopen, (const void* a, int (*b)(void*, char*, int), int (*c)(void*, const char*, int), fpos_t (*d)(void*, fpos_t, int), int (*e)(void*)), (a, b, c, d, e))
KERNEL(size_t, fwrite, (const void* a, size_t b, size_t c, FILE* d), (a, b, c, d))
KERNEL(size_t, fwrite_unlocked, (const void* a, size_t b, size_t c, FILE* d), (a, b, c, d))
KERNEL(int, getc, (FILE* a), (a))
KERNEL(int, getc_unlocked, (FILE* a), (a))
KERNEL(int, getchar, (void), ())
KERNEL(int, getchar_unlocked, (void), ())
KERNEL(ssize_t, getdelim, (char** __restrict a, size_t* __restrict b, int c, FILE* __restrict d), (a, b, c, d))
KERNEL(ssize_t, getline, (char** __restrict a, size_t* __restrict b, FILE* __restrict c), (a, b, c))
KERNEL(FILE*, open_memstream, (char** a, size_t* b), (a, b))
KERNEL(int, pclose, (FILE* a), (a))
KERNEL_VOID(perror, (const char* a), (a))
KERNEL(FILE*, popen, (const char* a, const char* b), (a, b))
KERNEL(int, putc, (int a, FILE* b), (a, b))
KERNEL(int, putc_unlocked, (int a, FILE* b), (a, b))
KERNEL(int, putchar, (int a), (a))
KERNEL(int, putchar_unlocked, (int a), (a))
KERNEL(int, puts, (const char* a), (a))
KERNEL_VOID(rewind, (FILE* a), (a))
KERNEL_VOID(setbuf, (FILE* __restrict a, char* __restrict b), (a, b))
KERNEL_VOID(setbuffer, (FILE* a, char* b, size_t c), (a, b, c))
KERNEL_VOID(setlinebuf, (FILE* a), (a))
KERNEL(int, setvbuf, (FILE* __restrict a, char* __restrict b, int c, size_t d), (a, b, c, d))
KERNEL(FILE*, tmpfile, (void), ())
KERNEL(char*, tmpnam, (char* a), (a))
KERNEL(int, ungetc, (int a, FILE* b), (a, b))
KERNEL(int, vasprintf, (char** a, const char* b, va_list c), (a, b, c))
KERNEL(int, vdprintf, (int a, const char* __restrict b, va_list c), (a, b, c))
KERNEL(int, vfprintf, (FILE* __restrict a, const char* __restrict b, va_list c), (a, b, c))
KERNEL(int, vfscanf, (FILE* __restrict a, const char* __restrict b, va_list c), (a, b, c))
KERNEL(int, vprintf, (const char* __restrict a, va_list b), (a, b))
KERNEL(int, vscanf, (const char* __restrict a, va_list b), (a, b))
KERNEL(int, vsnprintf, (char* __restrict a, size_t b, const char* __restrict c, va_list d), (a, b, c, d))
KERNEL(int, vsprintf, (char* __restrict a, const char* __restrict b, va_list c), (a, b, c))
KERNEL(int, vsscanf, (const char* __restrict a, const char* __restrict b, va_list c), (a, b, c))
KERNEL(FILE*, __stdin, (void), ())
KERNEL(FILE*, __stdout, (void), ())
KERNEL(FILE*, __stderr, (void), ())

/* fopencookie's functions go to the kernel by their place. */
FILE* __kernel_fopencookie(void*, const char*, const cookie_io_functions_t*, size_t) __KERNEL(fopencookie);

WEAK FILE* fopencookie(void* cookie, const char* mode, cookie_io_functions_t functions) {
    return __kernel_fopencookie(cookie, mode, &functions, sizeof functions);
}

/* stdlib.h */
KERNEL(void*, reallocarray, (void* a, size_t b, size_t c), (a, b, c))
KERNEL(void*, aligned_alloc, (size_t a, size_t b), (a, b))
KERNEL(int, posix_memalign, (void** a, size_t b, size_t c), (a, b, c))
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
KERNEL(char*, mktemp, (char* a), (a))
KERNEL(int, mkstemp, (char* a), (a))
KERNEL(int, mkostemp, (char* a, int b), (a, b))
KERNEL(int, mkstemps, (char* a, int b), (a, b))
KERNEL(int, mkostemps, (char* a, int b, int c), (a, b, c))
KERNEL_VOID(setkey, (const char* a), (a))
KERNEL(char*, mkdtemp, (char* a), (a))
KERNEL(long, a64l, (const char* a), (a))
KERNEL(char*, l64a, (long a), (a))
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
KERNEL(int, at_quick_exit, (void (*a)(void)), (a))
KERNEL(int, atexit, (void (*a)(void)), (a))
KERNEL(void*, bsearch, (const void* a, const void* b, size_t c, size_t d, int (*e)(const void*, const void*)), (a, b, c, d, e))
KERNEL(double, drand48, (void), ())
KERNEL(double, erand48, (unsigned short* a), (a))
KERNEL_NORETURN(exit, (int a), (a))
KERNEL(long, jrand48, (unsigned short* a), (a))
KERNEL_VOID(lcong48, (unsigned short* a), (a))
KERNEL(long, lrand48, (void), ())
KERNEL(int, mblen, (const char* a, size_t b), (a, b))
KERNEL(size_t, mbstowcs, (wchar_t* __restrict a, const char* __restrict b, size_t c), (a, b, c))
KERNEL(int, mbtowc, (wchar_t* __restrict a, const char* __restrict b, size_t c), (a, b, c))
KERNEL(long, mrand48, (void), ())
KERNEL(long, nrand48, (unsigned short* a), (a))
KERNEL_VOID(qsort, (void* a, size_t b, size_t c, int (*d)(const void*, const void*)), (a, b, c, d))
KERNEL_VOID(qsort_r, (void* a, size_t b, size_t c, int (*d)(const void*, const void*, void*), void* e), (a, b, c, d, e))
KERNEL_NORETURN(quick_exit, (int a), (a))
KERNEL(unsigned short*, seed48, (unsigned short* a), (a))
KERNEL_VOID(srand48, (long a), (a))
KERNEL(size_t, wcstombs, (char* __restrict a, const wchar_t* __restrict b, size_t c), (a, b, c))
KERNEL(int, wctomb, (char* a, wchar_t b), (a, b))

/* The C++ ABI's: an object's destructor to run at exit, and one of a thread's. */
KERNEL(int, __cxa_atexit, (void (*a)(void*), void* b, void* c), (a, b, c))
KERNEL(int, __cxa_thread_atexit, (void (*a)(void*), void* b, void* c), (a, b, c))

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
KERNEL(int, strcoll_l, (const char* a, const char* b, locale_t c), (a, b, c))
KERNEL(size_t, strxfrm_l, (char* __restrict a, const char* __restrict b, size_t c, locale_t d), (a, b, c, d))

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
KERNEL(int, memfile, (void* a, size_t b, int c), (a, b, c))
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
KERNEL_SIZED(int, sendmmsg, (int a, struct mmsghdr* b, unsigned c, int d), (int a, struct mmsghdr* b, size_t b_size, size_t iov_size, unsigned c, int d), (a, b, sizeof(struct mmsghdr), sizeof(struct iovec), c, d))
KERNEL_SIZED(int, recvmmsg, (int a, struct mmsghdr* b, unsigned c, int d, struct timespec* e), (int a, struct mmsghdr* b, size_t b_size, size_t iov_size, unsigned c, int d, struct timespec* e, size_t e_size), (a, b, sizeof(struct mmsghdr), sizeof(struct iovec), c, d, e, sizeof(struct timespec)))
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
KERNEL_VOID(vsyslog, (int a, const char* b, va_list c), (a, b, c))

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
KERNEL_VOID(call_once, (once_flag* a, void (*b)(void)), (a, b))
KERNEL_NORETURN(thrd_exit, (int a), (a))

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
KERNEL_SIZED(size_t, strftime_l, (char* __restrict a, size_t b, const char* __restrict c, const struct tm* __restrict d, locale_t e), (char* __restrict a, size_t b, const char* __restrict c, const struct tm* __restrict d, size_t d_size, locale_t e), (a, b, c, d, sizeof(struct tm), e))
KERNEL(int*, __getdate_err, (void), ())
KERNEL_SIZED(struct tm*, getdate, (const char* a), (const char* a, size_t size), (a, sizeof(struct tm)))

/* uchar.h */
KERNEL_SIZED(size_t, c16rtomb, (char* __restrict a, char16_t b, mbstate_t* __restrict c), (char* __restrict a, char16_t b, mbstate_t* __restrict c, size_t c_size), (a, b, c, sizeof(mbstate_t)))
KERNEL_SIZED(size_t, c32rtomb, (char* __restrict a, char32_t b, mbstate_t* __restrict c), (char* __restrict a, char32_t b, mbstate_t* __restrict c, size_t c_size), (a, b, c, sizeof(mbstate_t)))
KERNEL_SIZED(size_t, mbrtoc16, (char16_t* __restrict a, const char* __restrict b, size_t c, mbstate_t* __restrict d), (char16_t* __restrict a, const char* __restrict b, size_t c, mbstate_t* __restrict d, size_t d_size), (a, b, c, d, sizeof(mbstate_t)))
KERNEL_SIZED(size_t, mbrtoc32, (char32_t* __restrict a, const char* __restrict b, size_t c, mbstate_t* __restrict d), (char32_t* __restrict a, const char* __restrict b, size_t c, mbstate_t* __restrict d, size_t d_size), (a, b, c, d, sizeof(mbstate_t)))

/* unistd.h */
KERNEL(ssize_t, read, (int a, void* b, size_t c), (a, b, c))
KERNEL(ssize_t, write, (int a, const void* b, size_t c), (a, b, c))
KERNEL(ssize_t, pread, (int a, void* b, size_t c, off_t d), (a, b, c, d))
KERNEL(ssize_t, pwrite, (int a, const void* b, size_t c, off_t d), (a, b, c, d))
KERNEL(ssize_t, copy_file_range, (int a, off_t* b, int c, off_t* d, size_t e, unsigned f), (a, b, c, d, e, f))
KERNEL(off_t, lseek, (int a, off_t b, int c), (a, b, c))
KERNEL(int, close, (int a), (a))
KERNEL(int, posix_close, (int a, int b), (a, b))
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
KERNEL_SIZED(int, wordexp, (const char* __restrict a, wordexp_t* __restrict b, int c), (const char* __restrict a, wordexp_t* __restrict b, size_t b_size, int c), (a, b, sizeof(wordexp_t), c))
KERNEL_VOID_SIZED(wordfree, (wordexp_t* a), (wordexp_t* a, size_t a_size), (a, sizeof(wordexp_t)))
KERNEL(ssize_t, getrandom, (void* a, size_t b, unsigned c), (a, b, c))
KERNEL(int, posix_madvise, (void* a, size_t b, int c), (a, b, c))
KERNEL(int, fmtmsg, (long a, const char* b, int c, const char* d, const char* e, const char* f), (a, b, c, d, e, f))
KERNEL(char*, ether_ntoa, (const struct ether_addr* a), (a))
KERNEL(char*, ether_ntoa_r, (const struct ether_addr* a, char* b), (a, b))
KERNEL(struct ether_addr*, ether_aton, (const char* a), (a))
KERNEL(struct ether_addr*, ether_aton_r, (const char* a, struct ether_addr* b), (a, b))
KERNEL(int, ether_ntohost, (char* a, const struct ether_addr* b), (a, b))
KERNEL(int, ether_hostton, (const char* a, struct ether_addr* b), (a, b))
KERNEL(int, ether_line, (const char* a, struct ether_addr* b, char* c), (a, b, c))
KERNEL_SIZED(int, aio_read, (struct aiocb* a), (struct aiocb* a, size_t a_size), (a, sizeof(struct aiocb)))
KERNEL_SIZED(int, aio_write, (struct aiocb* a), (struct aiocb* a, size_t a_size), (a, sizeof(struct aiocb)))
KERNEL_SIZED(int, aio_fsync, (int a, struct aiocb* b), (int a, struct aiocb* b, size_t b_size), (a, b, sizeof(struct aiocb)))
KERNEL_SIZED(int, aio_error, (const struct aiocb* a), (const struct aiocb* a, size_t a_size), (a, sizeof(struct aiocb)))
KERNEL_SIZED(ssize_t, aio_return, (struct aiocb* a), (struct aiocb* a, size_t a_size), (a, sizeof(struct aiocb)))
KERNEL_SIZED(int, aio_suspend, (const struct aiocb* const a[], int b, const struct timespec* c), (const struct aiocb* const a[], size_t a_size, int b, const struct timespec* c, size_t c_size), (a, sizeof(struct aiocb), b, c, sizeof(struct timespec)))
KERNEL_SIZED(int, aio_cancel, (int a, struct aiocb* b), (int a, struct aiocb* b, size_t b_size), (a, b, sizeof(struct aiocb)))
KERNEL_SIZED(int, lio_listio, (int a, struct aiocb* __restrict const* __restrict b, int c, struct sigevent* __restrict d), (int a, struct aiocb* __restrict const* __restrict b, size_t b_size, int c, struct sigevent* __restrict d, size_t d_size), (a, b, sizeof(struct aiocb), c, d, sizeof(struct sigevent)))
KERNEL_VOID(setutxent, (void), ())
KERNEL_VOID(endutxent, (void), ())
KERNEL_SIZED(struct utmpx*, getutxent, (void), (size_t size), (sizeof(struct utmpx)))
KERNEL_SIZED(struct utmpx*, getutxid, (const struct utmpx* a), (const struct utmpx* a, size_t a_size, size_t size), (a, sizeof(struct utmpx), sizeof(struct utmpx)))
KERNEL_SIZED(struct utmpx*, getutxline, (const struct utmpx* a), (const struct utmpx* a, size_t a_size, size_t size), (a, sizeof(struct utmpx), sizeof(struct utmpx)))
KERNEL_SIZED(struct utmpx*, pututxline, (const struct utmpx* a), (const struct utmpx* a, size_t a_size, size_t size), (a, sizeof(struct utmpx), sizeof(struct utmpx)))
KERNEL(int, mq_close, (mqd_t a), (a))
KERNEL(int, mq_unlink, (const char* a), (a))
KERNEL(int, mq_send, (mqd_t a, const char* b, size_t c, unsigned d), (a, b, c, d))
KERNEL_SIZED(int, mq_timedsend, (mqd_t a, const char* b, size_t c, unsigned d, const struct timespec* e), (mqd_t a, const char* b, size_t c, unsigned d, const struct timespec* e, size_t e_size), (a, b, c, d, e, sizeof(struct timespec)))
KERNEL(ssize_t, mq_receive, (mqd_t a, char* b, size_t c, unsigned* d), (a, b, c, d))
KERNEL_SIZED(ssize_t, mq_timedreceive, (mqd_t a, char* __restrict b, size_t c, unsigned* __restrict d, const struct timespec* __restrict e), (mqd_t a, char* __restrict b, size_t c, unsigned* __restrict d, const struct timespec* __restrict e, size_t e_size), (a, b, c, d, e, sizeof(struct timespec)))
KERNEL_SIZED(int, mq_getattr, (mqd_t a, struct mq_attr* b), (mqd_t a, struct mq_attr* b, size_t b_size), (a, b, sizeof(struct mq_attr)))
KERNEL_SIZED(int, mq_setattr, (mqd_t a, const struct mq_attr* __restrict b, struct mq_attr* __restrict c), (mqd_t a, const struct mq_attr* __restrict b, size_t b_size, struct mq_attr* __restrict c, size_t c_size), (a, b, sizeof(struct mq_attr), c, sizeof(struct mq_attr)))
KERNEL_SIZED(int, mq_notify, (mqd_t a, const struct sigevent* b), (mqd_t a, const struct sigevent* b, size_t b_size), (a, b, sizeof(struct sigevent)))
KERNEL(key_t, ftok, (const char* a, int b), (a, b))
KERNEL(int, msgget, (key_t a, int b), (a, b))
KERNEL(int, msgsnd, (int a, const void* b, size_t c, int d), (a, b, c, d))
KERNEL(ssize_t, msgrcv, (int a, void* b, size_t c, long d, int e), (a, b, c, d, e))
KERNEL_SIZED(int, msgctl, (int a, int b, struct msqid_ds* c), (int a, int b, struct msqid_ds* c, size_t c_size), (a, b, c, sizeof(struct msqid_ds)))
KERNEL(int, semget, (key_t a, int b, int c), (a, b, c))
KERNEL_SIZED(int, semop, (int a, struct sembuf* b, size_t c), (int a, struct sembuf* b, size_t b_size, size_t c), (a, b, sizeof(struct sembuf), c))
KERNEL_SIZED(int, semtimedop, (int a, struct sembuf* b, size_t c, const struct timespec* d), (int a, struct sembuf* b, size_t b_size, size_t c, const struct timespec* d, size_t d_size), (a, b, sizeof(struct sembuf), c, d, sizeof(struct timespec)))
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
KERNEL(pid_t, fork, (void), ())

/* _Fork, the kernel's fork, returns in the child as well. */
pid_t __kernel__Fork(void) __RETURNS_TWICE __KERNEL(_Fork);

WEAK pid_t _Fork(void) {
    return __kernel__Fork();
}

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
KERNEL(int, eaccess, (const char* a, int b), (a, b))
KERNEL(int, chdir, (const char* a), (a))
KERNEL(int, fchdir, (int a), (a))
KERNEL(int, chroot, (const char* a), (a))
KERNEL(int, attach, (int a), (a))
KERNEL(int, watchdir, (int a, int b), (a, b))
KERNEL(int, fdid, (int a, unsigned long long* b), (a, b))
KERNEL(char*, getcwd, (char* a, size_t b), (a, b))
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
KERNEL(char*, getusershell, (void), ())
KERNEL_VOID(setusershell, (void), ())
KERNEL_VOID(endusershell, (void), ())
KERNEL(char*, crypt, (const char* a, const char* b), (a, b))
KERNEL_VOID(encrypt, (char* a, int b), (a, b))
KERNEL(char*, getpass, (const char* a), (a))
KERNEL(int, daemon, (int a, int b), (a, b))
KERNEL(int, acct, (const char* a), (a))
KERNEL(unsigned, sleep, (unsigned a), (a))
KERNEL(int, usleep, (useconds_t a), (a))
KERNEL(unsigned, alarm, (unsigned a), (a))
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
KERNEL(wint_t, btowc, (int a), (a))
KERNEL(wint_t, fgetwc, (FILE* a), (a))
KERNEL(wchar_t*, fgetws, (wchar_t* __restrict a, int b, FILE* __restrict c), (a, b, c))
KERNEL(wint_t, fputwc, (wchar_t a, FILE* b), (a, b))
KERNEL(int, fputws, (const wchar_t* __restrict a, FILE* __restrict b), (a, b))
KERNEL(int, fwide, (FILE* a, int b), (a, b))
KERNEL(wint_t, getwc, (FILE* a), (a))
KERNEL(wint_t, getwchar, (void), ())
KERNEL_SIZED(size_t, mbrlen, (const char* __restrict a, size_t b, mbstate_t* __restrict c), (const char* __restrict a, size_t b, mbstate_t* __restrict c, size_t c_size), (a, b, c, sizeof(mbstate_t)))
KERNEL_SIZED(size_t, mbrtowc, (wchar_t* __restrict a, const char* __restrict b, size_t c, mbstate_t* __restrict d), (wchar_t* __restrict a, const char* __restrict b, size_t c, mbstate_t* __restrict d, size_t d_size), (a, b, c, d, sizeof(mbstate_t)))
KERNEL_SIZED(int, mbsinit, (const mbstate_t* a), (const mbstate_t* a, size_t a_size), (a, sizeof(mbstate_t)))
KERNEL_SIZED(size_t, mbsnrtowcs, (wchar_t* __restrict a, const char** __restrict b, size_t c, size_t d, mbstate_t* __restrict e), (wchar_t* __restrict a, const char** __restrict b, size_t c, size_t d, mbstate_t* __restrict e, size_t e_size), (a, b, c, d, e, sizeof(mbstate_t)))
KERNEL_SIZED(size_t, mbsrtowcs, (wchar_t* __restrict a, const char** __restrict b, size_t c, mbstate_t* __restrict d), (wchar_t* __restrict a, const char** __restrict b, size_t c, mbstate_t* __restrict d, size_t d_size), (a, b, c, d, sizeof(mbstate_t)))
KERNEL(FILE*, open_wmemstream, (wchar_t** a, size_t* b), (a, b))
KERNEL(wint_t, putwc, (wchar_t a, FILE* b), (a, b))
KERNEL(wint_t, putwchar, (wchar_t a), (a))
KERNEL(wint_t, ungetwc, (wint_t a, FILE* b), (a, b))
KERNEL(int, vfwprintf, (FILE* __restrict a, const wchar_t* __restrict b, va_list c), (a, b, c))
KERNEL(int, vfwscanf, (FILE* __restrict a, const wchar_t* __restrict b, va_list c), (a, b, c))
KERNEL(int, vswprintf, (wchar_t* __restrict a, size_t b, const wchar_t* __restrict c, va_list d), (a, b, c, d))
KERNEL(int, vswscanf, (const wchar_t* __restrict a, const wchar_t* __restrict b, va_list c), (a, b, c))
KERNEL(int, vwprintf, (const wchar_t* __restrict a, va_list b), (a, b))
KERNEL(int, vwscanf, (const wchar_t* __restrict a, va_list b), (a, b))
KERNEL_SIZED(size_t, wcrtomb, (char* __restrict a, wchar_t b, mbstate_t* __restrict c), (char* __restrict a, wchar_t b, mbstate_t* __restrict c, size_t c_size), (a, b, c, sizeof(mbstate_t)))
KERNEL(int, wcscoll_l, (const wchar_t* a, const wchar_t* b, locale_t c), (a, b, c))
KERNEL_SIZED(size_t, wcsnrtombs, (char* __restrict a, const wchar_t** __restrict b, size_t c, size_t d, mbstate_t* __restrict e), (char* __restrict a, const wchar_t** __restrict b, size_t c, size_t d, mbstate_t* __restrict e, size_t e_size), (a, b, c, d, e, sizeof(mbstate_t)))
KERNEL_SIZED(size_t, wcsrtombs, (char* __restrict a, const wchar_t** __restrict b, size_t c, mbstate_t* __restrict d), (char* __restrict a, const wchar_t** __restrict b, size_t c, mbstate_t* __restrict d, size_t d_size), (a, b, c, d, sizeof(mbstate_t)))
KERNEL(size_t, wcsxfrm_l, (wchar_t* __restrict a, const wchar_t* __restrict b, size_t c, locale_t d), (a, b, c, d))
KERNEL(int, wctob, (wint_t a), (a))

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
KERNEL(int, iswalnum_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswalpha_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswblank_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswcntrl_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswctype_l, (wint_t a, wctype_t b, locale_t c), (a, b, c))
KERNEL(int, iswdigit_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswgraph_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswlower_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswprint_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswpunct_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswspace_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswupper_l, (wint_t a, locale_t b), (a, b))
KERNEL(int, iswxdigit_l, (wint_t a, locale_t b), (a, b))
KERNEL(wint_t, towlower_l, (wint_t a, locale_t b), (a, b))
KERNEL(wint_t, towupper_l, (wint_t a, locale_t b), (a, b))
KERNEL(wctype_t, wctype_l, (const char* a, locale_t b), (a, b))

/*
 * Of a cancellation that may end the thread at once: after the kernel's
 * call, a cancellation pending that may act now ends it, by pthread_exit,
 * which runs what the thread pushed (lib/libc's pthread.c).
 */
int __kernel_pthread_cancel(pthread_t) __KERNEL(pthread_cancel);
int __kernel_pthread_setcanceltype(int, int*) __KERNEL(pthread_setcanceltype);

WEAK int pthread_cancel(pthread_t thread) {
    int e = __kernel_pthread_cancel(thread);
    if (e == 0 && thread == pthread_self() && __pthread_cancelled(1)) {
        pthread_exit(PTHREAD_CANCELED);
    }
    return e;
}

WEAK int pthread_setcanceltype(int type, int* old) {
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
mqd_t __kernel_mq_open(const char*, int, va_list, size_t) __KERNEL(mq_open);
int __kernel_semctl(int, int, int, va_list, size_t) __KERNEL(semctl);

WEAK int open(const char* path, int flags, ...) {
    va_list ap;
    va_start(ap, flags);
    int r = __kernel_open(path, flags, ap);
    va_end(ap);
    return r;
}

WEAK sem_t* sem_open(const char* name, int flags, ...) {
    va_list ap;
    va_start(ap, flags);
    sem_t* r = __kernel_sem_open(name, flags, ap, sizeof(sem_t));
    va_end(ap);
    return r;
}

WEAK mqd_t mq_open(const char* name, int flags, ...) {
    va_list ap;
    va_start(ap, flags);
    mqd_t r = __kernel_mq_open(name, flags, ap, sizeof(struct mq_attr));
    va_end(ap);
    return r;
}

WEAK int semctl(int id, int num, int cmd, ...) {
    va_list ap;
    va_start(ap, cmd);
    int r = __kernel_semctl(id, num, cmd, ap, sizeof(struct semid_ds));
    va_end(ap);
    return r;
}

WEAK int openat(int dirfd, const char* path, int flags, ...) {
    va_list ap;
    va_start(ap, flags);
    int r = __kernel_openat(dirfd, path, flags, ap);
    va_end(ap);
    return r;
}

WEAK int fcntl(int fd, int cmd, ...) {
    va_list ap;
    va_start(ap, cmd);
    // The size of the structure the command passes, or 0.
    size_t size = cmd == F_GETLK || cmd == F_SETLK || cmd == F_SETLKW ? sizeof(struct flock) : 0;
    int r = __kernel_fcntl(fd, cmd, ap, size);
    va_end(ap);
    return r;
}

WEAK int ioctl(int fd, unsigned long request, ...) {
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

WEAK int prctl(int option, ...) {
    va_list ap;
    va_start(ap, option);
    int r = __kernel_prctl(option, ap);
    va_end(ap);
    return r;
}

WEAK int execl(const char* path, const char* arg, ...) {
    va_list ap;
    va_start(ap, arg);
    int r = __kernel_execl(path, arg, ap);
    va_end(ap);
    return r;
}

WEAK int execlp(const char* file, const char* arg, ...) {
    va_list ap;
    va_start(ap, arg);
    int r = __kernel_execlp(file, arg, ap);
    va_end(ap);
    return r;
}

WEAK int execle(const char* path, const char* arg, ...) {
    va_list ap;
    va_start(ap, arg);
    int r = __kernel_execle(path, arg, ap);
    va_end(ap);
    return r;
}

__NORETURN void __kernel_err(int, const char*, va_list) __KERNEL(err);
__NORETURN void __kernel_errx(int, const char*, va_list) __KERNEL(errx);
void __kernel_warn(const char*, va_list) __KERNEL(warn);
void __kernel_warnx(const char*, va_list) __KERNEL(warnx);
ssize_t __kernel_strfmon(char*, size_t, const char*, va_list) __KERNEL(strfmon);
ssize_t __kernel_strfmon_l(char*, size_t, locale_t, const char*, va_list) __KERNEL(strfmon_l);
int __kernel_asprintf(char**, const char*, va_list) __KERNEL(asprintf);
int __kernel_dprintf(int, const char*, va_list) __KERNEL(dprintf);
int __kernel_fprintf(FILE*, const char*, va_list) __KERNEL(fprintf);
int __kernel_fscanf(FILE*, const char*, va_list) __KERNEL(fscanf);
int __kernel_printf(const char*, va_list) __KERNEL(printf);
int __kernel_scanf(const char*, va_list) __KERNEL(scanf);
int __kernel_snprintf(char*, size_t, const char*, va_list) __KERNEL(snprintf);
int __kernel_sprintf(char*, const char*, va_list) __KERNEL(sprintf);
int __kernel_sscanf(const char*, const char*, va_list) __KERNEL(sscanf);
void __kernel_syslog(int, const char*, va_list) __KERNEL(syslog);
int __kernel_fwprintf(FILE*, const wchar_t*, va_list) __KERNEL(fwprintf);
int __kernel_fwscanf(FILE*, const wchar_t*, va_list) __KERNEL(fwscanf);
int __kernel_swprintf(wchar_t*, size_t, const wchar_t*, va_list) __KERNEL(swprintf);
int __kernel_swscanf(const wchar_t*, const wchar_t*, va_list) __KERNEL(swscanf);
int __kernel_wprintf(const wchar_t*, va_list) __KERNEL(wprintf);
int __kernel_wscanf(const wchar_t*, va_list) __KERNEL(wscanf);

WEAK __NORETURN void err(int a, const char* b, ...) {
    va_list ap;
    va_start(ap, b);
    __kernel_err(a, b, ap);
}

WEAK __NORETURN void errx(int a, const char* b, ...) {
    va_list ap;
    va_start(ap, b);
    __kernel_errx(a, b, ap);
}

WEAK void warn(const char* a, ...) {
    va_list ap;
    va_start(ap, a);
    __kernel_warn(a, ap);
    va_end(ap);
}

WEAK void warnx(const char* a, ...) {
    va_list ap;
    va_start(ap, a);
    __kernel_warnx(a, ap);
    va_end(ap);
}

WEAK ssize_t strfmon(char* __restrict a, size_t b, const char* __restrict c, ...) {
    va_list ap;
    va_start(ap, c);
    ssize_t r = __kernel_strfmon(a, b, c, ap);
    va_end(ap);
    return r;
}

WEAK ssize_t strfmon_l(char* __restrict a, size_t b, locale_t c, const char* __restrict d, ...) {
    va_list ap;
    va_start(ap, d);
    ssize_t r = __kernel_strfmon_l(a, b, c, d, ap);
    va_end(ap);
    return r;
}

WEAK int asprintf(char** a, const char* b, ...) {
    va_list ap;
    va_start(ap, b);
    int r = __kernel_asprintf(a, b, ap);
    va_end(ap);
    return r;
}

WEAK int dprintf(int a, const char* __restrict b, ...) {
    va_list ap;
    va_start(ap, b);
    int r = __kernel_dprintf(a, b, ap);
    va_end(ap);
    return r;
}

WEAK int fprintf(FILE* __restrict a, const char* __restrict b, ...) {
    va_list ap;
    va_start(ap, b);
    int r = __kernel_fprintf(a, b, ap);
    va_end(ap);
    return r;
}

WEAK int fscanf(FILE* __restrict a, const char* __restrict b, ...) {
    va_list ap;
    va_start(ap, b);
    int r = __kernel_fscanf(a, b, ap);
    va_end(ap);
    return r;
}

WEAK int printf(const char* a, ...) {
    va_list ap;
    va_start(ap, a);
    int r = __kernel_printf(a, ap);
    va_end(ap);
    return r;
}

WEAK int scanf(const char* __restrict a, ...) {
    va_list ap;
    va_start(ap, a);
    int r = __kernel_scanf(a, ap);
    va_end(ap);
    return r;
}

WEAK int snprintf(char* __restrict a, size_t b, const char* __restrict c, ...) {
    va_list ap;
    va_start(ap, c);
    int r = __kernel_snprintf(a, b, c, ap);
    va_end(ap);
    return r;
}

WEAK int sprintf(char* __restrict a, const char* __restrict b, ...) {
    va_list ap;
    va_start(ap, b);
    int r = __kernel_sprintf(a, b, ap);
    va_end(ap);
    return r;
}

WEAK int sscanf(const char* __restrict a, const char* __restrict b, ...) {
    va_list ap;
    va_start(ap, b);
    int r = __kernel_sscanf(a, b, ap);
    va_end(ap);
    return r;
}

WEAK void syslog(int a, const char* b, ...) {
    va_list ap;
    va_start(ap, b);
    __kernel_syslog(a, b, ap);
    va_end(ap);
}

WEAK int fwprintf(FILE* __restrict a, const wchar_t* __restrict b, ...) {
    va_list ap;
    va_start(ap, b);
    int r = __kernel_fwprintf(a, b, ap);
    va_end(ap);
    return r;
}

WEAK int fwscanf(FILE* __restrict a, const wchar_t* __restrict b, ...) {
    va_list ap;
    va_start(ap, b);
    int r = __kernel_fwscanf(a, b, ap);
    va_end(ap);
    return r;
}

WEAK int swprintf(wchar_t* __restrict a, size_t b, const wchar_t* __restrict c, ...) {
    va_list ap;
    va_start(ap, c);
    int r = __kernel_swprintf(a, b, c, ap);
    va_end(ap);
    return r;
}

WEAK int swscanf(const wchar_t* __restrict a, const wchar_t* __restrict b, ...) {
    va_list ap;
    va_start(ap, b);
    int r = __kernel_swscanf(a, b, ap);
    va_end(ap);
    return r;
}

WEAK int wprintf(const wchar_t* __restrict a, ...) {
    va_list ap;
    va_start(ap, a);
    int r = __kernel_wprintf(a, ap);
    va_end(ap);
    return r;
}

WEAK int wscanf(const wchar_t* __restrict a, ...) {
    va_list ap;
    va_start(ap, a);
    int r = __kernel_wscanf(a, ap);
    va_end(ap);
    return r;
}
