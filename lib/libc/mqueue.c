/*
 * POSIX's message queues, mqueue's (bin/tools/mqueue), each call a ctl
 * (lib/protocol/calls.h): mq_open's and mq_unlink's of an open of
 * /dev/ipc/mq, the first answered with a descriptor of the queue, exec
 * closing it; the others' of the queue. A send brings its priority, who
 * sends it and until when, then the message; a receive, the room and
 * until when, answered the priority, then the message: each waits in
 * mqueue but of O_NONBLOCK, and a signal of SA_RESTART restarts it. A
 * name is one of a slash, the slashes before it not part of it. What is
 * no queue is EBADF.
 */
#include "sized.h"
#include "../protocol/calls.h"

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <mqueue.h>
#include <signal.h>
#include <stdarg.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

static const char control[] = "/dev/ipc/mq";

// A send's or a receive's deadline: timed, then the time.
struct __attribute__((packed)) Until {
    uint32_t timed;
    int64_t sec;
    int64_t nsec;
};

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

// A control's call, of size bytes, a descriptor of its answer's into got
// but for null, of O_NONBLOCK of flags: 0, or -1 and errno.
static int controlCall(const void* call, size_t size, int* got, int flags) {
    int c = open(control, O_RDWR | O_CLOEXEC);

    if (c < 0) {
        return -1;
    }

    struct ctl x = {call, size, NULL, 0, NULL, 0, got, got != NULL};
    int r = ctl(c, &x, O_CLOEXEC | (flags & O_NONBLOCK));
    int saved = errno;

    close(c);
    errno = saved;

    if (r == 0 && got && x.ngot != 1) {
        errno = EIO;

        return -1;
    }

    return r;
}

// A queue's call, its answer into out of room: the answer's size, or -1
// and errno, EBADF of what is no queue.
static ssize_t queueCall(mqd_t q, const void* in, size_t size, void* out, size_t room, int flags) {
    struct ctl c = {in, size, NULL, 0, out, room, NULL, 0};

    if (ctl(q, &c, flags) != 0) {
        if (errno == ENOTTY) {
            errno = EBADF;
        }

        return -1;
    }

    return (ssize_t)c.room;
}

mqd_t __kernel_mq_open(const char* name, int flags, va_list ap, size_t size) {
    sized("mq_open", size, sizeof(struct mq_attr));

    struct __attribute__((packed)) {
        uint32_t call;
        uint32_t flags;
        uint32_t mode;
        uint32_t maxmsg;
        uint32_t msgsize;
        char name[NAME_MAX + 1];
    } o = {ctlMqOpen, (uint32_t)flags, 0, 0, 0, {0}};
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
    int q = -1;

    memcpy(o.name, key, n + 1);

    return controlCall(&o, offsetof(__typeof__(o), name) + n + 1, &q, flags) == 0 ? q : -1;
}

int __kernel_mq_unlink(const char* name) {
    struct {
        uint32_t call;
        char name[NAME_MAX + 1];
    } o = {ctlMqUnlink, {0}};
    const char* key = queueName(name);

    if (!key) {
        return -1;
    }

    size_t n = strlen(key);

    memcpy(o.name, key, n + 1);

    return controlCall(&o, sizeof o.call + n + 1, NULL, 0);
}

// The attributes: maxmsg, msgsize, curmsgs and the description's flags.
static int attributes(mqd_t q, struct mq_attr* out) {
    const uint32_t call = ctlMqAttributes;
    uint32_t a[4];

    if (queueCall(q, &call, sizeof call, a, sizeof a, 0) != sizeof a) {
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
        return -1;
    }

    if (old) {
        *old = was;
    }

    return 0;
}

static struct Until until(const struct timespec* at) {
    struct Until u = {at != NULL, at ? at->tv_sec : 0, at ? at->tv_nsec : 0};

    return u;
}

// A message's send: its header, then the message.
static int sendOf(mqd_t q, const char* msg, size_t n, unsigned prio, const struct timespec* at) {
    struct __attribute__((packed)) {
        uint32_t call;
        uint32_t prio;
        uint32_t pid;
        uint32_t uid;
        struct Until until;
    } head = {ctlMqSend, prio, (uint32_t)getpid(), (uint32_t)getuid(), until(at)};

    if (prio >= MQ_PRIO_MAX) {
        errno = EINVAL;

        return -1;
    }

    if (n > 65536) {
        errno = EMSGSIZE;

        return -1;
    }

    char* in = malloc(sizeof head + n);

    if (!in) {
        return -1;
    }

    memcpy(in, &head, sizeof head);
    memcpy(in + sizeof head, msg, n);

    ssize_t r = queueCall(q, in, sizeof head + n, NULL, 0, CTL_RESTART);
    int saved = errno;

    free(in);
    errno = saved;

    return r < 0 ? -1 : 0;
}

int __kernel_mq_send(mqd_t q, const char* msg, size_t n, unsigned prio) {
    return sendOf(q, msg, n, prio, NULL);
}

int __kernel_mq_timedsend(mqd_t q, const char* msg, size_t n, unsigned prio, const struct timespec* at, size_t size) {
    sized("mq_timedsend", size, sizeof(struct timespec));

    return sendOf(q, msg, n, prio, at);
}

// A receive: the priority, then the message, its size the answer's.
static ssize_t receiveOf(mqd_t q, char* buf, size_t n, unsigned* prio, const struct timespec* at) {
    struct __attribute__((packed)) {
        uint32_t call;
        uint32_t room;
        struct Until until;
    } in = {ctlMqReceive, n > UINT32_MAX ? UINT32_MAX : (uint32_t)n, until(at)};
    size_t room = sizeof(uint32_t) + (n < 65536 ? n : 65536);
    char* out = malloc(room);

    if (!out) {
        return -1;
    }

    ssize_t r = queueCall(q, &in, sizeof in, out, room, CTL_RESTART);
    int saved = errno;

    if (r >= (ssize_t)sizeof(uint32_t)) {
        r -= (ssize_t)sizeof(uint32_t);
        memcpy(buf, out + sizeof(uint32_t), (size_t)r);

        if (prio) {
            memcpy(prio, out, sizeof *prio);
        }
    } else if (r >= 0) {
        r = -1;
        saved = EIO;
    }

    free(out);
    errno = saved;

    return r;
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

    uint32_t in[5] = {ctlMqUnnotify, (uint32_t)getpid(), 0, 0, 0};
    sigset_t set;

    if (!event) {
        return queueCall(q, in, 2 * sizeof *in, NULL, 0, 0) < 0 ? -1 : 0;
    }

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

    in[0] = ctlMqNotify;
    in[2] = (uint32_t)event->sigev_notify;
    in[3] = (uint32_t)event->sigev_signo;
    in[4] = (uint32_t)event->sigev_value.sival_int;

    return queueCall(q, in, sizeof in, NULL, 0, 0) < 0 ? -1 : 0;
}
