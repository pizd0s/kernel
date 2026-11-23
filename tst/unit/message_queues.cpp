//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "message queues: ok\n"

// POSIX message queues, as POSIX.1-2024 has them: a queue of a name with
// its attributes, created once (EEXIST with O_EXCL), its messages received
// highest priority first and in order within one; EMSGSIZE for a message
// past mq_msgsize or a buffer short of it; a full queue makes a sender wait,
// an empty one a receiver, EAGAIN with O_NONBLOCK (mq_setattr's) and
// ETIMEDOUT past the time given; descriptors poll; a child of fork shares
// the queue; mq_notify signals one process, SI_MESGQ with its value, when a
// message comes to an empty queue, and once; a descriptor not open for
// sending or receiving is EBADF; a queue unlinked stays while it is open.
// lib/c alone, as POSIX has it and Linux not: exec closes the descriptors.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <mqueue.h>
#include <poll.h>
#include <signal.h>
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

extern char** environ;

static const char* name = "/pizdos-message-queues";

static timespec after(long ms) {
    timespec t;
    assert(clock_gettime(CLOCK_REALTIME, &t) == 0);
    t.tv_nsec += ms * 1000000;
    t.tv_sec += t.tv_nsec / 1000000000;
    t.tv_nsec %= 1000000000;
    return t;
}

static void received(mqd_t q, const char* want, unsigned prio) {
    char buf[64];
    unsigned got = 99;
    ssize_t n = mq_receive(q, buf, sizeof buf, &got);
    assert(n == ssize_t(strlen(want)) && memcmp(buf, want, n) == 0 && got == prio);
}

static void exited(pid_t pid, int code) {
    int status;
    assert(waitpid(pid, &status, 0) == pid && WIFEXITED(status) && WEXITSTATUS(status) == code);
}

int main(int argc, char** argv) {
    if (argc == 2) {
        return fcntl(atoi(argv[1]), F_GETFD) == -1 && errno == EBADF ? 0 : 1;
    }
    mq_unlink(name);
    mq_attr attr = {};
    attr.mq_maxmsg = 4;
    attr.mq_msgsize = 64;
    mqd_t q = mq_open(name, O_RDWR | O_CREAT | O_EXCL, 0600, &attr);
    assert(q >= 0);
    assert(mq_open(name, O_RDWR | O_CREAT | O_EXCL, 0600, &attr) == -1 && errno == EEXIST);
    mq_attr got = {};
    assert(mq_getattr(q, &got) == 0 && got.mq_maxmsg == 4 && got.mq_msgsize == 64 && got.mq_curmsgs == 0 && got.mq_flags == 0);

    assert(mq_send(q, "low", 3, 1) == 0 && mq_send(q, "high", 4, 5) == 0 && mq_send(q, "mid", 3, 3) == 0 && mq_send(q, "high2", 5, 5) == 0);
    assert(mq_getattr(q, &got) == 0 && got.mq_curmsgs == 4);
    pollfd p = {q, POLLIN | POLLOUT, 0};
    assert(poll(&p, 1, 0) == 1 && p.revents == POLLIN);
    mq_attr nonblocking = {}, old = {};
    nonblocking.mq_flags = O_NONBLOCK;
    assert(mq_setattr(q, &nonblocking, &old) == 0 && old.mq_flags == 0 && old.mq_curmsgs == 4);
    assert(mq_send(q, "x", 1, 0) == -1 && errno == EAGAIN);
    assert(mq_getattr(q, &got) == 0 && got.mq_flags == O_NONBLOCK);
    assert(mq_setattr(q, &old, nullptr) == 0);
    timespec soon = after(20);
    assert(mq_timedsend(q, "x", 1, 0, &soon) == -1 && errno == ETIMEDOUT);
    char small[8];
    assert(mq_receive(q, small, sizeof small, nullptr) == -1 && errno == EMSGSIZE);
    char big[65] = {};
    assert(mq_send(q, big, sizeof big, 0) == -1 && errno == EMSGSIZE);
    assert(mq_send(q, "x", 1, MQ_PRIO_MAX) == -1 && errno == EINVAL);
    received(q, "high", 5);
    received(q, "high2", 5);
    received(q, "mid", 3);
    received(q, "low", 1);
    assert(poll(&p, 1, 0) == 1 && p.revents == POLLOUT);
    char buf[64];
    soon = after(20);
    assert(mq_timedreceive(q, buf, sizeof buf, nullptr, &soon) == -1 && errno == ETIMEDOUT);
    assert(mq_setattr(q, &nonblocking, nullptr) == 0);
    assert(mq_receive(q, buf, sizeof buf, nullptr) == -1 && errno == EAGAIN);
    assert(mq_setattr(q, &old, nullptr) == 0);

    pid_t child = fork();
    if (child == 0) {
        mqd_t w = mq_open(name, O_WRONLY);
        _exit(w >= 0 && mq_send(w, "child", 5, 2) == 0 && mq_send(q, "inherited", 9, 1) == 0 ? 0 : 1);
    }
    received(q, "child", 2);
    received(q, "inherited", 1);
    exited(child, 0);

    sigset_t usr1;
    sigemptyset(&usr1);
    sigaddset(&usr1, SIGUSR1);
    assert(sigprocmask(SIG_BLOCK, &usr1, nullptr) == 0);
    sigevent event = {};
    event.sigev_notify = SIGEV_SIGNAL;
    event.sigev_signo = SIGUSR1;
    event.sigev_value.sival_int = 42;
    assert(mq_notify(q, &event) == 0);
    assert(mq_notify(q, &event) == -1 && errno == EBUSY);
    child = fork();
    if (child == 0) {
        _exit(mq_notify(q, &event) == -1 && errno == EBUSY && mq_send(q, "note", 4, 0) == 0 ? 0 : 1);
    }
    exited(child, 0);
    siginfo_t info = {};
    timespec wait = {5, 0};
    assert(sigtimedwait(&usr1, &info, &wait) == SIGUSR1 && info.si_code == SI_MESGQ && info.si_value.sival_int == 42 && info.si_pid == child);
    assert(mq_send(q, "again", 5, 0) == 0);
    wait = {0, 20000000};
    assert(sigtimedwait(&usr1, &info, &wait) == -1 && errno == EAGAIN);
    received(q, "note", 0);
    received(q, "again", 0);
    assert(mq_notify(q, &event) == 0 && mq_notify(q, nullptr) == 0 && mq_notify(q, &event) == 0 && mq_notify(q, nullptr) == 0);

    mqd_t reader = mq_open(name, O_RDONLY), writer = mq_open(name, O_WRONLY);
    assert(reader >= 0 && writer >= 0);
    assert(mq_send(reader, "x", 1, 0) == -1 && errno == EBADF);
    assert(mq_receive(writer, buf, sizeof buf, nullptr) == -1 && errno == EBADF);
    assert(mq_send(-1, "x", 1, 0) == -1 && errno == EBADF && mq_getattr(-1, &got) == -1 && errno == EBADF);

#ifdef __wasm__
    char number[16];
    snprintf(number, sizeof number, "%d", q);
    char* args[] = {argv[0], number, nullptr};
    assert(posix_spawn(&child, argv[0], nullptr, nullptr, args, environ) == 0);
    exited(child, 0);
#endif

    assert(mq_unlink(name) == 0 && mq_unlink(name) == -1 && errno == ENOENT);
    assert(mq_open(name, O_RDWR) == -1 && errno == ENOENT);
    assert(mq_send(writer, "kept", 4, 0) == 0);
    received(reader, "kept", 0);
    assert(mq_close(reader) == 0 && mq_close(writer) == 0 && mq_close(q) == 0);
    assert(mq_close(q) == -1 && errno == EBADF);
    printf("message queues: ok\n");
}
