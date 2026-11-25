/*
 * POSIX's message queues, ipc's (bin/tools/ipc, lib/protocol): an open of
 * /dev/ipc/mq is a control file, whose SEND, send here, opens or unlinks
 * a queue by its name, and whose ACCEPT then gives a descriptor of the
 * queue, exec closing it. A message's SEND, sendmsg, brings its priority
 * and who sends it, and a RECV, recvmsg, takes the priority and the
 * message; each waits in ipc but of O_NONBLOCK, and a timed one waits
 * here, in ppoll, for the queue's readiness, trying again until its time
 * is past. The attributes are getsockopt's of the queue, and so is a
 * notification, setsockopt's. A name is one of a slash, the slashes
 * before it not part of it. What is no queue is EBADF, a socket sent
 * nothing.
 */
#include "sized.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <mqueue.h>
#include <poll.h>
#include <signal.h>
#include <stdarg.h>
#include <stddef.h>
#include <stdint.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/uio.h>
#include <time.h>
#include <unistd.h>

enum { mqOpen = 1, mqUnlink = 2 };
enum { ipcLevel = 0x6970, mqAttributes = 1, mqNotify = 2, mqUnnotify = 3 };

static const char control[] = "/dev/ipc/mq";

// The name past its slashes, or null with errno.
static const char* queueName(const char* name) {
    while (*name == '/') {
        name++;
    }

    size_t n = strlen(name);

    if (n == 0 || strchr(name, '/') || strcmp(name, ".") == 0 || strcmp(name, "..") == 0) {
        errno = EINVAL;

        return NULL;
    }

    if (n > NAME_MAX) {
        errno = ENAMETOOLONG;

        return NULL;
    }

    return name;
}

// A control file's call of size bytes: the control, or -1 with errno.
static int controlCall(const void* call, size_t size) {
    int c = open(control, O_RDWR | O_CLOEXEC);

    if (c < 0) {
        return -1;
    }

    if (send(c, call, size, 0) < 0) {
        int saved = errno;

        close(c);
        errno = saved;

        return -1;
    }

    return c;
}

// A queue's descriptor, as far as a socket is none: 0, or -1 and EBADF.
static int queueOf(mqd_t q) {
    struct stat st;

    if (fstat(q, &st) != 0 || S_ISSOCK(st.st_mode)) {
        errno = EBADF;

        return -1;
    }

    return 0;
}

// A queue's errno of what is none of a queue's: EBADF.
static int failed(void) {
    if (errno == ENOTSOCK || errno == ENOPROTOOPT) {
        errno = EBADF;
    }

    return -1;
}

mqd_t __kernel_mq_open(const char* name, int flags, va_list ap, size_t size) {
    sized("mq_open", size, sizeof(struct mq_attr));

    struct {
        uint32_t call;
        uint32_t flags;
        uint32_t mode;
        uint32_t maxmsg;
        uint32_t msgsize;
        char name[NAME_MAX + 1];
    } o = {mqOpen, (uint32_t)flags, 0, 0, 0, {0}};
    const char* key = queueName(name);

    if (!key) {
        return -1;
    }

    if ((flags & O_ACCMODE) == O_ACCMODE) {
        errno = EINVAL;

        return -1;
    }

    if (flags & O_CREAT) {
        mode_t mode = va_arg(ap, mode_t);
        const struct mq_attr* attr = va_arg(ap, const struct mq_attr*);

        if (attr && (attr->mq_maxmsg <= 0 || attr->mq_msgsize <= 0)) {
            errno = EINVAL;

            return -1;
        }

        o.mode = mode & 0777 & ~getumask();
        o.maxmsg = attr ? (uint32_t)attr->mq_maxmsg : 0;
        o.msgsize = attr ? (uint32_t)attr->mq_msgsize : 0;
    }

    size_t n = strlen(key);

    memcpy(o.name, key, n + 1);

    int c = controlCall(&o, offsetof(__typeof__(o), name) + n + 1);

    if (c < 0) {
        return -1;
    }

    int q = accept4(c, NULL, NULL, SOCK_CLOEXEC | (flags & O_NONBLOCK ? SOCK_NONBLOCK : 0));
    int saved = errno;

    close(c);
    errno = saved;

    return q;
}

int __kernel_mq_unlink(const char* name) {
    struct {
        uint32_t call;
        char name[NAME_MAX + 1];
    } o = {mqUnlink, {0}};
    const char* key = queueName(name);

    if (!key) {
        return -1;
    }

    size_t n = strlen(key);

    memcpy(o.name, key, n + 1);

    int c = controlCall(&o, sizeof o.call + n + 1);

    if (c < 0) {
        return -1;
    }

    close(c);

    return 0;
}

// The attributes: maxmsg, msgsize, curmsgs and the description's flags.
static int attributes(mqd_t q, struct mq_attr* out) {
    uint32_t a[4];
    socklen_t n = sizeof a;

    if (queueOf(q) != 0) {
        return -1;
    }

    if (getsockopt(q, ipcLevel, mqAttributes, a, &n) != 0 || n != sizeof a) {
        errno = EBADF;

        return -1;
    }

    out->mq_maxmsg = a[0];
    out->mq_msgsize = a[1];
    out->mq_curmsgs = a[2];
    out->mq_flags = a[3];

    return 0;
}

int __kernel_mq_close(mqd_t q) {
    struct mq_attr a;

    return attributes(q, &a) == 0 ? close(q) : -1;
}

int __kernel_mq_getattr(mqd_t q, struct mq_attr* attr, size_t size) {
    sized("mq_getattr", size, sizeof(struct mq_attr));

    return attributes(q, attr);
}

int __kernel_mq_setattr(mqd_t q, const struct mq_attr* attr, size_t size, struct mq_attr* old, size_t oldSize) {
    sized("mq_setattr", size, sizeof(struct mq_attr));
    sized("mq_setattr", oldSize, sizeof(struct mq_attr));

    struct mq_attr was;

    if (attributes(q, &was) != 0) {
        return -1;
    }

    int flags = fcntl(q, F_GETFL);

    if (flags < 0 || fcntl(q, F_SETFL, (flags & ~O_NONBLOCK) | (attr->mq_flags & O_NONBLOCK)) != 0) {
        return failed();
    }

    if (old) {
        *old = was;
    }

    return 0;
}

// The deadline's time left, 0 past it, or -1 of a time that is none,
// EINVAL.
static int left(const struct timespec* at, struct timespec* out) {
    if (at->tv_nsec < 0 || at->tv_nsec >= 1000000000) {
        errno = EINVAL;

        return -1;
    }

    struct timespec now;

    clock_gettime(CLOCK_REALTIME, &now);
    out->tv_sec = at->tv_sec - now.tv_sec;
    out->tv_nsec = at->tv_nsec - now.tv_nsec;

    if (out->tv_nsec < 0) {
        out->tv_nsec += 1000000000;
        out->tv_sec--;
    }

    return out->tv_sec >= 0 && (out->tv_sec > 0 || out->tv_nsec > 0);
}

// A call that waits until a deadline: tried without waiting, and again
// once the queue is ready for it, events, until the time is past,
// ETIMEDOUT; of a queue of O_NONBLOCK, EAGAIN at once.
static ssize_t timed(mqd_t q, struct msghdr* m, int receives, short events, const struct timespec* at) {
    int flags = fcntl(q, F_GETFL);

    if (flags < 0) {
        return failed();
    }

    for (;;) {
        ssize_t n = receives ? recvmsg(q, m, MSG_DONTWAIT) : sendmsg(q, m, MSG_DONTWAIT);

        if (n >= 0 || errno != EAGAIN || (flags & O_NONBLOCK)) {
            return n < 0 ? failed() : n;
        }

        struct timespec wait;
        int r = left(at, &wait);

        if (r <= 0) {
            errno = r < 0 ? EINVAL : ETIMEDOUT;

            return -1;
        }

        struct pollfd p = {q, events, 0};

        if (ppoll(&p, 1, &wait, NULL) < 0) {
            return -1;
        }
    }
}

// A message's SEND: its header, the priority and who sends it, then the
// message.
static ssize_t sendOf(mqd_t q, const char* msg, size_t n, unsigned prio, const struct timespec* at) {
    uint32_t head[3] = {prio, (uint32_t)getpid(), (uint32_t)getuid()};
    struct iovec io[2] = {{head, sizeof head}, {(void*)msg, n}};
    struct msghdr m = {0};

    if (prio >= MQ_PRIO_MAX) {
        errno = EINVAL;

        return -1;
    }

    if (queueOf(q) != 0) {
        return -1;
    }

    m.msg_iov = io;
    m.msg_iovlen = 2;

    if (at) {
        return timed(q, &m, 0, POLLOUT, at) < 0 ? -1 : 0;
    }

    return sendmsg(q, &m, 0) < 0 ? failed() : 0;
}

int __kernel_mq_send(mqd_t q, const char* msg, size_t n, unsigned prio) {
    return (int)sendOf(q, msg, n, prio, NULL);
}

int __kernel_mq_timedsend(mqd_t q, const char* msg, size_t n, unsigned prio, const struct timespec* at, size_t size) {
    sized("mq_timedsend", size, sizeof(struct timespec));

    return (int)sendOf(q, msg, n, prio, at);
}

// A RECV: the priority, then the message, its size the answer's.
static ssize_t receiveOf(mqd_t q, char* buf, size_t n, unsigned* prio, const struct timespec* at) {
    uint32_t got = 0;
    struct iovec io[2] = {{&got, sizeof got}, {buf, n}};
    struct msghdr m = {0};

    if (queueOf(q) != 0) {
        return -1;
    }

    m.msg_iov = io;
    m.msg_iovlen = 2;

    ssize_t r = at ? timed(q, &m, 1, POLLIN, at) : recvmsg(q, &m, 0);

    if (r < 0) {
        return at ? -1 : failed();
    }

    if (prio) {
        *prio = got;
    }

    return r - (ssize_t)sizeof got;
}

ssize_t __kernel_mq_receive(mqd_t q, char* buf, size_t n, unsigned* prio) {
    return receiveOf(q, buf, n, prio, NULL);
}

ssize_t __kernel_mq_timedreceive(mqd_t q, char* restrict buf, size_t n, unsigned* restrict prio, const struct timespec* restrict at,
                                 size_t size) {
    sized("mq_timedreceive", size, sizeof(struct timespec));

    return receiveOf(q, buf, n, prio, at);
}

// A notification asked of a queue, of who asks: a signal, or nothing
// but that it is the process's; none takes the process's away.
int __kernel_mq_notify(mqd_t q, const struct sigevent* event, size_t size) {
    sized("mq_notify", size, sizeof(struct sigevent));

    uint32_t value[4] = {(uint32_t)getpid(), 0, 0, 0};

    if (queueOf(q) != 0) {
        return -1;
    }

    if (!event) {
        return setsockopt(q, ipcLevel, mqUnnotify, value, sizeof value[0]) == 0 ? 0 : failed();
    }

    sigset_t set;

    sigemptyset(&set);

    switch (event->sigev_notify) {
        case SIGEV_NONE:
            break;
        case SIGEV_SIGNAL:
            if (sigaddset(&set, event->sigev_signo) != 0) {
                return -1;
            }

            break;
        case SIGEV_THREAD:
            errno = EAGAIN;

            return -1;
        default:
            errno = EINVAL;

            return -1;
    }

    value[1] = (uint32_t)event->sigev_notify;
    value[2] = (uint32_t)event->sigev_signo;
    value[3] = (uint32_t)event->sigev_value.sival_int;

    return setsockopt(q, ipcLevel, mqNotify, value, sizeof value) == 0 ? 0 : failed();
}
