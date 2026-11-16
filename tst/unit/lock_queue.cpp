//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "given in the order asked: 1234\na shared lock behind an exclusive one that waits: waited 1, tried 1, then WR\na waiter that ended: the next has the lock 1\na handler that restarts: ran 1, then the lock 1\na range that waits: 1\n"

// A lock that waits is a request its file system keeps: those that wait
// have the lock in the order they asked, a shared one behind an
// exclusive one that waits too, and one tried with LOCK_NB is not taken
// past it; one whose process ended is in nobody's way; a handler that
// restarts calls leaves its call waiting; and a range of fcntl's waits
// the same.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <sys/file.h>
#include <sys/wait.h>
#include <unistd.h>

static const char* const path = "lock-queue.data";

// How long a child is given to get from saying it is about to ask to
// waiting at the file system.
static void settle() {
    usleep(100000);
}

static int told[2];

static void tell(char c) {
    assert(write(told[1], &c, 1) == 1);
}

static char heard() {
    char c;
    assert(read(told[0], &c, 1) == 1);
    return c;
}

// A child that says it is there, takes flock's lock, waiting for it,
// says what it is, and lets the lock go a little later.
static pid_t locker(int op, char name, void (*before)()) {
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        const int fd = open(path, O_RDWR);
        assert(fd >= 0);
        if (before) {
            before();
        }
        tell('.');
        assert(flock(fd, op) == 0);
        tell(name);
        usleep(20000);
        assert(flock(fd, LOCK_UN) == 0);
        _exit(0);
    }
    assert(heard() == '.');
    settle();
    return child;
}

static void reap(pid_t child) {
    int status;
    assert(waitpid(child, &status, 0) == child);
}

static void handler(int) {
    tell('h');
}

static void restarts() {
    struct sigaction action{};
    action.sa_handler = handler;
    action.sa_flags = SA_RESTART;
    assert(sigaction(SIGUSR1, &action, nullptr) == 0);
}

int main() {
    alarm(60);
    const int fd = open(path, O_CREAT | O_TRUNC | O_RDWR, 0600);
    assert(fd >= 0 && ftruncate(fd, 16) == 0 && pipe(told) == 0);

    // Four wait for the lock this process holds; they have it as they
    // asked.
    assert(flock(fd, LOCK_EX) == 0);
    pid_t children[4];
    for (int i = 0; i < 4; ++i) {
        children[i] = locker(LOCK_EX, char('1' + i), nullptr);
    }
    assert(flock(fd, LOCK_UN) == 0);
    char order[5] = {};
    for (int i = 0; i < 4; ++i) {
        order[i] = heard();
    }
    for (const pid_t child : children) {
        reap(child);
    }
    printf("given in the order asked: %s\n", order);

    // A shared lock, free by the locks held, waits behind an exclusive
    // one that waits, and is not there to try.
    assert(flock(fd, LOCK_SH) == 0);
    const pid_t writer = locker(LOCK_EX, 'W', nullptr);
    const pid_t reader = locker(LOCK_SH, 'R', nullptr);
    pollfd quiet{told[0], POLLIN, 0};
    const int waited = poll(&quiet, 1, 100) == 0;
    const int another = open(path, O_RDWR);
    assert(another >= 0);
    const int tried = flock(another, LOCK_SH | LOCK_NB) == -1 && errno == EAGAIN;
    assert(close(another) == 0);
    assert(flock(fd, LOCK_UN) == 0);
    order[0] = heard();
    order[1] = heard();
    order[2] = 0;
    reap(writer);
    reap(reader);
    printf("a shared lock behind an exclusive one that waits: waited %d, tried %d, then %s\n", waited, tried, order);

    // The first that waits ends; the second has the lock.
    assert(flock(fd, LOCK_EX) == 0);
    const pid_t first = locker(LOCK_EX, 'a', nullptr);
    const pid_t second = locker(LOCK_EX, 'b', nullptr);
    assert(kill(first, SIGKILL) == 0);
    reap(first);
    assert(flock(fd, LOCK_UN) == 0);
    const char got = heard();
    reap(second);
    printf("a waiter that ended: the next has the lock %d\n", got == 'b');

    // A handler that restarts calls runs, and the call waits on.
    assert(flock(fd, LOCK_EX) == 0);
    const pid_t patient = locker(LOCK_EX, 'g', restarts);
    assert(kill(patient, SIGUSR1) == 0);
    const int ran = heard() == 'h';
    settle();
    assert(flock(fd, LOCK_UN) == 0);
    const int locked = heard() == 'g';
    reap(patient);
    printf("a handler that restarts: ran %d, then the lock %d\n", ran, locked);

    // fcntl's lock of a range waits the same.
    struct flock range{};
    range.l_type = F_WRLCK;
    range.l_whence = SEEK_SET;
    range.l_len = 4;
    assert(fcntl(fd, F_SETLK, &range) == 0);
    fflush(stdout);
    const pid_t ranged = fork();
    assert(ranged >= 0);
    if (ranged == 0) {
        const int other = open(path, O_RDWR);
        assert(other >= 0);
        tell('.');
        assert(fcntl(other, F_SETLKW, &range) == 0);
        tell('r');
        _exit(0);
    }
    assert(heard() == '.');
    settle();
    range.l_type = F_UNLCK;
    assert(fcntl(fd, F_SETLK, &range) == 0);
    const int after = heard() == 'r';
    reap(ranged);
    printf("a range that waits: %d\n", after);
    assert(close(fd) == 0 && unlink(path) == 0);
    return 0;
}
