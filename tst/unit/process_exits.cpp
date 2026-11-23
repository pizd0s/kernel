//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "quick: 3 2 1\nfork: prepare parent child\nfork through a pointer: prepare parent child\n_Fork: none\nprocess exits: ok\n"

// The ways out and their neighbours: quick_exit (C11) runs the
// at_quick_exit functions, the last registered first, and _Exits with
// its status, no atexit function and no stdio flush; fork, a function as
// any other, runs pthread_atfork's handlers called through a pointer too;
// _Fork (POSIX.1-2024) is fork without them; brk always fails ENOMEM and
// sbrk changes nothing (Linux and musl's), sbrk(0) the break; issetugid
// is 0 for a process that changed no ids; posix_close closes as close
// and is EBADF for no descriptor; timespec_get (C11) is TIME_UTC's
// time, 0 for another base.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

namespace {
    void say(const char* text) {
        assert(write(1, text, strlen(text)) == (ssize_t)strlen(text));
    }

    void one() {
        say(" 1\n");
    }

    void two() {
        say(" 2");
    }

    void three() {
        say("quick: 3");
    }

    void never() {
        say("atexit ran\n");
    }

    char handlers[64];

    void prepare() {
        strcat(handlers, " prepare");
    }

    void parent() {
        strcat(handlers, " parent");
    }

    void child() {
        strcat(handlers, " child");
    }

    int wait(pid_t pid) {
        int status;
        assert(pid > 0 && waitpid(pid, &status, 0) == pid && WIFEXITED(status));
        return WEXITSTATUS(status);
    }
}

int main() {
    fflush(stdout);
    pid_t pid = fork();
    if (pid == 0) {
        assert(at_quick_exit(one) == 0 && at_quick_exit(two) == 0 && at_quick_exit(three) == 0);
        assert(atexit(never) == 0);
        printf("unflushed");
        quick_exit(5);
    }
    assert(wait(pid) == 5);

    assert(pthread_atfork(prepare, parent, child) == 0);
    pid = fork();
    if (pid == 0) {
        _exit(strcmp(handlers, " prepare child") == 0 ? 0 : 1);
    }
    assert(wait(pid) == 0 && strcmp(handlers, " prepare parent") == 0);
    printf("fork: prepare parent child\n");
    handlers[0] = 0;
    fflush(stdout);
    pid_t (*volatile through)(void) = fork;
    pid = through();
    if (pid == 0) {
        _exit(strcmp(handlers, " prepare child") == 0 ? 0 : 1);
    }
    assert(wait(pid) == 0 && strcmp(handlers, " prepare parent") == 0);
    printf("fork through a pointer: prepare parent child\n");
    handlers[0] = 0;
    fflush(stdout);
    pid = _Fork();
    if (pid == 0) {
        _exit(handlers[0] == 0 ? 0 : 1);
    }
    assert(wait(pid) == 0 && handlers[0] == 0);
    printf("_Fork: none\n");

    void* end = sbrk(0);
    assert(end != (void*)-1);
    errno = 0;
    assert(sbrk(4096) == (void*)-1 && errno == ENOMEM && sbrk(0) == end);
    errno = 0;
    assert(brk(end) == -1 && errno == ENOMEM);
    assert(issetugid() == 0);

    int fd = open(".", O_RDONLY);
    assert(fd >= 0 && posix_close(fd, 0) == 0 && fcntl(fd, F_GETFD) == -1 && errno == EBADF);
    errno = 0;
    assert(posix_close(fd, 0) == -1 && errno == EBADF);
    fd = open(".", O_RDONLY);
    assert(fd >= 0 && posix_close(fd, POSIX_CLOSE_RESTART) == 0 && fcntl(fd, F_GETFD) == -1);

    timespec now;
    timespec clock;
    assert(clock_gettime(CLOCK_REALTIME, &clock) == 0 && timespec_get(&now, TIME_UTC) == TIME_UTC);
    assert(now.tv_nsec >= 0 && now.tv_nsec < 1000000000 && now.tv_sec >= clock.tv_sec && now.tv_sec <= clock.tv_sec + 2);
    assert(timespec_get(&now, 0) == 0 && timespec_get(&now, TIME_UTC + 1) == 0);

    printf("process exits: ok\n");
}
