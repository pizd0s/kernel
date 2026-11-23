//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "message vectors: ok\n"

// sendmmsg and recvmmsg, Linux's and FreeBSD's: a vector of a datagram
// socket's messages sent or received at once, each message's length its
// msg_len. MSG_DONTWAIT ends a receive with the messages there are,
// MSG_WAITFORONE waits for the first only; an error ends the vector, -1
// with errno where it is the first message's, else the count before it;
// an invalid timeout is EINVAL. lib/c's alone: recvmmsg's timeout is the
// whole call's, 0 messages where none came in time and those that did
// else, where Linux looks at it only after a message, so that a call can
// wait for ever, and FreeBSD only before the first.

#include <assert.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

struct Vector {
    mmsghdr m[4];
    iovec io[4];
    char data[4][16];
};

static void prepare(Vector& v) {
    memset(&v, 0, sizeof v);
    for (int i = 0; i < 4; ++i) {
        v.io[i] = {v.data[i], sizeof v.data[i]};
        v.m[i].msg_hdr.msg_iov = &v.io[i];
        v.m[i].msg_hdr.msg_iovlen = 1;
    }
}

int main() {
    int fds[2];
    assert(socketpair(AF_UNIX, SOCK_DGRAM, 0, fds) == 0);
    const char* texts[] = {"one", "three", "fives"};
    mmsghdr out[3] = {};
    iovec io[3];
    for (int i = 0; i < 3; ++i) {
        io[i] = {const_cast<char*>(texts[i]), strlen(texts[i])};
        out[i].msg_hdr.msg_iov = &io[i];
        out[i].msg_hdr.msg_iovlen = 1;
    }
    assert(sendmmsg(fds[0], out, 3, 0) == 3);
    for (int i = 0; i < 3; ++i) {
        assert(out[i].msg_len == strlen(texts[i]));
    }

    Vector v;
    prepare(v);
    assert(recvmmsg(fds[1], v.m, 4, MSG_DONTWAIT, nullptr) == 3);
    for (int i = 0; i < 3; ++i) {
        assert(v.m[i].msg_len == strlen(texts[i]) && memcmp(v.data[i], texts[i], v.m[i].msg_len) == 0);
    }
    prepare(v);
    errno = 0;
    assert(recvmmsg(fds[1], v.m, 4, MSG_DONTWAIT, nullptr) == -1 && errno == EAGAIN);

    assert(sendmmsg(fds[0], out, 2, 0) == 2);
    prepare(v);
    assert(recvmmsg(fds[1], v.m, 4, MSG_WAITFORONE, nullptr) == 2 && v.m[1].msg_len == 5);
    assert(sendmmsg(fds[0], out, 0, 0) == 0 && recvmmsg(fds[1], v.m, 0, 0, nullptr) == 0);

    // A message larger than a datagram can be ends the vector.
    static char huge[1 << 20];
    mmsghdr two[2] = {out[0], {}};
    iovec big = {huge, sizeof huge};
    two[1].msg_hdr.msg_iov = &big;
    two[1].msg_hdr.msg_iovlen = 1;
    assert(sendmmsg(fds[0], two, 2, 0) == 1);
    errno = 0;
    assert(sendmmsg(fds[0], two + 1, 1, 0) == -1 && errno == EMSGSIZE);
    prepare(v);
    assert(recvmmsg(fds[1], v.m, 4, MSG_DONTWAIT, nullptr) == 1 && v.m[0].msg_len == 3);

    errno = 0;
    assert(sendmmsg(-1, out, 1, 0) == -1 && errno == EBADF);
    errno = 0;
    assert(recvmmsg(-1, v.m, 1, 0, nullptr) == -1 && errno == EBADF);
    timespec bad = {0, 1000000000};
    errno = 0;
    assert(recvmmsg(fds[1], v.m, 4, 0, &bad) == -1 && errno == EINVAL);

#ifdef __wasm__
    timespec soon = {0, 50000000};
    prepare(v);
    assert(recvmmsg(fds[1], v.m, 4, 0, &soon) == 0);
    assert(sendmmsg(fds[0], out, 1, 0) == 1);
    prepare(v);
    assert(recvmmsg(fds[1], v.m, 4, 0, &soon) == 1 && v.m[0].msg_len == 3);
#endif
    printf("message vectors: ok\n");
}
