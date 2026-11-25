/*
 * execv, execl and execle are execve's, of environ or of execle's own
 * environment; execvp, execvpe, execlp and posix_spawnp: a file of no
 * slash is looked for in each directory of PATH, an empty one the working directory, and
 * of /usr/local/bin:/bin:/usr/bin where PATH is unset. One that is not
 * there, ENOENT or ENOTDIR, is passed over, and so is one that may not be
 * run, EACCES, which is the error where none is found; any other error
 * ends the search. They use no heap, so that a child of vfork can call
 * them.
 */
#include "kernel.h"
#include "sized.h"

#include <errno.h>
#include <spawn.h>
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int searched(const char* file, int (*attempt)(const char* path, void* data), void* data) {
    if (!*file) {
        return ENOENT;
    }

    if (strchr(file, '/')) {
        return attempt(file, data);
    }

    const char* path = getenv("PATH");
    size_t n = strlen(file);
    int e = ENOENT;

    path = path ? path : "/usr/local/bin:/bin:/usr/bin";

    for (const char* dir = path;; dir++) {
        size_t length = strcspn(dir, ":");
        size_t at = length ? length : 1;
        char full[at + 1 + n + 1];

        memcpy(full, length ? dir : ".", at);
        full[at] = '/';
        memcpy(full + at + 1, file, n + 1);

        int r = attempt(full, data);

        if (r == EACCES) {
            e = EACCES;
        } else if (r != ENOENT && r != ENOTDIR) {
            return r;
        }

        dir += length;

        if (!*dir) {
            return e;
        }
    }
}

struct Exec {
    char* const* argv;
    char* const* envp;
};

static int executed(const char* path, void* data) {
    const struct Exec* x = data;

    execve(path, x->argv, x->envp);

    return errno;
}

int __kernel_execvpe(const char* file, char* const argv[], char* const envp[]) {
    struct Exec x = {argv, envp};

    errno = searched(file, executed, &x);

    return -1;
}

int __kernel_execvp(const char* file, char* const argv[]) {
    return __kernel_execvpe(file, argv, *__program->environAt);
}

int __kernel_execv(const char* path, char* const argv[]) {
    return execve(path, argv, *__program->environAt);
}

// The count of the arguments from arg0 to the null of a va_list.
static size_t arguments(const char* arg0, va_list args) {
    va_list counted;
    size_t n = arg0 != NULL;

    va_copy(counted, args);

    while (n && va_arg(counted, char*)) {
        n++;
    }

    va_end(counted);

    return n;
}

// The n arguments in argv, then its null; args is past their null after.
static void collect(char** argv, size_t n, const char* arg0, va_list* args) {
    for (size_t i = 1; i < n; i++) {
        argv[i] = va_arg(*args, char*);
    }

    if (n) {
        argv[0] = (char*)arg0;
        va_arg(*args, char*);
    }

    argv[n] = NULL;
}

// execl, execle and execlp take their va_list, as a program's forwarder
// passes it.
int __kernel_execl(const char* path, const char* arg0, va_list args) {
    char* argv[arguments(arg0, args) + 1];

    collect(argv, sizeof argv / sizeof argv[0] - 1, arg0, &args);

    return execve(path, argv, *__program->environAt);
}

int __kernel_execle(const char* path, const char* arg0, va_list args) {
    char* argv[arguments(arg0, args) + 1];

    collect(argv, sizeof argv / sizeof argv[0] - 1, arg0, &args);

    return execve(path, argv, va_arg(args, char* const*));
}

int __kernel_execlp(const char* file, const char* arg0, va_list args) {
    char* argv[arguments(arg0, args) + 1];

    collect(argv, sizeof argv / sizeof argv[0] - 1, arg0, &args);

    return __kernel_execvp(file, argv);
}

struct Spawn {
    pid_t* pid;
    const posix_spawn_file_actions_t* actions;
    const posix_spawnattr_t* attr;
    char* const* argv;
    char* const* envp;
};

static int spawned(const char* path, void* data) {
    const struct Spawn* s = data;

    return posix_spawn(s->pid, path, s->actions, s->attr, s->argv, s->envp);
}

int __kernel_posix_spawnp(pid_t* restrict pid, const char* restrict file, const posix_spawn_file_actions_t* actions, size_t actionsSize,
                          const posix_spawnattr_t* restrict attr, size_t attrSize, char* const argv[], char* const envp[]) {
    sized("posix_spawnp", actionsSize, sizeof(posix_spawn_file_actions_t));
    sized("posix_spawnp", attrSize, sizeof(posix_spawnattr_t));

    struct Spawn s = {pid, actions, attr, argv, envp};

    return searched(file, spawned, &s);
}
