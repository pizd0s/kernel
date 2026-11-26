/*
 * The calls of sockets that take what the kernel does not know: a family
 * and a path. A socket of AF_UNIX is the VFS's, rootfs's (lib/protocol),
 * a ctlSocket of the root, and an address of a path is a ctl of the
 * socket's, the root and where the path starts, the working directory or
 * the root, with it, which rootfs walks: bind makes the node, of the
 * umask's mode, connect and a send find the socket bound to it. A socket
 * that serves no such ctl, an end of a pair or the network's, takes the
 * kernel's call, as an address of no path does. A socket of the
 * network's is an open of sockmgr's /dev/sockmgr/socket, told what it
 * is by a ctlSocket too. An address of AF_UNIX with no path, or one that starts
 * with a 0, names nothing, ENOENT, and one of a length that is none of
 * sockaddr_un's is EINVAL. sendto is sendmsg's, and sendmmsg sendmsg's
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

#define PATH_ROOM sizeof(((struct sockaddr_un*)0)->sun_path)

// The most rights a send with a path brings: a ctl's descriptors but the
// two of the path.
enum { rightsMax = 29 };

// The path of an AF_UNIX address, into out of PATH_ROOM + 1 bytes: 1; 0
// of an address of another family or of none, the kernel's to judge; -1
// and errno of one that is no path.
static int unixPath(const struct sockaddr* a, socklen_t n, char* out) {
    size_t before = offsetof(struct sockaddr_un, sun_path);

    if (!a || n < sizeof(sa_family_t) || a->sa_family != AF_UNIX) {
        return 0;
    }

    if (n <= before || n > sizeof(struct sockaddr_un)) {
        errno = EINVAL;

        return -1;
    }

    const char* path = ((const struct sockaddr_un*)a)->sun_path;
    size_t k = strnlen(path, n - before);

    if (k == 0) {
        errno = ENOENT;

        return -1;
    }

    memcpy(out, path, k);
    out[k] = 0;

    return 1;
}

// A socket's call with a path (calls.h), arg with it, then the path, and
// for a send a 0 and the n bytes at data, the rights its descriptors
// after the root and where the path starts; the answer's u32 into out
// but for null. 0, or -1 and errno, ENOTTY of a socket that is not
// rootfs's.
static int pathCall(int fd, uint32_t call, uint32_t arg, const char* path, const void* data, size_t n, const int* rights, size_t nrights, uint32_t* out) {
    unsigned char small[8 + PATH_ROOM + 1];
    size_t k = strlen(path);
    size_t size = 8 + k + (data ? 1 + n : 0);
    unsigned char* in = size <= sizeof small ? small : malloc(size);
    int fds[2 + rightsMax] = {AT_FDROOT, path[0] == '/' ? AT_FDROOT : AT_FDCWD};

    if (!in) {
        return -1;
    }

    memcpy(in, &call, 4);
    memcpy(in + 4, &arg, 4);
    memcpy(in + 8, path, k);

    if (data) {
        in[8 + k] = 0;
        memcpy(in + 8 + k + 1, data, n);
    }

    memcpy(fds + 2, rights, nrights * sizeof(int));

    struct ctl c = {in, size, fds, 2 + nrights, out, out ? sizeof *out : 0, NULL, 0};
    int r = ctl(fd, &c, 0);

    if (in != small) {
        int saved = errno;

        free(in);
        errno = saved;
    }

    return r;
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
    char path[PATH_ROOM + 1];
    int named = unixPath(a, n, path);

    if (named < 0) {
        return -1;
    }

    if (named) {
        int r = pathCall(fd, ctlBind, 0777 & ~getumask(), path, NULL, 0, NULL, 0, NULL);

        if (r == 0 || errno != ENOTTY) {
            return r;
        }
    }

    return __kernel_sockbind(fd, a, n);
}

int __kernel_connect(int fd, const struct sockaddr* a, socklen_t n) {
    char path[PATH_ROOM + 1];
    int named = unixPath(a, n, path);

    if (named < 0) {
        return -1;
    }

    if (named) {
        int r = pathCall(fd, ctlConnect, 0, path, NULL, 0, NULL, 0, NULL);

        if (r == 0 || errno != ENOTTY) {
            return r;
        }
    }

    return __kernel_sockconnect(fd, a, n);
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

// A message to a path, a ctlSendto of the socket's, its bytes in one
// piece: the count sent, -1 and errno, ENOTTY of a socket that is not
// rootfs's.
static ssize_t sendTo(int fd, const char* path, const struct msghdr* m, int flags) {
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

    int r = pathCall(fd, ctlSendto, (uint32_t)flags, path, data, n, rights, (size_t)nrights, &count);
    int saved = errno;

    free(data);
    errno = saved;

    return r == 0 ? (ssize_t)count : -1;
}

static ssize_t sendOf(int fd, const struct msghdr* m, int flags) {
    char path[PATH_ROOM + 1];
    int named = unixPath(m->msg_name, m->msg_namelen, path);

    if (named < 0) {
        return -1;
    }

    if (named) {
        ssize_t r = sendTo(fd, path, m, flags);

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
