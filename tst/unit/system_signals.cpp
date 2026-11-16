//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "SIGINT from the command: handler ran 0\nSIGUSR1 from the command: handler ran 1, before the command ended 1\n"

// system, as POSIX has it: the caller ignores SIGINT while the command
// runs, so one the command sends it is gone, and takes its other signals
// at once, their handlers run while the command still runs.

#include <assert.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

static volatile sig_atomic_t interrupts;
static volatile sig_atomic_t others;
static volatile sig_atomic_t early;

static void onInterrupt(int) {
    interrupts = interrupts + 1;
}

// The command makes system-done as it ends.
static void onOther(int) {
    others = others + 1;
    early = access("system-done", F_OK) != 0;
}

int main() {
    assert(signal(SIGINT, onInterrupt) != SIG_ERR && signal(SIGUSR1, onOther) != SIG_ERR);
    unlink("system-done");
    assert(system("kill -INT $PPID") == 0);
    printf("SIGINT from the command: handler ran %d\n", int(interrupts));
    assert(system("kill -USR1 $PPID; sleep 1; : > system-done") == 0);
    printf("SIGUSR1 from the command: handler ran %d, before the command ended %d\n", int(others), int(early));
    unlink("system-done");
    return 0;
}
