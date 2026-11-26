/*
 * The sockets of the C library, but what an end of a pair does, the
 * kernel's (lib/protocol). socket is a ctlSocket, of the root for
 * AF_UNIX, rootfs's, of an open of sockmgr's /dev/sockmgr/socket for the
 * network's families. bind, connect, listen, accept, the names, the
 * options and shutdown are CTLs of the socket (calls.h): its server
 * answers them, the kernel those of an end of a pair, and what is no
 * socket serves none, ENOTSOCK. An address of AF_UNIX is a path of the
 * VFS's, which rootfs walks: the root and the working directory go with
 * it, and a send to one is a CTL too, ctlSendto, the kernel's send of a
 * socket that serves none. sendto is sendmsg's, and sendmmsg sendmsg's
 * of each message.
 */
#include "sized.h"
#include "kernel.h"

#include "../protocol/calls.h"

#include <errno.h>
#include <fcntl.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/uio.h>
#include <sys/un.h>
#include <unistd.h>

// The most rights a send to a path brings: a ctl's descriptors but the
// two of the path.
enum { rightsMax = 29 };

// A socket's call (calls.h): its number, its arg, then the parts, with
// the nfds descriptors; the answer's bytes into out of *room, *room
// then their count, and its descriptor into got, each but for null;
// flags are ctl's. 0, or -1 and errno, ENOTTY of what serves no such
// call.
static int call(int fd, uint32_t what, uint32_t arg, const struct iovec* parts, int count, const int* fds, size_t nfds, void* out, size_t* room, int* got, int flags) {
    unsigned char small[8 + 280];
    size_t size = 8;

    for (int i = 0; i < count; i++) {
        size += parts[i].iov_len;
    }

    unsigned char* in = size <= sizeof small ? small : malloc(size);

    if (!in) {
        return -1;
    }

    memcpy(in, &what, 4);
    memcpy(in + 4, &arg, 4);

    for (size_t i = 0, at = 8; i < (size_t)count; i++) {
        memcpy(in + at, parts[i].iov_base, parts[i].iov_len);
        at += parts[i].iov_len;
    }

    struct ctl c = {in, size, fds, nfds, out, room ? *room : 0, got, got != NULL};
    int r = ctl(fd, &c, flags);

    if (in != small) {
        int saved = errno;

        free(in);
        errno = saved;
    }

    if (r != 0) {
        return -1;
    }

    if (room) {
        *room = c.room;
    }

    if (got && c.ngot != 1) {
        errno = EIO;

        return -1;
    }

    return 0;
}

// What a call of a socket's said, of what is no socket: ENOTSOCK.
static int ofSocket(int r) {
    if (r != 0 && errno == ENOTTY) {
        errno = ENOTSOCK;
    }

    return r;
}

// A call that takes an address: len u32 and the address, then the n
// bytes at data. The root and the working directory are the first
// descriptors of an AF_UNIX's, a path's, and the rights follow them.
static int addressCall(int fd, uint32_t what, uint32_t arg, const struct sockaddr* a, socklen_t alen, const void* data, size_t n, const int* rights, size_t nrights, uint32_t* out) {
    int fds[2 + rightsMax] = {AT_FDROOT, AT_FDCWD};
    int named = a && alen >= sizeof(sa_family_t) && a->sa_family == AF_UNIX;
    uint32_t length = alen;
    struct iovec parts[3] = {{&length, 4}, {(void*)a, alen}, {(void*)data, n}};
    size_t room = sizeof *out;

    if (!a || alen > sizeof(struct sockaddr_storage)) {
        errno = EINVAL;

        return -1;
    }

    memcpy(fds + 2, rights, nrights * sizeof(int));

    return call(fd, what, arg, parts, 3, named ? fds : fds + 2, (named ? 2 : 0) + nrights, out, out ? &room : NULL, NULL, 0);
}

// An address a call answered, size bytes of it, to who asked: as much
// as *alen holds, *alen then its size.
static void putAddress(const void* from, size_t size, struct sockaddr* a, socklen_t* alen) {
    if (a && alen) {
        memcpy(a, from, *alen < size ? *alen : size);
        *alen = (socklen_t)size;
    }
}

static void closeKeeping(int fd) {
    int saved = errno;

    close(fd);
    errno = saved;
}

int __kernel_socket(int domain, int type, int protocol) {
    const uint32_t in[4] = {ctlSocket, (uint32_t)type & ~(uint32_t)SOCK_CLOEXEC, (uint32_t)domain, (uint32_t)protocol};
    int flags = (type & SOCK_CLOEXEC ? O_CLOEXEC : 0) | (type & SOCK_NONBLOCK ? O_NONBLOCK : 0);
    int got = -1;
    struct ctl c = {in, sizeof in, NULL, 0, NULL, 0, &got, 1};

    if (domain == AF_UNIX) {
        if (ctl(AT_FDROOT, &c, flags) != 0) {
            if (errno == ENOTTY) {
                errno = EAFNOSUPPORT;
            }

            return -1;
        }

        if (c.ngot != 1) {
            errno = EIO;

            return -1;
        }

        return got;
    }

    if (domain != AF_INET && domain != AF_INET6) {
        errno = EAFNOSUPPORT;

        return -1;
    }

    int f = open("/dev/sockmgr/socket", O_RDWR | flags);

    if (f < 0) {
        if (errno == ENOENT) {
            errno = EAFNOSUPPORT;
        }

        return -1;
    }

    c.got = NULL;
    c.ngot = 0;

    if (ctl(f, &c, 0) != 0) {
        closeKeeping(f);

        return -1;
    }

    return f;
}

int __kernel_bind(int fd, const struct sockaddr* a, socklen_t n) {
    return ofSocket(addressCall(fd, ctlBind, 0777 & ~getumask(), a, n, NULL, 0, NULL, 0, NULL));
}

int __kernel_connect(int fd, const struct sockaddr* a, socklen_t n) {
    return ofSocket(addressCall(fd, ctlConnect, 0, a, n, NULL, 0, NULL, 0, NULL));
}

int __kernel_listen(int fd, int backlog) {
    return ofSocket(call(fd, ctlListen, (uint32_t)backlog, NULL, 0, NULL, 0, NULL, NULL, NULL, 0));
}

int __kernel_accept4(int fd, struct sockaddr* a, socklen_t* alen, int flags) {
    unsigned char peer[sizeof(struct sockaddr_storage)];
    size_t room = sizeof peer;
    int got = -1;

    if (flags & ~(SOCK_NONBLOCK | SOCK_CLOEXEC)) {
        errno = EINVAL;

        return -1;
    }

    int how = CTL_RESTART | (flags & SOCK_CLOEXEC ? O_CLOEXEC : 0) | (flags & SOCK_NONBLOCK ? O_NONBLOCK : 0);

    if (ofSocket(call(fd, ctlAccept, (uint32_t)flags, NULL, 0, NULL, 0, peer, &room, &got, how)) != 0) {
        return -1;
    }

    putAddress(peer, room < sizeof peer ? room : sizeof peer, a, alen);

    return got;
}

int __kernel_accept(int fd, struct sockaddr* a, socklen_t* alen) {
    return __kernel_accept4(fd, a, alen, 0);
}

static int nameOf(int fd, uint32_t peer, struct sockaddr* a, socklen_t* alen) {
    unsigned char name[sizeof(struct sockaddr_storage)];
    size_t room = sizeof name;

    if (ofSocket(call(fd, ctlSockname, peer, NULL, 0, NULL, 0, name, &room, NULL, 0)) != 0) {
        return -1;
    }

    putAddress(name, room < sizeof name ? room : sizeof name, a, alen);

    return 0;
}

int __kernel_getsockname(int fd, struct sockaddr* a, socklen_t* alen) {
    return nameOf(fd, 0, a, alen);
}

int __kernel_getpeername(int fd, struct sockaddr* a, socklen_t* alen) {
    return nameOf(fd, 1, a, alen);
}

int __kernel_getsockopt(int fd, int level, int name, void* value, socklen_t* length) {
    const uint32_t which[2] = {(uint32_t)level, (uint32_t)name};
    const struct iovec part = {(void*)which, sizeof which};
    unsigned char out[256];
    size_t room = sizeof out;

    if (ofSocket(call(fd, ctlGetsockopt, 0, &part, 1, NULL, 0, out, &room, NULL, 0)) != 0) {
        return -1;
    }

    room = room < sizeof out ? room : sizeof out;
    *length = *length < room ? *length : (socklen_t)room;
    memcpy(value, out, *length);

    return 0;
}

int __kernel_setsockopt(int fd, int level, int name, const void* value, socklen_t length) {
    const uint32_t which[2] = {(uint32_t)level, (uint32_t)name};
    const struct iovec parts[2] = {{(void*)which, sizeof which}, {(void*)value, length}};

    return ofSocket(call(fd, ctlSetsockopt, 0, parts, 2, NULL, 0, NULL, NULL, NULL, 0));
}

int __kernel_shutdown(int fd, int how) {
    return ofSocket(call(fd, ctlShutdown, (uint32_t)how, NULL, 0, NULL, 0, NULL, NULL, NULL, 0));
}

// A stream has no mark, as nothing is sent out of band; what is no
// socket is ENOTTY, as POSIX has it of this one.
int __kernel_sockatmark(int fd) {
    int type = 0;
    socklen_t length = sizeof type;

    if (__kernel_getsockopt(fd, SOL_SOCKET, SO_TYPE, &type, &length) != 0) {
        if (errno == ENOTSOCK) {
            errno = ENOTTY;
        }

        return -1;
    }

    if (type != SOCK_STREAM) {
        errno = EOPNOTSUPP;

        return -1;
    }

    return 0;
}

// The rights of a message's control, SCM_RIGHTS's, into rights of
// rightsMax: their count, or -1 and errno, EINVAL of other control or of
// more.
static int rightsOf(const struct msghdr* m, int* rights) {
    int count = 0;

    for (struct cmsghdr* c = CMSG_FIRSTHDR(m); c; c = CMSG_NXTHDR((struct msghdr*)m, c)) {
        size_t k = c->cmsg_len < CMSG_LEN(0) ? 0 : (c->cmsg_len - CMSG_LEN(0)) / sizeof(int);

        if (c->cmsg_level != SOL_SOCKET || c->cmsg_type != SCM_RIGHTS || count + k > rightsMax) {
            errno = EINVAL;

            return -1;
        }

        memcpy(rights + count, CMSG_DATA(c), k * sizeof(int));
        count += (int)k;
    }

    return count;
}

// A message to an AF_UNIX address, a ctlSendto of the socket's, its
// bytes in one piece: the count sent, -1 and errno, ENOTTY of a socket
// that serves none.
static ssize_t sendTo(int fd, const struct msghdr* m, int flags) {
    int rights[rightsMax];
    int nrights = rightsOf(m, rights);
    size_t n = 0;
    uint32_t count = 0;

    if (nrights < 0) {
        return -1;
    }

    for (size_t i = 0; i < (size_t)m->msg_iovlen; i++) {
        n += m->msg_iov[i].iov_len;
    }

    unsigned char* data = malloc(n ? n : 1);

    if (!data) {
        return -1;
    }

    for (size_t i = 0, at = 0; i < (size_t)m->msg_iovlen; i++) {
        memcpy(data + at, m->msg_iov[i].iov_base, m->msg_iov[i].iov_len);
        at += m->msg_iov[i].iov_len;
    }

    int r = addressCall(fd, ctlSendto, (uint32_t)flags, m->msg_name, m->msg_namelen, data, n, rights, (size_t)nrights, &count);
    int saved = errno;

    free(data);
    errno = saved;

    return r == 0 ? (ssize_t)count : -1;
}

static ssize_t sendOf(int fd, const struct msghdr* m, int flags) {
    const struct sockaddr* to = m->msg_name;

    if (to && m->msg_namelen >= sizeof(sa_family_t) && to->sa_family == AF_UNIX) {
        ssize_t r = sendTo(fd, m, flags);

        if (r >= 0 || errno != ENOTTY) {
            return r;
        }
    }

    return __kernel_socksendmsg(fd, m, flags);
}

ssize_t __kernel_sendmsg(int fd, const struct msghdr* m, size_t size, size_t iovSize, int flags) {
    sized("sendmsg", size, sizeof(struct msghdr));
    sized("sendmsg", iovSize, sizeof(struct iovec));

    return sendOf(fd, m, flags);
}

ssize_t __kernel_sendto(int fd, const void* buf, size_t n, int flags, const struct sockaddr* a, socklen_t alen) {
    struct iovec io = {(void*)buf, n};
    struct msghdr m = {(void*)a, a ? alen : 0, &io, 1, NULL, 0, 0};

    return sendOf(fd, &m, flags);
}

int __kernel_sendmmsg(int fd, struct mmsghdr* v, size_t size, size_t iovSize, unsigned n, int flags) {
    sized("sendmmsg", size, sizeof(struct mmsghdr));
    sized("sendmmsg", iovSize, sizeof(struct iovec));

    if (fcntl(fd, F_GETFD) < 0) {
        return -1;
    }

    n = n < UIO_MAXIOV ? n : UIO_MAXIOV;

    for (unsigned i = 0; i < n; i++) {
        ssize_t r = sendOf(fd, &v[i].msg_hdr, flags);

        if (r < 0) {
            return i == 0 ? -1 : (int)i;
        }

        v[i].msg_len = (unsigned)r;
    }

    return (int)n;
}
