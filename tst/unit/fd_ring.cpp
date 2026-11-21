//@ compile-flags: -g
//@ stdout: "fd ring: ok\n"

// A ring of processes over SOCK_SEQPACKET pairs passing a descriptor
// (SCM_RIGHTS) with a byte: each takes them from the pair before it,
// writes the byte to the file the descriptor is, and passes the
// descriptor, the byte plus one, to the pair after it, closing its own
// copy. The file is one open of it all the way, O_APPEND: it holds every
// hop's byte in order, the descriptor that comes back is that file, and
// the first process's descriptors are as many as before.

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

namespace {
    constexpr int processes = 8;
    constexpr int rounds = 150;

    void give(int s, unsigned char b, int fd) {
        alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))] = {};
        iovec io{&b, 1};
        msghdr m{};
        m.msg_iov = &io;
        m.msg_iovlen = 1;
        m.msg_control = control;
        m.msg_controllen = sizeof control;
        cmsghdr* c = CMSG_FIRSTHDR(&m);
        c->cmsg_level = SOL_SOCKET;
        c->cmsg_type = SCM_RIGHTS;
        c->cmsg_len = CMSG_LEN(sizeof(int));
        memcpy(CMSG_DATA(c), &fd, sizeof fd);
        assert(sendmsg(s, &m, 0) == 1);
    }

    int take(int s, unsigned char& b) {
        alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))];
        iovec io{&b, 1};
        msghdr m{};
        m.msg_iov = &io;
        m.msg_iovlen = 1;
        m.msg_control = control;
        m.msg_controllen = sizeof control;
        assert(recvmsg(s, &m, 0) == 1);
        cmsghdr* c = CMSG_FIRSTHDR(&m);
        assert(c && c->cmsg_level == SOL_SOCKET && c->cmsg_type == SCM_RIGHTS && c->cmsg_len == CMSG_LEN(sizeof(int)));
        int fd;
        memcpy(&fd, CMSG_DATA(c), sizeof fd);
        return fd;
    }

    // Takes the descriptor, writes the byte to its file, passes them on.
    void hop(int in, int out) {
        unsigned char b;
        const int fd = take(in, b);
        assert(write(fd, &b, 1) == 1);
        give(out, (unsigned char)(b + 1), fd);
        assert(close(fd) == 0);
    }

    int lowest() {
        const int fd = open(".", O_RDONLY | O_DIRECTORY);
        assert(fd >= 0 && close(fd) == 0);
        return fd;
    }
}

int main() {
    int pairs[processes][2];
    for (auto& p : pairs) {
        assert(socketpair(AF_UNIX, SOCK_SEQPACKET, 0, p) == 0);
    }
    for (int i = 1; i < processes; ++i) {
        if (fork() == 0) {
            for (int r = 0; r < rounds; ++r) {
                hop(pairs[i][1], pairs[(i + 1) % processes][0]);
            }
            _exit(0);
        }
    }
    const int fd = open("fd_ring.txt", O_RDWR | O_CREAT | O_TRUNC | O_APPEND, 0644);
    assert(fd >= 0);
    struct stat first;
    assert(fstat(fd, &first) == 0);
    give(pairs[1][0], 0, fd);
    assert(close(fd) == 0);
    const int before = lowest();
    // The descriptor goes around rounds times, the last ending here.
    for (int r = 0; r + 1 < rounds; ++r) {
        hop(pairs[0][1], pairs[1][0]);
    }
    unsigned char b;
    const int back = take(pairs[0][1], b);
    const int writes = processes * rounds - 1;
    assert(b == (unsigned char)writes);
    struct stat last;
    assert(fstat(back, &last) == 0 && last.st_ino == first.st_ino && last.st_dev == first.st_dev);
    assert(close(back) == 0);
    for (int i = 1; i < processes; ++i) {
        int status;
        assert(wait(&status) > 0 && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    }
    assert(lowest() == before);

    unsigned char got[processes * rounds];
    const int file = open("fd_ring.txt", O_RDONLY);
    assert(file >= 0 && read(file, got, sizeof got) == writes && close(file) == 0);
    for (int k = 0; k < writes; ++k) {
        assert(got[k] == (unsigned char)k);
    }
    assert(unlink("fd_ring.txt") == 0);
    puts("fd ring: ok");
}
