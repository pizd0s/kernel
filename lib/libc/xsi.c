/*
 * XSI's message queues and semaphore sets, xsi's (bin/tools/xsi), each
 * call a ctl (lib/protocol/calls.h) of the process's open of
 * /dev/ipc/xsi, made at its first call, and again where the program
 * closed it, which the kernel's id tells (fdid): the answer waits as long
 * as the call does; who calls is the header's ids and the pid the call
 * says. SEM_UNDO's adjustments are that open's, and come back to the
 * semaphores when it closes, as the process ends or execs; a child of
 * fork lets go of its parent's. ftok is the key of a file's device and
 * inode and the id's low byte.
 */
#include "sized.h"
#include "../protocol/calls.h"

#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ipc.h>
#include <sys/msg.h>
#include <sys/sem.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

_Static_assert(sizeof(long) == 4, "a message's mtype is the answer's");
_Static_assert(sizeof(struct sembuf) == 6, "a semop's operations are the call's");

static const char device[] = "/dev/ipc/xsi";

struct __attribute__((packed)) Perm {
    int32_t key;
    uint32_t uid;
    uint32_t gid;
    uint32_t cuid;
    uint32_t cgid;
    uint32_t mode;
    int32_t seq;
};

static int xsiFile = -1;
static unsigned long long xsiId;
static pthread_mutex_t xsiLock = PTHREAD_MUTEX_INITIALIZER;

static void forget(void) {
    if (xsiFile >= 0) {
        close(xsiFile);
        xsiFile = -1;
    }
}

// The process's open of ipc/xsi, made once, and again where the program
// closed it: a child of fork lets go of its parent's.
static int xsiOf(void) {
    static int registered;
    unsigned long long id;

    pthread_mutex_lock(&xsiLock);

    if (xsiFile < 0 || fdid(xsiFile, &id) != 0 || id != xsiId) {
        int x = open(device, O_RDWR | O_CLOEXEC);

        xsiFile = x >= 0 && fdid(x, &xsiId) == 0 ? x : -1;

        if (!registered && xsiFile >= 0) {
            registered = 1;
            pthread_atfork(NULL, NULL, forget);
        }
    }

    int x = xsiFile;

    pthread_mutex_unlock(&xsiLock);

    return x;
}

// A call of size bytes, its answer into out of room: the answer's size,
// or -1 and errno.
static ssize_t call(const void* in, size_t size, void* out, size_t room) {
    int x = xsiOf();

    if (x < 0) {
        return -1;
    }

    struct ctl c = {in, size, NULL, 0, out, room, NULL, 0};

    return ctl(x, &c, 0) < 0 ? -1 : (ssize_t)c.room;
}

// A call of two parts, joined: as call.
static ssize_t joined(const void* head, size_t size, const void* tail, size_t n, void* out, size_t room) {
    char* in = malloc(size + n);

    if (!in) {
        return -1;
    }

    memcpy(in, head, size);
    memcpy(in + size, tail, n);

    ssize_t r = call(in, size + n, out, room);
    int saved = errno;

    free(in);
    errno = saved;

    return r;
}

key_t __kernel_ftok(const char* path, int id) {
    struct stat st;

    if (stat(path, &st) != 0) {
        return -1;
    }

    return (key_t)((uint32_t)(id & 0xff) << 24 | (uint32_t)(st.st_dev & 0xff) << 16 | (uint32_t)(st.st_ino & 0xffff));
}

int __kernel_msgget(key_t key, int flags) {
    const uint32_t c[3] = {ctlMsgget, (uint32_t)key, (uint32_t)flags};
    int32_t id;

    return call(c, sizeof c, &id, sizeof id) == sizeof id ? id : -1;
}

int __kernel_semget(key_t key, int n, int flags) {
    const uint32_t c[4] = {ctlSemget, (uint32_t)key, (uint32_t)n, (uint32_t)flags};
    int32_t id;

    return call(c, sizeof c, &id, sizeof id) == sizeof id ? id : -1;
}

int __kernel_msgsnd(int id, const void* msg, size_t n, int flags) {
    struct __attribute__((packed)) {
        uint32_t call;
        int32_t id;
        uint32_t flags;
        uint32_t pid;
        int32_t mtype;
    } c = {ctlMsgsnd, id, (uint32_t)flags, (uint32_t)getpid(), 0};

    if (n > MSGMAX) {
        errno = EINVAL;

        return -1;
    }

    memcpy(&c.mtype, msg, sizeof c.mtype);

    return joined(&c, sizeof c, (const char*)msg + sizeof(long), n, NULL, 0) < 0 ? -1 : 0;
}

ssize_t __kernel_msgrcv(int id, void* msg, size_t n, long type, int flags) {
    const int32_t c[6] = {ctlMsgrcv, id, (int32_t)n, (int32_t)type, flags, (int32_t)getpid()};

    if ((int32_t)n < 0) {
        errno = EINVAL;

        return -1;
    }

    ssize_t got = call(c, sizeof c, msg, sizeof(long) + n);

    return got < 0 ? -1 : got - (ssize_t)sizeof(long);
}

static void permOf(struct ipc_perm* out, const struct Perm* m) {
    out->__key = m->key;
    out->uid = m->uid;
    out->gid = m->gid;
    out->cuid = m->cuid;
    out->cgid = m->cgid;
    out->mode = m->mode;
    out->__seq = m->seq;
}

int __kernel_msgctl(int id, int cmd, struct msqid_ds* buf, size_t size) {
    sized("msgctl", size, sizeof(struct msqid_ds));

    uint32_t c[7] = {ctlMsgctl, (uint32_t)id, (uint32_t)cmd, 0, 0, 0, 0};
    struct __attribute__((packed)) {
        struct Perm perm;
        int64_t stime;
        int64_t rtime;
        int64_t ctime;
        uint32_t qnum;
        uint32_t qbytes;
        uint32_t lspid;
        uint32_t lrpid;
        uint32_t cbytes;
    } st;

    if (cmd == IPC_SET) {
        c[3] = buf->msg_perm.uid;
        c[4] = buf->msg_perm.gid;
        c[5] = buf->msg_perm.mode;
        c[6] = (uint32_t)buf->msg_qbytes;
    }

    ssize_t got = call(c, sizeof c, &st, sizeof st);

    if (got < 0) {
        return -1;
    }

    if (cmd == IPC_STAT && got == sizeof st) {
        permOf(&buf->msg_perm, &st.perm);
        buf->msg_stime = st.stime;
        buf->msg_rtime = st.rtime;
        buf->msg_ctime = st.ctime;
        buf->msg_qnum = st.qnum;
        buf->msg_qbytes = st.qbytes;
        buf->msg_lspid = (pid_t)st.lspid;
        buf->msg_lrpid = (pid_t)st.lrpid;
        buf->__msg_cbytes = st.cbytes;
    }

    return 0;
}

// A semop of n operations, until a time from now but for null.
static int semopOf(int id, struct sembuf* ops, size_t n, const struct timespec* timeout) {
    struct __attribute__((packed)) {
        uint32_t call;
        int32_t id;
        uint32_t nops;
        uint32_t timed;
        int64_t sec;
        int64_t nsec;
        uint32_t pid;
    } c = {ctlSemop, id, (uint32_t)n, timeout != NULL, timeout ? timeout->tv_sec : 0, timeout ? timeout->tv_nsec : 0,
           (uint32_t)getpid()};

    if (n > SEMOPM) {
        errno = E2BIG;

        return -1;
    }

    return joined(&c, sizeof c, ops, n * sizeof *ops, NULL, 0) < 0 ? -1 : 0;
}

int __kernel_semop(int id, struct sembuf* ops, size_t size, size_t n) {
    sized("semop", size, sizeof(struct sembuf));

    return semopOf(id, ops, n, NULL);
}

int __kernel_semtimedop(int id, struct sembuf* ops, size_t size, size_t n, const struct timespec* timeout, size_t timeoutSize) {
    sized("semtimedop", size, sizeof(struct sembuf));
    sized("semtimedop", timeoutSize, sizeof(struct timespec));

    return semopOf(id, ops, n, timeout);
}

union semun {
    int val;
    struct semid_ds* buf;
    unsigned short* array;
};

struct __attribute__((packed)) SemCtl {
    uint32_t call;
    int32_t id;
    int32_t num;
    uint32_t cmd;
    uint32_t pid;
    int32_t value;
    uint32_t uid;
    uint32_t gid;
    uint32_t mode;
};

struct __attribute__((packed)) SemStat {
    struct Perm perm;
    int64_t otime;
    int64_t ctime;
    uint32_t nsems;
};

static int semStat(int id, struct SemStat* out) {
    struct SemCtl c = {ctlSemctl, id, 0, IPC_STAT, (uint32_t)getpid(), 0, 0, 0, 0};

    return call(&c, sizeof c, out, sizeof *out) == sizeof *out ? 0 : -1;
}

int __kernel_semctl(int id, int num, int cmd, va_list ap, size_t size) {
    sized("semctl", size, sizeof(struct semid_ds));

    struct SemCtl c = {ctlSemctl, id, num, (uint32_t)cmd, (uint32_t)getpid(), 0, 0, 0, 0};
    union semun arg = {0};
    struct SemStat st;
    int32_t value;

    switch (cmd) {
        case GETVAL:
        case GETPID:
        case GETNCNT:
        case GETZCNT:
            return call(&c, sizeof c, &value, sizeof value) == sizeof value ? value : -1;
        case IPC_RMID:
            return call(&c, sizeof c, &value, sizeof value) < 0 ? -1 : 0;
        case SETVAL:
        case GETALL:
        case SETALL:
        case IPC_STAT:
        case IPC_SET:
            arg = va_arg(ap, union semun);
            break;
        default:
            errno = EINVAL;

            return -1;
    }

    switch (cmd) {
        case SETVAL:
            c.value = arg.val;

            return call(&c, sizeof c, &value, sizeof value) < 0 ? -1 : 0;
        case IPC_SET:
            c.uid = arg.buf->sem_perm.uid;
            c.gid = arg.buf->sem_perm.gid;
            c.mode = arg.buf->sem_perm.mode;

            return call(&c, sizeof c, &value, sizeof value) < 0 ? -1 : 0;
        case IPC_STAT:
            if (semStat(id, &st) != 0) {
                return -1;
            }

            permOf(&arg.buf->sem_perm, &st.perm);
            arg.buf->sem_otime = st.otime;
            arg.buf->sem_ctime = st.ctime;
            arg.buf->sem_nsems = (unsigned short)st.nsems;

            return 0;
    }

    // GETALL and SETALL: as many values as the set has.
    if (semStat(id, &st) != 0) {
        return -1;
    }

    if (cmd == GETALL) {
        return call(&c, sizeof c, arg.array, st.nsems * sizeof *arg.array) < 0 ? -1 : 0;
    }

    return joined(&c, sizeof c, arg.array, st.nsems * sizeof *arg.array, NULL, 0) < 0 ? -1 : 0;
}
