/*
 * The calls of sockets that name a path: the kernel knows no path, so an
 * AF_UNIX address's is a handle of its node, opened here, which bind,
 * connect and a send to it give the kernel (kernel.h), the address its
 * name still. bind makes the node first, of the umask's mode, a node
 * there already of a socket's not bound taken as it is, and one it made
 * it takes away again where the kernel's bind fails; connect and a
 * send open it following its links, and the kernel finds what is bound
 * to it, of the right to write to it. A socket of the network's is an
 * open of sockmgr's /dev/sockmgr/socket, made a socket by the kernel;
 * sendto is sendmsg's, and sendmmsg sendmsg's of each message.
 */
#include "sized.h"
#include "kernel.h"

#include <errno.h>
#include <fcntl.h>
#include <stddef.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/uio.h>
#include <sys/un.h>
#include <unistd.h>

// The path of an AF_UNIX address, into out of sizeof sun_path bytes, or
// null of another address, whose errors are the kernel's.
static const char* unixPath(const struct sockaddr* a, socklen_t n, char* out) {
    size_t room = sizeof(((struct sockaddr_un*)0)->sun_path);

    if (!a || n <= offsetof(struct sockaddr_un, sun_path) || a->sa_family != AF_UNIX) {
        return NULL;
    }

    const char* path = ((const struct sockaddr_un*)a)->sun_path;
    size_t k = n - offsetof(struct sockaddr_un, sun_path);

    k = strnlen(path, k < room ? k : room);

    if (k == 0 || k >= room) {
        return NULL;
    }

    memcpy(out, path, k);
    out[k] = 0;

    return out;
}

// The node of a path, an O_PATH open: -1 and errno of none.
static int nodeOf(const char* path, int follow) {
    return open(path, O_PATH | O_CLOEXEC | (follow ? 0 : O_NOFOLLOW));
}

static void closeKeeping(int fd) {
    int saved = errno;

    close(fd);
    errno = saved;
}

int __kernel_socket(int domain, int type, int protocol) {
    if (domain != AF_INET && domain != AF_INET6) {
        return __kernel_socketon(-1, domain, type, protocol);
    }

    int f = open("/dev/sockmgr/socket", O_RDWR | O_CLOEXEC | (type & SOCK_NONBLOCK ? O_NONBLOCK : 0));

    if (f < 0) {
        if (errno == ENOENT) {
            errno = EAFNOSUPPORT;
        }

        return -1;
    }

    int s = __kernel_socketon(f, domain, type, protocol);

    closeKeeping(f);

    return s;
}

int __kernel_bind(int fd, const struct sockaddr* a, socklen_t n) {
    char path[sizeof(((struct sockaddr_un*)0)->sun_path)];
    int type;
    socklen_t size = sizeof type;

    if (!unixPath(a, n, path)) {
        return __kernel_bindat(fd, -1, a, n);
    }

    if (getsockopt(fd, SOL_SOCKET, SO_TYPE, &type, &size) != 0) {
        return -1;
    }

    int made = mknod(path, S_IFSOCK | 0777, 0) == 0;

    if (!made && errno != EEXIST) {
        return -1;
    }

    int node = nodeOf(path, 0);
    int r = node < 0 ? -1 : __kernel_bindat(fd, node, a, n);

    if (node >= 0) {
        closeKeeping(node);
    }

    if (r != 0 && made) {
        int saved = errno;

        unlink(path);
        errno = saved;
    }

    return r;
}

int __kernel_connect(int fd, const struct sockaddr* a, socklen_t n) {
    char path[sizeof(((struct sockaddr_un*)0)->sun_path)];

    if (!unixPath(a, n, path)) {
        return __kernel_connectat(fd, -1, a, n);
    }

    int node = nodeOf(path, 1);

    if (node < 0) {
        return -1;
    }

    int r = __kernel_connectat(fd, node, a, n);

    closeKeeping(node);

    return r;
}

static ssize_t sendOf(int fd, const struct msghdr* m, int flags) {
    char path[sizeof(((struct sockaddr_un*)0)->sun_path)];

    if (!unixPath(m->msg_name, m->msg_namelen, path)) {
        return __kernel_sendmsgat(fd, -1, m, flags);
    }

    int node = nodeOf(path, 1);

    if (node < 0) {
        return -1;
    }

    ssize_t r = __kernel_sendmsgat(fd, node, m, flags);

    closeKeeping(node);

    return r;
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
