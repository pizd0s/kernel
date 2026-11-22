//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "process spawning: ok\n"

// posix_spawn and posix_spawnp, as POSIX.1-2024 has them; the program
// spawns itself. The child gets argv and exactly envp. Its descriptors are
// the parent's, those marked FD_CLOEXEC closed, after the file actions in
// their order: open closes the descriptor first, a dup2 of a descriptor to
// itself clears its FD_CLOEXEC, a close of a closed one is no error. A
// relative path of the program is the child's after chdir and fchdir.
// posix_spawnp searches the parent's PATH and runs no shell for what is no
// program. The attributes: a signal mask replaces the parent's, default
// actions for the signals named, caught signals default, ignored ones
// stay; a process group, a session, the scheduling attributes. A failure
// is the error returned, and there is no child; the file actions reject
// negative descriptors. Three parts are lib/c's alone, as musl has none of
// them: the POSIX.1-2024 names addchdir and addfchdir, the EBADF of
// addopen and adddup2 for a descriptor past OPEN_MAX, and the scheduling
// attributes (musl's functions of them return ENOSYS).

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <sched.h>
#include <signal.h>
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

extern char** environ;

static void caught(int) {}

static bool handled(int sig, void (*handler)(int)) {
    struct sigaction act;
    return sigaction(sig, nullptr, &act) == 0 && act.sa_handler == handler;
}

static int child(int argc, char** argv) {
    const char* mode = argv[1];
    if (strcmp(mode, "args") == 0) {
        bool ok = argc == 3 && strcmp(argv[2], "two") == 0 && environ[0] && strcmp(environ[0], "SPAWNED=yes") == 0 && !environ[1];
        return ok ? 7 : 1;
    }
    if (strcmp(mode, "fds") == 0) {
        char buf[8];
        bool ok = read(0, buf, sizeof buf) == 3 && memcmp(buf, "in\n", 3) == 0 && read(10, buf, sizeof buf) == 0;
        ok = ok && fcntl(5, F_GETFD) == 0 && fcntl(7, F_GETFD) == 0;
        ok = ok && fcntl(6, F_GETFD) == -1 && errno == EBADF && fcntl(8, F_GETFD) == -1 && errno == EBADF;
        return ok && write(1, "out\n", 4) == 4 ? 0 : 1;
    }
    if (strcmp(mode, "cwd") == 0) {
        char here[PATH_MAX];
        return getcwd(here, sizeof here) && strcmp(here, argv[2]) == 0 ? 0 : 1;
    }
    if (strcmp(mode, "signals") == 0) {
        sigset_t mask;
        assert(sigprocmask(SIG_BLOCK, nullptr, &mask) == 0);
        bool ok = sigismember(&mask, SIGUSR1) && !sigismember(&mask, SIGQUIT);
        ok = ok && handled(SIGUSR2, SIG_DFL) && handled(SIGHUP, SIG_IGN) && handled(SIGTERM, SIG_DFL);
        return ok ? 0 : 1;
    }
    if (strcmp(mode, "inherited") == 0) {
        sigset_t mask;
        assert(sigprocmask(SIG_BLOCK, nullptr, &mask) == 0);
        bool ok = !sigismember(&mask, SIGUSR1) && sigismember(&mask, SIGQUIT);
        ok = ok && handled(SIGUSR2, SIG_IGN) && handled(SIGHUP, SIG_IGN) && handled(SIGTERM, SIG_DFL);
        return ok ? 0 : 1;
    }
    if (strcmp(mode, "group") == 0) {
        return getpgrp() == getpid() && getsid(0) == atoi(argv[2]) ? 0 : 1;
    }
    if (strcmp(mode, "session") == 0) {
        return getsid(0) == getpid() && getpgrp() == getpid() ? 0 : 1;
    }
    if (strcmp(mode, "same-group") == 0) {
        return getpgrp() == atoi(argv[2]) && geteuid() == getuid() && getegid() == getgid() ? 0 : 1;
    }
    return 99;
}

static int finished(pid_t pid) {
    int status;
    assert(waitpid(pid, &status, 0) == pid);
    return WIFEXITED(status) ? WEXITSTATUS(status) : 128 + WTERMSIG(status);
}

static void noChild() {
    int status;
    assert(waitpid(-1, &status, WNOHANG) == -1 && errno == ECHILD);
}

static int spawned(const char* path, const posix_spawn_file_actions_t* actions, const posix_spawnattr_t* attr, const char* mode,
                   const char* arg = nullptr, bool search = false) {
    char* argv[] = {const_cast<char*>(path), const_cast<char*>(mode), const_cast<char*>(arg), nullptr};
    pid_t pid = -1;
    int error = (search ? posix_spawnp : posix_spawn)(&pid, path, actions, attr, argv, environ);
    if (error != 0) {
        fprintf(stderr, "%s %s: %s\n", path, mode, strerror(error));
    }
    assert(error == 0 && pid > 0);
    return finished(pid);
}

static int failure(const char* path, const posix_spawn_file_actions_t* actions, const posix_spawnattr_t* attr, bool search = false) {
    char* argv[] = {const_cast<char*>(path), const_cast<char*>("args"), nullptr};
    int error = (search ? posix_spawnp : posix_spawn)(nullptr, path, actions, attr, argv, environ);
    noChild();
    return error;
}

static void make(const char* path, const char* text, mode_t mode) {
    int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, mode);
    assert(fd >= 0 && write(fd, text, strlen(text)) == ssize_t(strlen(text)) && close(fd) == 0);
}

int main(int argc, char** argv) {
    if (argc > 1) {
        return child(argc, argv);
    }
    char self[PATH_MAX];
    assert(realpath(argv[0], self) == self);
    char dir[PATH_MAX];
    strcpy(dir, self);
    *strrchr(dir, '/') = 0;
    const char* base = strrchr(self, '/') + 1;

    char work[] = "spawning-XXXXXX";
    assert(mkdtemp(work) == work && chdir(work) == 0);
    char here[PATH_MAX];
    assert(getcwd(here, sizeof here) == here);

    char* argv2[] = {self, const_cast<char*>("args"), const_cast<char*>("two"), nullptr};
    char* env[] = {const_cast<char*>("SPAWNED=yes"), nullptr};
    pid_t pid = -1;
    assert(posix_spawn(&pid, self, nullptr, nullptr, argv2, env) == 0 && pid > 0 && finished(pid) == 7);
    assert(posix_spawn(nullptr, self, nullptr, nullptr, argv2, env) == 0);
    int status;
    assert(wait(&status) > 0 && WIFEXITED(status) && WEXITSTATUS(status) == 7);

    make("spawned.in", "in\n", 0644);
    int pipes[2];
    assert(pipe(pipes) == 0);
    assert(dup2(pipes[0], 5) == 5 && fcntl(5, F_SETFD, FD_CLOEXEC) == 0);
    assert(dup2(pipes[0], 6) == 6 && fcntl(6, F_SETFD, FD_CLOEXEC) == 0);
    assert(dup2(pipes[1], 7) == 7 && dup2(pipes[1], 8) == 8 && dup2(pipes[1], 10) == 10);
    close(9);
    posix_spawn_file_actions_t actions;
    assert(posix_spawn_file_actions_init(&actions) == 0);
    assert(posix_spawn_file_actions_addopen(&actions, 1, "spawned.out", O_WRONLY | O_CREAT | O_TRUNC, 0644) == 0);
    assert(posix_spawn_file_actions_addopen(&actions, 10, "spawned.in", O_RDONLY, 0) == 0);
    assert(posix_spawn_file_actions_adddup2(&actions, 10, 0) == 0);
    assert(posix_spawn_file_actions_adddup2(&actions, 5, 5) == 0);
    assert(posix_spawn_file_actions_addclose(&actions, 8) == 0);
    assert(posix_spawn_file_actions_addclose(&actions, 9) == 0);
    assert(spawned(self, &actions, nullptr, "fds") == 0);
    assert(posix_spawn_file_actions_destroy(&actions) == 0);
    char out[8] = {};
    int fd = open("spawned.out", O_RDONLY);
    assert(fd >= 0 && read(fd, out, sizeof out) == 4 && strcmp(out, "out\n") == 0 && close(fd) == 0);
    assert(fcntl(5, F_GETFD) == FD_CLOEXEC && fcntl(8, F_GETFD) == 0);
    int opened[] = {5, 6, 7, 8, 10, pipes[0], pipes[1]};
    for (int i : opened) {
        assert(close(i) == 0);
    }

    char relative[PATH_MAX + 2];
    snprintf(relative, sizeof relative, "./%s", base);
    assert(posix_spawn_file_actions_init(&actions) == 0);
    assert(posix_spawn_file_actions_addchdir_np(&actions, dir) == 0);
    assert(spawned(relative, &actions, nullptr, "cwd", dir) == 0);
    assert(posix_spawn_file_actions_destroy(&actions) == 0);
    int dirfd = open(dir, O_RDONLY | O_DIRECTORY);
    assert(dirfd >= 0 && posix_spawn_file_actions_init(&actions) == 0);
    assert(posix_spawn_file_actions_addchdir_np(&actions, "/") == 0);
    assert(posix_spawn_file_actions_addfchdir_np(&actions, dirfd) == 0);
    assert(spawned(self, &actions, nullptr, "cwd", dir) == 0);
    assert(posix_spawn_file_actions_destroy(&actions) == 0 && close(dirfd) == 0);
    assert(spawned(self, nullptr, nullptr, "cwd", here) == 0);

    char* path = getenv("PATH");
    char* saved = path ? strdup(path) : nullptr;
    assert(setenv("PATH", dir, 1) == 0);
    assert(spawned(base, nullptr, nullptr, "cwd", here, true) == 0);
    make("plain", "not a program\n", 0755);
    assert(setenv("PATH", here, 1) == 0);
    assert(failure("plain", nullptr, nullptr, true) == ENOEXEC);
    assert(failure("./plain", nullptr, nullptr, true) == ENOEXEC);
    assert(failure("./plain", nullptr, nullptr) == ENOEXEC);
    assert(failure("no-such-program", nullptr, nullptr, true) == ENOENT);
    assert(saved ? setenv("PATH", saved, 1) == 0 : unsetenv("PATH") == 0);
    free(saved);
    assert(failure("./no-such-program", nullptr, nullptr) == ENOENT);

    assert(posix_spawn_file_actions_init(&actions) == 0);
    assert(posix_spawn_file_actions_addopen(&actions, 3, "missing/file", O_RDONLY, 0) == 0);
    assert(failure(self, &actions, nullptr) == ENOENT);
    assert(posix_spawn_file_actions_destroy(&actions) == 0 && posix_spawn_file_actions_init(&actions) == 0);
    assert(posix_spawn_file_actions_adddup2(&actions, 40, 3) == 0);
    assert(failure(self, &actions, nullptr) == EBADF);
    assert(posix_spawn_file_actions_destroy(&actions) == 0 && posix_spawn_file_actions_init(&actions) == 0);
    assert(posix_spawn_file_actions_addchdir_np(&actions, "missing") == 0);
    assert(failure(self, &actions, nullptr) == ENOENT);
    assert(posix_spawn_file_actions_addopen(&actions, -1, "x", O_RDONLY, 0) == EBADF);
    assert(posix_spawn_file_actions_addclose(&actions, -1) == EBADF);
    assert(posix_spawn_file_actions_adddup2(&actions, -1, 0) == EBADF && posix_spawn_file_actions_adddup2(&actions, 0, -1) == EBADF);
    assert(posix_spawn_file_actions_addfchdir_np(&actions, -1) == EBADF);
    assert(posix_spawn_file_actions_destroy(&actions) == 0);

    posix_spawnattr_t attr;
    assert(posix_spawnattr_init(&attr) == 0);
    short flags = -1;
    pid_t group = -1;
    sigset_t set;
    assert(posix_spawnattr_getflags(&attr, &flags) == 0 && flags == 0);
    assert(posix_spawnattr_getpgroup(&attr, &group) == 0 && group == 0);
    assert(posix_spawnattr_setflags(&attr, 0x4000) == EINVAL);
    assert(posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETSIGMASK | POSIX_SPAWN_SETSIGDEF) == 0);
    assert(posix_spawnattr_getflags(&attr, &flags) == 0 && flags == (POSIX_SPAWN_SETSIGMASK | POSIX_SPAWN_SETSIGDEF));
    assert(posix_spawnattr_setpgroup(&attr, 42) == 0 && posix_spawnattr_getpgroup(&attr, &group) == 0 && group == 42);
    sigemptyset(&set);
    sigaddset(&set, SIGUSR1);
    assert(posix_spawnattr_setsigmask(&attr, &set) == 0);
    sigemptyset(&set);
    sigaddset(&set, SIGUSR2);
    assert(posix_spawnattr_setsigdefault(&attr, &set) == 0);
    sigemptyset(&set);
    assert(posix_spawnattr_getsigmask(&attr, &set) == 0 && sigismember(&set, SIGUSR1) && !sigismember(&set, SIGUSR2));
    assert(posix_spawnattr_getsigdefault(&attr, &set) == 0 && sigismember(&set, SIGUSR2) && !sigismember(&set, SIGUSR1));

    signal(SIGUSR2, SIG_IGN);
    signal(SIGHUP, SIG_IGN);
    signal(SIGTERM, caught);
    sigset_t blocked, old;
    sigemptyset(&blocked);
    sigaddset(&blocked, SIGQUIT);
    assert(sigprocmask(SIG_BLOCK, &blocked, &old) == 0);
    assert(spawned(self, nullptr, &attr, "signals") == 0);
    assert(spawned(self, nullptr, nullptr, "inherited") == 0);
    assert(sigprocmask(SIG_SETMASK, &old, nullptr) == 0);
    signal(SIGUSR2, SIG_DFL);
    signal(SIGHUP, SIG_DFL);
    signal(SIGTERM, SIG_DFL);

    char number[16];
    snprintf(number, sizeof number, "%d", getsid(0));
    assert(posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETPGROUP) == 0 && posix_spawnattr_setpgroup(&attr, 0) == 0);
    assert(spawned(self, nullptr, &attr, "group", number) == 0);
    snprintf(number, sizeof number, "%d", getpgrp());
    assert(posix_spawnattr_setpgroup(&attr, getpgrp()) == 0);
    assert(spawned(self, nullptr, &attr, "same-group", number) == 0);
    assert(posix_spawnattr_setflags(&attr, POSIX_SPAWN_RESETIDS) == 0);
    assert(spawned(self, nullptr, &attr, "same-group", number) == 0);
    assert(spawned(self, nullptr, nullptr, "same-group", number) == 0);
    assert(posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETSID) == 0);
    assert(spawned(self, nullptr, &attr, "session") == 0);
    assert(posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETPGROUP) == 0 && posix_spawnattr_setpgroup(&attr, INT_MAX) == 0);
    assert(failure(self, nullptr, &attr) == EPERM);


#ifdef __wasm__
    int policy = -1;
    struct sched_param param = {5};
    assert(posix_spawnattr_setschedpolicy(&attr, SCHED_OTHER) == 0 && posix_spawnattr_getschedpolicy(&attr, &policy) == 0);
    assert(policy == SCHED_OTHER);
    assert(posix_spawnattr_setschedparam(&attr, &param) == 0);
    param.sched_priority = 0;
    assert(posix_spawnattr_getschedparam(&attr, &param) == 0 && param.sched_priority == 5);
    param.sched_priority = 0;
    assert(posix_spawnattr_setschedparam(&attr, &param) == 0);
    assert(posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETSCHEDPARAM) == 0);
    assert(spawned(self, nullptr, &attr, "same-group", number) == 0);
    assert(posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETSCHEDULER) == 0);
    assert(spawned(self, nullptr, &attr, "same-group", number) == 0);
    param.sched_priority = 1;
    assert(posix_spawnattr_setschedparam(&attr, &param) == 0);
    assert(failure(self, nullptr, &attr) == EINVAL);
    assert(posix_spawnattr_setflags(&attr, POSIX_SPAWN_SETSCHEDPARAM) == 0);
    assert(failure(self, nullptr, &attr) == EINVAL);
    long limit = sysconf(_SC_OPEN_MAX);
    assert(limit > 10 && posix_spawn_file_actions_init(&actions) == 0);
    assert(posix_spawn_file_actions_addopen(&actions, int(limit), "x", O_RDONLY, 0) == EBADF);
    assert(posix_spawn_file_actions_adddup2(&actions, 0, int(limit)) == EBADF);
    assert(posix_spawn_file_actions_adddup2(&actions, int(limit), 0) == EBADF);
    assert(posix_spawn_file_actions_addchdir(&actions, dir) == 0);
    assert(spawned(relative, &actions, nullptr, "cwd", dir) == 0);
    assert(posix_spawn_file_actions_destroy(&actions) == 0);
    dirfd = open(dir, O_RDONLY | O_DIRECTORY);
    assert(dirfd >= 0 && posix_spawn_file_actions_init(&actions) == 0);
    assert(posix_spawn_file_actions_addfchdir(&actions, dirfd) == 0 && posix_spawn_file_actions_addfchdir(&actions, -1) == EBADF);
    assert(spawned(self, &actions, nullptr, "cwd", dir) == 0);
    assert(posix_spawn_file_actions_destroy(&actions) == 0 && close(dirfd) == 0);
#endif
    assert(posix_spawnattr_destroy(&attr) == 0);

    noChild();
    printf("process spawning: ok\n");
}
