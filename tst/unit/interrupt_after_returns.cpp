//@ compile-flags: -O2
//@ stdout: "interrupt after returns: ok\n"

// The interruption of a signal can find the code just after a call of the
// kernel returned, while the code reads its memory's base again. A child
// calls the kernel in a loop while its parent signals it thousands of
// times; it must take the signals there and live.

#include <assert.h>
#include <signal.h>
#include <stdio.h>
#include <sys/wait.h>
#include <unistd.h>

static volatile sig_atomic_t taken;
static volatile sig_atomic_t stop;

static void take(int) {
    taken = taken + 1;
}

static void finish(int) {
    stop = 1;
}

int main() {
    int ready[2];
    assert(pipe(ready) == 0);
    pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        struct sigaction action {};
        action.sa_handler = take;
        assert(sigaction(SIGUSR1, &action, nullptr) == 0);
        action.sa_handler = finish;
        assert(sigaction(SIGUSR2, &action, nullptr) == 0);
        assert(write(ready[1], "r", 1) == 1);
        while (!stop) {
            getpid();
        }
        _exit(taken > 0 ? 0 : 1);
    }
    char byte;
    assert(read(ready[0], &byte, 1) == 1 && byte == 'r');
    for (int i = 0; i < 1000000; ++i) {
        assert(kill(child, SIGUSR1) == 0);
    }
    assert(kill(child, SIGUSR2) == 0);
    int status;
    assert(waitpid(child, &status, 0) == child);
    assert(WIFEXITED(status) && WEXITSTATUS(status) == 0);
    assert(close(ready[0]) == 0 && close(ready[1]) == 0);
    puts("interrupt after returns: ok");
}
