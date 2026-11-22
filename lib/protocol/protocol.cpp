#include "protocol.h"

#include <errno.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/uio.h>

ssize_t sendPacket(int fd, const iovec* parts, size_t count, const int* fds, size_t fdCount, int flags) {
    alignas(cmsghdr) unsigned char control[CMSG_SPACE(maxFds * sizeof(int))] = {};
    msghdr m{};
    m.msg_iov = const_cast<iovec*>(parts);
    m.msg_iovlen = int(count);
    if (fdCount > maxFds) {
        errno = EINVAL;
        return -1;
    }
    if (fdCount > 0) {
        m.msg_control = control;
        m.msg_controllen = socklen_t(CMSG_LEN(fdCount * sizeof(int)));
        cmsghdr* c = CMSG_FIRSTHDR(&m);
        c->cmsg_level = SOL_SOCKET;
        c->cmsg_type = SCM_RIGHTS;
        c->cmsg_len = m.msg_controllen;
        memcpy(CMSG_DATA(c), fds, fdCount * sizeof(int));
    }
    return sendmsg(fd, &m, flags | MSG_NOSIGNAL);
}

ssize_t receivePacket(int fd, void* buffer, size_t size, int* fds, size_t& fdCount, int flags) {
    alignas(cmsghdr) unsigned char control[CMSG_SPACE(maxFds * sizeof(int))];
    iovec iov{buffer, size};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    fdCount = 0;
    const ssize_t n = recvmsg(fd, &m, flags | MSG_CMSG_CLOEXEC);
    if (n < 0) {
        return n;
    }
    for (cmsghdr* c = CMSG_FIRSTHDR(&m); c; c = CMSG_NXTHDR(&m, c)) {
        if (c->cmsg_level != SOL_SOCKET || c->cmsg_type != SCM_RIGHTS || c->cmsg_len < CMSG_LEN(0)) {
            continue;
        }
        const size_t count = (c->cmsg_len - CMSG_LEN(0)) / sizeof(int);
        for (size_t i = 0; i < count && fdCount < maxFds; ++i) {
            memcpy(&fds[fdCount++], CMSG_DATA(c) + i * sizeof(int), sizeof(int));
        }
    }
    return n;
}

int unserved(uint32_t op) {
    if (op >= vfsOpen && op <= vfsPath) {
        return ENOTDIR;
    }
    if (op >= opTcgetattr && op <= opSetCtty) {
        return ENOTTY;
    }
    if (op >= opBind && op <= opSocket) {
        return ENOTSOCK;
    }
    switch (op) {
        case opReaddir:
        case opWatch:
            return ENOTDIR;
        case opSeek:
            return ESPIPE;
        case opFtruncate:
        case opFsync:
            return EINVAL;
        case opRead:
        case opWrite:
            return EBADF;
        default:
            return ENOSYS;
    }
}
