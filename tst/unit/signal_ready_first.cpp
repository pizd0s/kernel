//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "ppoll: 1 ready, handler ran 0, still pending 1\n"

// A wait finds what is ready before it looks at its signals: ppoll,
// whose mask lets a pending signal through, returns the descriptor that
// is ready, and the signal stays pending under the mask it restores, its
// handler not run.

#include <assert.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <unistd.h>

static volatile sig_atomic_t ran;

static void handler(int) {
    ran = 1;
}

int main() {
    assert(signal(SIGUSR1, handler) != SIG_ERR);
    sigset_t block;
    sigemptyset(&block);
    sigaddset(&block, SIGUSR1);
    assert(sigprocmask(SIG_BLOCK, &block, nullptr) == 0);
    assert(raise(SIGUSR1) == 0);
    int ends[2];
    assert(pipe(ends) == 0 && write(ends[1], "x", 1) == 1);
    pollfd fd{ends[0], POLLIN, 0};
    sigset_t none;
    sigemptyset(&none);
    const int ready = ppoll(&fd, 1, nullptr, &none);
    sigset_t pending;
    assert(sigpending(&pending) == 0);
    printf("ppoll: %d ready, handler ran %d, still pending %d\n", ready, int(ran), sigismember(&pending, SIGUSR1));
    return 0;
}
