/*
 * posix_spawn, as POSIX.1-2024 has it, over vfork: the child, in its
 * parent's memory while the parent waits, does what the attributes and
 * the file actions ask, in their order, then runs the program (execve);
 * of a step that fails it ends, 127, its error the parent's to return,
 * the child reaped. All signals are blocked meanwhile, and the child's
 * handlers are the defaults before it unblocks any, so that none runs in
 * the parent's memory; the mask the child runs with is the attributes'
 * or the parent's. The file actions are a list of the heap the object
 * points to; the child takes no heap.
 */
#include "sized.h"

#include <errno.h>
#include <fcntl.h>
#include <sched.h>
#include <signal.h>
#include <spawn.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/wait.h>
#include <unistd.h>

enum { actionOpen, actionClose, actionDup2, actionChdir, actionFchdir };

struct Action {
    int kind;
    int fd;
    int to;
    int flags;
    mode_t mode;
    char* path;
};

struct Actions {
    size_t count;
    size_t room;
    struct Action list[];
};

_Static_assert(sizeof(int) == sizeof(struct Actions*), "the object holds the list's address");

static struct Actions* listOf(const posix_spawn_file_actions_t* a) {
    return (struct Actions*)(uintptr_t)(unsigned)a->__actions;
}

// A descriptor an action may name: one below the process's limit.
static int named(int fd) {
    struct rlimit r;

    return fd >= 0 && (getrlimit(RLIMIT_NOFILE, &r) != 0 || r.rlim_cur == RLIM_INFINITY || (rlim_t)fd < r.rlim_cur);
}

// An action at the list's end: 0, or the errno.
static int added(posix_spawn_file_actions_t* a, struct Action x) {
    struct Actions* l = listOf(a);

    if (!l || l->count == l->room) {
        size_t room = l ? 2 * l->room : 4;
        struct Actions* more = realloc(l, sizeof *l + room * sizeof l->list[0]);

        if (!more) {
            free(x.path);

            return ENOMEM;
        }

        if (!l) {
            more->count = 0;
        }

        more->room = room;
        l = more;
        a->__actions = (int)(uintptr_t)l;
    }

    l->list[l->count++] = x;

    return 0;
}

int __kernel_posix_spawn_file_actions_init(posix_spawn_file_actions_t* a, size_t size) {
    sized("posix_spawn_file_actions_init", size, sizeof *a);
    a->__actions = 0;

    return 0;
}

int __kernel_posix_spawn_file_actions_destroy(posix_spawn_file_actions_t* a, size_t size) {
    sized("posix_spawn_file_actions_destroy", size, sizeof *a);

    struct Actions* l = listOf(a);

    for (size_t i = 0; l && i < l->count; i++) {
        free(l->list[i].path);
    }

    free(l);
    a->__actions = 0;

    return 0;
}

int __kernel_posix_spawn_file_actions_addopen(posix_spawn_file_actions_t* restrict a, size_t size, int fd, const char* restrict path,
                                              int flags, mode_t mode) {
    sized("posix_spawn_file_actions_addopen", size, sizeof *a);

    if (!named(fd)) {
        return EBADF;
    }

    char* copy = strdup(path);

    return copy ? added(a, (struct Action){actionOpen, fd, 0, flags, mode, copy}) : ENOMEM;
}

int __kernel_posix_spawn_file_actions_addclose(posix_spawn_file_actions_t* a, size_t size, int fd) {
    sized("posix_spawn_file_actions_addclose", size, sizeof *a);

    return fd < 0 ? EBADF : added(a, (struct Action){actionClose, fd, 0, 0, 0, NULL});
}

int __kernel_posix_spawn_file_actions_adddup2(posix_spawn_file_actions_t* a, size_t size, int fd, int to) {
    sized("posix_spawn_file_actions_adddup2", size, sizeof *a);

    return !named(fd) || !named(to) ? EBADF : added(a, (struct Action){actionDup2, fd, to, 0, 0, NULL});
}

int __kernel_posix_spawn_file_actions_addchdir(posix_spawn_file_actions_t* restrict a, size_t size, const char* restrict path) {
    sized("posix_spawn_file_actions_addchdir", size, sizeof *a);

    char* copy = strdup(path);

    return copy ? added(a, (struct Action){actionChdir, 0, 0, 0, 0, copy}) : ENOMEM;
}

int __kernel_posix_spawn_file_actions_addfchdir(posix_spawn_file_actions_t* a, size_t size, int fd) {
    sized("posix_spawn_file_actions_addfchdir", size, sizeof *a);

    return fd < 0 ? EBADF : added(a, (struct Action){actionFchdir, fd, 0, 0, 0, NULL});
}

int __kernel_posix_spawn_file_actions_addchdir_np(posix_spawn_file_actions_t* restrict a, size_t size, const char* restrict path) {
    return __kernel_posix_spawn_file_actions_addchdir(a, size, path);
}

int __kernel_posix_spawn_file_actions_addfchdir_np(posix_spawn_file_actions_t* a, size_t size, int fd) {
    return __kernel_posix_spawn_file_actions_addfchdir(a, size, fd);
}

int __kernel_posix_spawnattr_init(posix_spawnattr_t* a, size_t size) {
    sized("posix_spawnattr_init", size, sizeof *a);
    memset(a, 0, sizeof *a);
    a->__policy = SCHED_OTHER;

    return 0;
}

int __kernel_posix_spawnattr_destroy(posix_spawnattr_t* a, size_t size) {
    sized("posix_spawnattr_destroy", size, sizeof *a);

    return 0;
}

int __kernel_posix_spawnattr_getflags(const posix_spawnattr_t* restrict a, size_t size, short* restrict flags) {
    sized("posix_spawnattr_getflags", size, sizeof *a);
    *flags = (short)a->__flags;

    return 0;
}

int __kernel_posix_spawnattr_setflags(posix_spawnattr_t* a, size_t size, short flags) {
    sized("posix_spawnattr_setflags", size, sizeof *a);

    const int all = POSIX_SPAWN_RESETIDS | POSIX_SPAWN_SETPGROUP | POSIX_SPAWN_SETSIGDEF | POSIX_SPAWN_SETSIGMASK |
                    POSIX_SPAWN_SETSCHEDPARAM | POSIX_SPAWN_SETSCHEDULER | POSIX_SPAWN_SETSID;

    if ((unsigned short)flags & ~all) {
        return EINVAL;
    }

    a->__flags = (unsigned short)flags;

    return 0;
}

int __kernel_posix_spawnattr_getpgroup(const posix_spawnattr_t* restrict a, size_t size, pid_t* restrict pgroup) {
    sized("posix_spawnattr_getpgroup", size, sizeof *a);
    *pgroup = a->__pgroup;

    return 0;
}

int __kernel_posix_spawnattr_setpgroup(posix_spawnattr_t* a, size_t size, pid_t pgroup) {
    sized("posix_spawnattr_setpgroup", size, sizeof *a);
    a->__pgroup = pgroup;

    return 0;
}

int __kernel_posix_spawnattr_getsigdefault(const posix_spawnattr_t* restrict a, size_t size, sigset_t* restrict set, size_t setSize) {
    sized("posix_spawnattr_getsigdefault", size, sizeof *a);
    sized("posix_spawnattr_getsigdefault", setSize, sizeof *set);
    *set = a->__sigdefault;

    return 0;
}

int __kernel_posix_spawnattr_setsigdefault(posix_spawnattr_t* restrict a, size_t size, const sigset_t* restrict set, size_t setSize) {
    sized("posix_spawnattr_setsigdefault", size, sizeof *a);
    sized("posix_spawnattr_setsigdefault", setSize, sizeof *set);
    a->__sigdefault = *set;

    return 0;
}

int __kernel_posix_spawnattr_getsigmask(const posix_spawnattr_t* restrict a, size_t size, sigset_t* restrict set, size_t setSize) {
    sized("posix_spawnattr_getsigmask", size, sizeof *a);
    sized("posix_spawnattr_getsigmask", setSize, sizeof *set);
    *set = a->__sigmask;

    return 0;
}

int __kernel_posix_spawnattr_setsigmask(posix_spawnattr_t* restrict a, size_t size, const sigset_t* restrict set, size_t setSize) {
    sized("posix_spawnattr_setsigmask", size, sizeof *a);
    sized("posix_spawnattr_setsigmask", setSize, sizeof *set);
    a->__sigmask = *set;

    return 0;
}

int __kernel_posix_spawnattr_getschedpolicy(const posix_spawnattr_t* restrict a, size_t size, int* restrict policy) {
    sized("posix_spawnattr_getschedpolicy", size, sizeof *a);
    *policy = a->__policy;

    return 0;
}

int __kernel_posix_spawnattr_setschedpolicy(posix_spawnattr_t* a, size_t size, int policy) {
    sized("posix_spawnattr_setschedpolicy", size, sizeof *a);
    a->__policy = policy;

    return 0;
}

int __kernel_posix_spawnattr_getschedparam(const posix_spawnattr_t* restrict a, size_t size, struct sched_param* restrict param,
                                           size_t paramSize) {
    sized("posix_spawnattr_getschedparam", size, sizeof *a);
    sized("posix_spawnattr_getschedparam", paramSize, sizeof *param);
    *param = a->__param;

    return 0;
}

int __kernel_posix_spawnattr_setschedparam(posix_spawnattr_t* restrict a, size_t size, const struct sched_param* restrict param,
                                           size_t paramSize) {
    sized("posix_spawnattr_setschedparam", size, sizeof *a);
    sized("posix_spawnattr_setschedparam", paramSize, sizeof *param);
    a->__param = *param;

    return 0;
}

// An action, the child's: 0, or the errno.
static int act(const struct Action* x) {
    switch (x->kind) {
        case actionOpen: {
            close(x->fd);

            int f = open(x->path, x->flags & ~O_CLOEXEC, x->mode);

            if (f < 0) {
                return errno;
            }

            if (f != x->fd) {
                int r = dup2(f, x->fd);

                close(f);

                if (r < 0) {
                    return errno;
                }
            }

            return x->flags & O_CLOEXEC && fcntl(x->fd, F_SETFD, FD_CLOEXEC) != 0 ? errno : 0;
        }
        case actionClose:
            return close(x->fd) != 0 && errno == EBADF && !named(x->fd) ? EBADF : 0;
        case actionDup2:
            if (x->fd != x->to) {
                return dup2(x->fd, x->to) < 0 ? errno : 0;
            }

            return fcntl(x->fd, F_SETFD, 0) != 0 ? errno : 0;
        case actionChdir:
            return chdir(x->path) != 0 ? errno : 0;
        default:
            return fchdir(x->fd) != 0 ? errno : 0;
    }
}

// The attributes, the child's: 0, or the errno.
static int attributes(const posix_spawnattr_t* a) {
    int flags = a->__flags;

    if (flags & POSIX_SPAWN_SETSID) {
        if (setsid() < 0) {
            return errno;
        }
    } else if (flags & POSIX_SPAWN_SETPGROUP) {
        if (setpgid(0, a->__pgroup) != 0) {
            return errno == ESRCH ? EPERM : errno;
        }
    }

    if (flags & POSIX_SPAWN_RESETIDS && (setegid(getgid()) != 0 || seteuid(getuid()) != 0)) {
        return errno;
    }

    if (flags & POSIX_SPAWN_SETSCHEDULER) {
        return sched_setscheduler(0, a->__policy, &a->__param) < 0 ? errno : 0;
    }

    if (flags & POSIX_SPAWN_SETSCHEDPARAM && a->__param.sched_priority != 0) {
        return EINVAL;
    }

    return 0;
}

int __kernel_posix_spawn(pid_t* restrict pid, const char* restrict path, const posix_spawn_file_actions_t* actions, size_t actionsSize,
                         const posix_spawnattr_t* restrict attr, size_t attrSize, char* const argv[], char* const envp[]) {
    sized("posix_spawn", actionsSize, sizeof *actions);
    sized("posix_spawn", attrSize, sizeof *attr);

    const struct Actions* list = actions ? listOf(actions) : NULL;
    sigset_t all;
    sigset_t old;
    volatile int error = 0;

    sigfillset(&all);
    pthread_sigmask(SIG_BLOCK, &all, &old);

    pid_t child = vfork();

    if (child == 0) {
        int flags = attr ? attr->__flags : 0;

        for (int sig = 1; sig < NSIG; sig++) {
            struct sigaction now;

            if (sigaction(sig, NULL, &now) == 0 && ((flags & POSIX_SPAWN_SETSIGDEF && sigismember(&attr->__sigdefault, sig) == 1) ||
                                                    (now.sa_handler != SIG_IGN && now.sa_handler != SIG_DFL))) {
                signal(sig, SIG_DFL);
            }
        }

        int e = attr ? attributes(attr) : 0;

        for (size_t i = 0; e == 0 && list && i < list->count; i++) {
            e = act(&list->list[i]);
        }

        if (e == 0) {
            sigprocmask(SIG_SETMASK, flags & POSIX_SPAWN_SETSIGMASK ? &attr->__sigmask : &old, NULL);
            execve(path, argv, envp);
            e = errno;
        }

        error = e;
        _exit(127);
    }

    int e = child < 0 ? errno : error;

    pthread_sigmask(SIG_SETMASK, &old, NULL);

    if (child > 0 && e != 0) {
        waitpid(child, NULL, 0);
    }

    if (e == 0 && pid) {
        *pid = child;
    }

    return e;
}
