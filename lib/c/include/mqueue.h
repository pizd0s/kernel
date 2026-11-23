#pragma once

#include <__base.h>
#include <fcntl.h>
#include <signal.h>
#include <time.h>

__BEGIN_DECLS

/*
 * POSIX message queues, the kernel's: a queue is a kernel object of a name,
 * and a message queue descriptor is a file descriptor of an open queue,
 * closed by exec, its O_NONBLOCK its own.
 */
typedef int mqd_t;

struct mq_attr {
    long mq_flags;
    long mq_maxmsg;
    long mq_msgsize;
    long mq_curmsgs;
    long __reserved[4];
};

mqd_t mq_open(const char*, int, ...);
int mq_close(mqd_t);
int mq_unlink(const char*);
int mq_send(mqd_t, const char*, size_t, unsigned);
int mq_timedsend(mqd_t, const char*, size_t, unsigned, const struct timespec*);
ssize_t mq_receive(mqd_t, char*, size_t, unsigned*);
ssize_t mq_timedreceive(mqd_t, char* __restrict, size_t, unsigned* __restrict, const struct timespec* __restrict);
int mq_getattr(mqd_t, struct mq_attr*);
int mq_setattr(mqd_t, const struct mq_attr* __restrict, struct mq_attr* __restrict);
int mq_notify(mqd_t, const struct sigevent*);

__END_DECLS
