//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "async io: ok\n"

// Asynchronous I/O, as POSIX.1-2024 has it. aio_write and aio_read move
// their bytes at their offsets, aio_error is EINPROGRESS until an operation
// is done and its error after, aio_return its count once; aio_fsync waits
// for the descriptor's operations before it. A read from a pipe waits for
// data: aio_suspend times out with EAGAIN, then returns when it comes.
// aio_cancel cancels a waiting read, ECANCELED and -1, and finds nothing to
// cancel once all is done. SIGEV_SIGNAL signals SI_ASYNCIO with its value,
// an operation's and a whole list's. lio_listio(LIO_WAIT) does a list and
// is EIO where one of it failed, each one's error kept. Errors POSIX lets be
// found at once or later (EBADF for a descriptor not open to read, EINVAL
// for a negative offset) are taken either way. lio_listio's EINVAL for a
// mode of neither LIO_WAIT nor LIO_NOWAIT is lib/c's alone: musl takes any
// mode but LIO_WAIT for LIO_NOWAIT.

#include <aio.h>
#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

static aiocb control(int fd, void* buf, size_t n, off_t offset) {
    aiocb cb = {};
    cb.aio_fildes = fd;
    cb.aio_buf = buf;
    cb.aio_nbytes = n;
    cb.aio_offset = offset;
    cb.aio_sigevent.sigev_notify = SIGEV_NONE;
    return cb;
}

static void finish(aiocb* cb) {
    const aiocb* list[] = {cb};
    while (aio_error(cb) == EINPROGRESS) {
        assert(aio_suspend(list, 1, nullptr) == 0 || errno == EINTR);
    }
}

static ssize_t completed(aiocb* cb, int error) {
    finish(cb);
    assert(aio_error(cb) == error);
    return aio_return(cb);
}

// An error found at the call or in the operation's status.
static void fails(int (*submit)(aiocb*), aiocb* cb, int error) {
    if (submit(cb) == -1) {
        assert(errno == error);
        return;
    }
    assert(completed(cb, error) == -1);
}

int main() {
    char path[] = "async-XXXXXX";
    int fd = mkstemp(path);
    assert(fd >= 0 && unlink(path) == 0);

    char hello[] = "hello world";
    aiocb w = control(fd, hello, 11, 0);
    assert(aio_write(&w) == 0 && completed(&w, 0) == 11);
    char got[64] = {};
    aiocb r = control(fd, got, 5, 6);
    assert(aio_read(&r) == 0 && completed(&r, 0) == 5 && memcmp(got, "world", 5) == 0);
    r = control(fd, got, 16, 100);
    assert(aio_read(&r) == 0 && completed(&r, 0) == 0);

    static char a[4096], b[4096];
    memset(a, 'a', sizeof a);
    memset(b, 'b', sizeof b);
    aiocb wa = control(fd, a, sizeof a, 0), wb = control(fd, b, sizeof b, sizeof a);
    assert(aio_write(&wa) == 0 && aio_write(&wb) == 0);
    aiocb sync = control(fd, nullptr, 0, 0);
    assert(aio_fsync(O_SYNC, &sync) == 0 && completed(&sync, 0) == 0);
    assert(completed(&wa, 0) == 4096 && completed(&wb, 0) == 4096);
    static char back[8192];
    assert(pread(fd, back, sizeof back, 0) == 8192 && memcmp(back, a, 4096) == 0 && memcmp(back + 4096, b, 4096) == 0);
    sync = control(fd, nullptr, 0, 0);
    assert(aio_fsync(O_DSYNC, &sync) == 0 && completed(&sync, 0) == 0);
    assert(aio_fsync(O_RDONLY, &sync) == -1 && errno == EINVAL);

    int pipes[2];
    assert(pipe(pipes) == 0);
    char data[16] = {};
    aiocb p = control(pipes[0], data, sizeof data, 0);
    assert(aio_read(&p) == 0 && aio_error(&p) == EINPROGRESS);
    const aiocb* list[] = {nullptr, &p};
    timespec soon = {0, 20000000};
    assert(aio_suspend(list, 2, &soon) == -1 && errno == EAGAIN && aio_error(&p) == EINPROGRESS);
    assert(write(pipes[1], "data", 4) == 4);
    assert(completed(&p, 0) == 4 && memcmp(data, "data", 4) == 0);

    p = control(pipes[0], data, sizeof data, 0);
    assert(aio_read(&p) == 0 && aio_error(&p) == EINPROGRESS);
    assert(aio_cancel(pipes[0], &p) == AIO_CANCELED);
    // musl's aio_cancel may return before the operation's error is set:
    // its worker wakes it first, so the canceled read is waited for.
    const aiocb* canceled[] = {&p};
    assert(aio_suspend(canceled, 1, nullptr) == 0 && aio_error(&p) == ECANCELED && aio_return(&p) == -1);
    assert(aio_cancel(pipes[0], nullptr) == AIO_ALLDONE && aio_cancel(-1, nullptr) == -1 && errno == EBADF);

    sigset_t usr1;
    sigemptyset(&usr1);
    sigaddset(&usr1, SIGUSR1);
    assert(sigprocmask(SIG_BLOCK, &usr1, nullptr) == 0);
    w = control(fd, hello, 5, 0);
    w.aio_sigevent.sigev_notify = SIGEV_SIGNAL;
    w.aio_sigevent.sigev_signo = SIGUSR1;
    w.aio_sigevent.sigev_value.sival_int = 7;
    assert(aio_write(&w) == 0);
    siginfo_t info = {};
    timespec wait = {5, 0};
    assert(sigtimedwait(&usr1, &info, &wait) == SIGUSR1 && info.si_code == SI_ASYNCIO && info.si_value.sival_int == 7);
    assert(completed(&w, 0) == 5);

    aiocb first = control(fd, a, 100, 0), second = control(fd, b, 100, 100), nop = control(fd, a, 1, 0);
    first.aio_lio_opcode = LIO_WRITE;
    second.aio_lio_opcode = LIO_WRITE;
    nop.aio_lio_opcode = LIO_NOP;
    aiocb* writes[] = {&first, nullptr, &nop, &second};
    assert(lio_listio(LIO_WAIT, writes, 4, nullptr) == 0);
    assert(aio_error(&first) == 0 && aio_return(&first) == 100 && aio_error(&second) == 0 && aio_return(&second) == 100);
    char x[100], y[100];
    first = control(fd, x, 100, 0);
    second = control(fd, y, 100, 100);
    first.aio_lio_opcode = LIO_READ;
    second.aio_lio_opcode = LIO_READ;
    aiocb* reads[] = {&first, &second};
    sigevent all = {};
    all.sigev_notify = SIGEV_SIGNAL;
    all.sigev_signo = SIGUSR1;
    all.sigev_value.sival_int = 9;
    assert(lio_listio(LIO_NOWAIT, reads, 2, &all) == 0);
    assert(sigtimedwait(&usr1, &info, &wait) == SIGUSR1 && info.si_code == SI_ASYNCIO && info.si_value.sival_int == 9);
    assert(completed(&first, 0) == 100 && completed(&second, 0) == 100 && memcmp(x, a, 100) == 0 && memcmp(y, b, 100) == 0);
#ifdef __wasm__
    assert(lio_listio(42, reads, 2, nullptr) == -1 && errno == EINVAL);
#endif

    aiocb bad = control(pipes[1], data, 4, 0);
    fails(aio_read, &bad, EBADF);
    bad = control(fd, data, 4, -1);
    fails(aio_read, &bad, EINVAL);
    bad = control(-1, data, 4, 0);
    assert(aio_read(&bad) == -1 && errno == EBADF);
    first = control(fd, x, 10, 0);
    second = control(pipes[0], y, 10, 0);
    first.aio_lio_opcode = LIO_READ;
    second.aio_lio_opcode = LIO_WRITE;
    aiocb* mixed[] = {&first, &second};
    assert(lio_listio(LIO_WAIT, mixed, 2, nullptr) == -1 && errno == EIO);
    assert(aio_error(&first) == 0 && aio_return(&first) == 10 && aio_error(&second) == EBADF);

    assert(close(pipes[0]) == 0 && close(pipes[1]) == 0 && close(fd) == 0);
    printf("async io: ok\n");
}
