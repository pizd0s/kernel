/*
 * execvp, execvpe, execlp and posix_spawnp: a file of no slash is looked
 * for in each directory of PATH, an empty one the working directory, and
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

// execlp takes its va_list, as a program's forwarder passes it.
int __kernel_execlp(const char* file, const char* arg0, va_list args) {
    va_list counted;
    size_t n = arg0 != NULL;

    va_copy(counted, args);

    while (n && va_arg(counted, char*)) {
        n++;
    }

    va_end(counted);

    char* argv[n + 1];

    for (size_t i = 0; i < n; i++) {
        argv[i] = i ? va_arg(args, char*) : (char*)arg0;
    }

    argv[n] = NULL;

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
