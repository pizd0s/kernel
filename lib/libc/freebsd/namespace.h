#pragma once

/*
 * FreeBSD's libc renames the functions it calls to their underscored
 * names, its own entry points, here and back in un-namespace.h, so its
 * headers declare those. Its sources call the underscored names: lib/c's
 * functions of the names without the underscore. And it locks no stream
 * and no list of streams, nor pushes a thread's cleanup handlers, in a
 * process of lib/c's, which has one thread.
 */
#include <fcntl.h>
#include <errno.h>
#include <pthread.h>
#include <stdarg.h>
#include <sys/resource.h>
#include <sys/stat.h>
#include <sys/uio.h>
#include <sys/wait.h>
#include <unistd.h>

// What a message of FreeBSD's begins with: the kernel's name of the
// program (lib/c's kernel.h).
const char* __kernel_name(void) __KERNEL(__kernel_name);

#define __isthreaded 0
#define _FLOCKFILE(fp) ((void)(fp))
#define _funlockfile(fp) ((void)(fp))
#define FLOCKFILE(fp) ((void)(fp))
#define FUNLOCKFILE(fp) ((void)(fp))
#define STDIO_THREAD_LOCK() ((void)0)
#define STDIO_THREAD_UNLOCK() ((void)0)

struct _pthread_cleanup_info {
    int unused;
};

#define ___pthread_cleanup_push_imp(routine, arg, info) ((void)(info))
#define ___pthread_cleanup_pop_imp(execute) ((void)0)

#define _pthread_mutex_lock(m) pthread_mutex_lock(m)
#define _pthread_once(once, init) pthread_once(once, init)
#define _pthread_key_create(key, destructor) pthread_key_create(key, destructor)
#define _pthread_getspecific(key) pthread_getspecific(key)
#define _pthread_setspecific(key, value) pthread_setspecific(key, value)
#define _pthread_mutex_unlock(m) pthread_mutex_unlock(m)

static inline const char* _getprogname(void) {
    return __kernel_name();
}

static inline int _close(int fd) {
    return close(fd);
}

static inline int _fcntl(int fd, int cmd, ...) {
    va_list ap;

    va_start(ap, cmd);
    long arg = va_arg(ap, long);
    va_end(ap);

    if (cmd == F_DUP2FD_CLOEXEC) {
        return dup3(fd, (int)arg, O_CLOEXEC);
    }

    return fcntl(fd, cmd, arg);
}

static inline int _open(const char* path, int flags, ...) {
    va_list ap;

    va_start(ap, flags);
    int mode = flags & O_CREAT ? va_arg(ap, int) : 0;
    va_end(ap);

    return open(path, flags, mode);
}

static inline int _openat(int dir, const char* path, int flags, ...) {
    va_list ap;

    va_start(ap, flags);
    int mode = flags & O_CREAT ? va_arg(ap, int) : 0;
    va_end(ap);

    return openat(dir, path, flags, mode);
}

static inline int _fstat(int fd, struct stat* st) {
    return fstat(fd, st);
}

static inline int _dup2(int from, int to) {
    return dup2(from, to);
}

static inline ssize_t _read(int fd, void* buf, size_t n) {
    return read(fd, buf, n);
}

static inline ssize_t _write(int fd, const void* buf, size_t n) {
    return write(fd, buf, n);
}

static inline ssize_t _writev(int fd, const struct iovec* iov, int n) {
    return writev(fd, iov, n);
}

static inline pid_t _wait4(pid_t pid, int* status, int options, struct rusage* usage) {
    return wait4(pid, status, options, usage);
}

static inline int _execve(const char* path, char* const argv[], char* const envp[]) {
    return execve(path, argv, envp);
}
