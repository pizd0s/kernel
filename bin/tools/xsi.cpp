// xsi: XSI's message queues and semaphore sets of the system, a service
// of rootfs's /dev/init/ctl (lib/protocol/service.h), its calls ctls
// (lib/protocol/calls.h). An open of ipc/xsi takes a call of XSI's,
// answered once it is done, which waits as long as the call does, a
// deadline's EAGAIN or a CANCEL's EINTR ending it, and EIDRM the removal
// of what it waits for. Who calls is the header's ids, and the pid the
// call says. A process makes its calls on an open of its own, which its
// lib/c keeps: a semop's adjustments of SEM_UNDO are that open's, and
// come back to the semaphores when it closes.
//
//     xsi [-r fd]
//
// -r: a descriptor to close once the device is there; rootfs starts the
// shell when it has been.

#include <bin/tools/tools.h>
#include <lib/protocol/service.h>

#include <errno.h>
#include <poll.h>
#include <signal.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/ipc.h>
#include <sys/msg.h>
#include <sys/param.h>
#include <sys/sem.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

namespace {
    // What an endpoint is of: each of the structures begins with its kind.
    enum class Kind : uint8_t {
        Xsi,
        XsiWaiter,
    };
}

static int epoll = -1;

static Kind kindOf(const Endpoint& e) {
    return *static_cast<const Kind*>(e.of);
}

namespace {
    struct Perm {
        int32_t key;
        uint32_t uid;
        uint32_t gid;
        uint32_t cuid;
        uint32_t cgid;
        uint32_t mode;
        int32_t seq;
    };

    struct Text {
        Text* next;
        int32_t mtype;
        uint32_t size;
        unsigned char data[];
    };

    struct MsgQueue {
        int32_t id;
        Perm perm;
        Text* first;
        uint32_t qnum;
        uint32_t cbytes;
        uint32_t qbytes;
        uint32_t lspid;
        uint32_t lrpid;
        int64_t stime;
        int64_t rtime;
        int64_t ctime;
        MsgQueue* next;
    };

    struct SemSet {
        int32_t id;
        Perm perm;
        uint32_t nsems;
        int32_t* values;
        uint32_t* pids;
        int64_t otime;
        int64_t ctime;
        SemSet* next;
    };

    // SEM_UNDO's adjustments of a set, an undo file's.
    struct Adjust {
        SemSet* set;
        Adjust* next;
        int32_t by[];
    };

    // An open of ipc/xsi, a process's: its pair's id, SEM_UNDO's
    // adjustments of its semops and the process's pid.
    struct Xsi {
        Kind kind;
        uint64_t id;
        Adjust* adjusts;
        uint32_t pid;
        Xsi* next; // among all
    };

    // A CTL whose call waits: who asked it, its deadline, 0 for none, and
    // what it waits for, a semaphore's of a set, to grow or be 0.
    struct XsiWaiter {
        Kind kind;
        Endpoint* kept;
        Header h;
        unsigned char* call;
        uint32_t size;
        uint64_t undo;
        uint64_t deadline;
        bool sem;
        int32_t on;
        uint32_t num;
        bool zero;
        XsiWaiter* next;
    };

    struct [[gnu::packed]] MsgStat {
        Perm perm;
        int64_t stime;
        int64_t rtime;
        int64_t ctime;
        uint32_t qnum;
        uint32_t qbytes;
        uint32_t lspid;
        uint32_t lrpid;
        uint32_t cbytes;
    };

    struct [[gnu::packed]] SemStat {
        Perm perm;
        int64_t otime;
        int64_t ctime;
        uint32_t nsems;
    };
}

constexpr int ipcMax = 32000;
constexpr int waits = -1; // a call's outcome: it waits

static MsgQueue* msgQueues;
static SemSet* semSets;
static int32_t msgIds;
static int32_t semIds;
static Xsi* xsis;
static XsiWaiter* xsiWaiters;
static unsigned char outcome[65536];

static int64_t seconds() {
    return int64_t(time(nullptr));
}

static uint64_t monotonic() {
    timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return uint64_t(ts.tv_sec) * 1000000000 + uint64_t(ts.tv_nsec);
}

static bool owned(const Perm& m, const Header& h) {
    return h.uid == 0 || h.uid == m.uid || h.uid == m.cuid;
}

static bool grants(const Perm& m, const Header& h, uint32_t want) {
    uint32_t bits = m.mode;
    if (h.uid == 0) {
        return true;
    }
    if (h.uid == m.uid || h.uid == m.cuid) {
        bits >>= 6;
    } else if (h.gid == m.gid || h.gid == m.cgid) {
        bits >>= 3;
    }
    return (bits & want) == want;
}

static MsgQueue* msgQueueOf(int32_t id) {
    for (MsgQueue* q = msgQueues; q; q = q->next) {
        if (q->id == id) {
            return q;
        }
    }
    return nullptr;
}

static SemSet* semSetOf(int32_t id) {
    for (SemSet* s = semSets; s; s = s->next) {
        if (s->id == id) {
            return s;
        }
    }
    return nullptr;
}

static Xsi* xsiOf(uint64_t id) {
    for (Xsi* x = xsis; x; x = x->next) {
        if (x->id == id) {
            return x;
        }
    }
    return nullptr;
}

static int count(const void* list, size_t next) {
    int n = 0;
    for (const void* at = list; at; at = *reinterpret_cast<void* const*>(static_cast<const char*>(at) + next)) {
        ++n;
    }
    return n;
}

// The id of a key's object of a kind, or of a new one: the found's
// permission and fits, or made's errno.
template <typename T>
static int getOf(T*& list, int32_t& ids, const Header& h, int32_t key, uint32_t flags, int (*fits)(const T&), int (*made)(T&),
                 int32_t& out) {
    if (key != IPC_PRIVATE) {
        for (T* o = list; o; o = o->next) {
            if (o->perm.key != key) {
                continue;
            }
            if ((flags & (IPC_CREAT | IPC_EXCL)) == (IPC_CREAT | IPC_EXCL)) {
                return EEXIST;
            }
            if (!grants(o->perm, h, (flags >> 6 | flags >> 3 | flags) & 7)) {
                return EACCES;
            }
            out = o->id;
            return fits(*o);
        }
        if ((flags & IPC_CREAT) == 0) {
            return ENOENT;
        }
    }
    if (count(list, offsetof(T, next)) >= ipcMax) {
        return ENOSPC;
    }
    auto* o = static_cast<T*>(calloc(1, sizeof(T)));
    if (!o) {
        return ENOMEM;
    }
    o->perm = {key, h.uid, h.gid, h.uid, h.gid, flags & 0777, 0};
    o->ctime = seconds();
    if (const int e = made(*o)) {
        free(o);
        return e;
    }
    o->id = ids++;
    o->next = list;
    list = o;
    out = o->id;
    return 0;
}

static int msgFits(const MsgQueue&) {
    return 0;
}

static int msgMade(MsgQueue& q) {
    q.qbytes = MSGMNB;
    return 0;
}

static uint32_t wantedSems;

static int semFits(const SemSet& s) {
    return wantedSems > s.nsems ? EINVAL : 0;
}

static int semMade(SemSet& s) {
    if (wantedSems == 0) {
        return EINVAL;
    }
    s.nsems = wantedSems;
    s.values = static_cast<int32_t*>(calloc(wantedSems, sizeof(int32_t)));
    s.pids = static_cast<uint32_t*>(calloc(wantedSems, sizeof(uint32_t)));
    if (!s.values || !s.pids) {
        free(s.values);
        free(s.pids);
        return ENOMEM;
    }
    return 0;
}

static int answerId(int32_t id, size_t& size) {
    memcpy(outcome, &id, sizeof id);
    size = sizeof id;
    return 0;
}

static int msgget(const Header& h, const unsigned char* call, size_t size, size_t& out) {
    struct {
        uint32_t call;
        int32_t key;
        uint32_t flags;
    } c;
    if (size != sizeof c) {
        return EINVAL;
    }
    memcpy(&c, call, sizeof c);
    int32_t id = 0;
    const int e = getOf(msgQueues, msgIds, h, c.key, c.flags, msgFits, msgMade, id);
    return e != 0 ? e : answerId(id, out);
}

static int semget(const Header& h, const unsigned char* call, size_t size, size_t& out) {
    struct {
        uint32_t call;
        int32_t key;
        int32_t nsems;
        uint32_t flags;
    } c;
    if (size != sizeof c) {
        return EINVAL;
    }
    memcpy(&c, call, sizeof c);
    if (c.nsems < 0 || c.nsems > SEMMSL) {
        return EINVAL;
    }
    wantedSems = uint32_t(c.nsems);
    int32_t id = 0;
    const int e = getOf(semSets, semIds, h, c.key, c.flags, semFits, semMade, id);
    return e != 0 ? e : answerId(id, out);
}

struct [[gnu::packed]] MsgSend {
    uint32_t call;
    int32_t id;
    uint32_t flags;
    uint32_t pid;
    int32_t mtype;
};

static int msgsnd(const Header& h, const unsigned char* call, size_t size, size_t& out) {
    MsgSend c;
    if (size < sizeof c) {
        return EINVAL;
    }
    memcpy(&c, call, sizeof c);
    MsgQueue* q = msgQueueOf(c.id);
    const size_t n = size - sizeof c;
    if (!q) {
        return EINVAL;
    }
    if (n > MSGMAX || c.mtype < 1) {
        return EINVAL;
    }
    if (!grants(q->perm, h, 2)) {
        return EACCES;
    }
    if (q->cbytes + n > q->qbytes || q->qnum >= q->qbytes) {
        return (c.flags & IPC_NOWAIT) != 0 ? EAGAIN : waits;
    }
    auto* m = static_cast<Text*>(malloc(sizeof(Text) + n));
    if (!m) {
        return ENOMEM;
    }
    m->next = nullptr;
    m->mtype = c.mtype;
    m->size = uint32_t(n);
    memcpy(m->data, call + sizeof c, n);
    Text** at = &q->first;
    while (*at) {
        at = &(*at)->next;
    }
    *at = m;
    ++q->qnum;
    q->cbytes += uint32_t(n);
    q->lspid = c.pid;
    q->stime = seconds();
    out = 0;
    return 0;
}

static int msgrcv(const Header& h, const unsigned char* call, size_t size, size_t& out) {
    struct {
        uint32_t call;
        int32_t id;
        int32_t room;
        int32_t mtype;
        uint32_t flags;
        uint32_t pid;
    } c;
    if (size != sizeof c) {
        return EINVAL;
    }
    memcpy(&c, call, sizeof c);
    MsgQueue* q = msgQueueOf(c.id);
    if (!q || c.room < 0) {
        return EINVAL;
    }
    if (!grants(q->perm, h, 4)) {
        return EACCES;
    }
    Text** found = nullptr;
    for (Text** at = &q->first; *at; at = &(*at)->next) {
        const int32_t t = (*at)->mtype;
        if (c.mtype == 0 || t == c.mtype) {
            found = at;
            break;
        }
        if (c.mtype < 0 && t <= -c.mtype && (!found || t < (*found)->mtype)) {
            found = at;
        }
    }
    if (!found) {
        return (c.flags & IPC_NOWAIT) != 0 ? ENOMSG : waits;
    }
    Text* m = *found;
    if (m->size > uint32_t(c.room) && (c.flags & MSG_NOERROR) == 0) {
        return E2BIG;
    }
    const uint32_t n = MIN(m->size, uint32_t(c.room));
    memcpy(outcome, &m->mtype, sizeof m->mtype);
    memcpy(outcome + sizeof m->mtype, m->data, n);
    out = sizeof m->mtype + n;
    *found = m->next;
    --q->qnum;
    q->cbytes -= m->size;
    q->lrpid = c.pid;
    q->rtime = seconds();
    free(m);
    return 0;
}

// A removed object's waiters end, EIDRM.
static void removedFor(bool sem, int32_t id) {
    for (XsiWaiter** at = &xsiWaiters; *at;) {
        XsiWaiter* w = *at;
        if (w->sem != sem || w->on != id) {
            at = &w->next;
            continue;
        }
        *at = w->next;
        fail(w->kept->fd, EIDRM);
        unkeep(epoll, w->kept);
        free(w->call);
        free(w);
    }
}

static int msgctl(const Header& h, const unsigned char* call, size_t size, size_t& out) {
    struct {
        uint32_t call;
        int32_t id;
        uint32_t cmd;
        uint32_t uid;
        uint32_t gid;
        uint32_t mode;
        uint32_t qbytes;
    } c;
    if (size != sizeof c) {
        return EINVAL;
    }
    memcpy(&c, call, sizeof c);
    MsgQueue* q = msgQueueOf(c.id);
    if (!q) {
        return EINVAL;
    }
    switch (c.cmd) {
        case IPC_STAT: {
            if (!grants(q->perm, h, 4)) {
                return EACCES;
            }
            const MsgStat st{q->perm, q->stime, q->rtime, q->ctime, q->qnum, q->qbytes, q->lspid, q->lrpid, q->cbytes};
            memcpy(outcome, &st, sizeof st);
            out = sizeof st;
            return 0;
        }
        case IPC_SET:
            if (!owned(q->perm, h) || (c.qbytes > q->qbytes && c.qbytes > MSGMNB && h.uid != 0)) {
                return EPERM;
            }
            q->perm.uid = c.uid;
            q->perm.gid = c.gid;
            q->perm.mode = c.mode & 0777;
            q->qbytes = c.qbytes;
            q->ctime = seconds();
            out = 0;
            return 0;
        case IPC_RMID:
            if (!owned(q->perm, h)) {
                return EPERM;
            }
            for (MsgQueue** at = &msgQueues; *at; at = &(*at)->next) {
                if (*at == q) {
                    *at = q->next;
                    break;
                }
            }
            while (Text* m = q->first) {
                q->first = m->next;
                free(m);
            }
            removedFor(false, q->id);
            free(q);
            out = 0;
            return 0;
        default:
            return EINVAL;
    }
}

static Adjust* adjustOf(Xsi& undo, SemSet& s) {
    for (Adjust* a = undo.adjusts; a; a = a->next) {
        if (a->set == &s) {
            return a;
        }
    }
    auto* a = static_cast<Adjust*>(calloc(1, sizeof(Adjust) + s.nsems * sizeof(int32_t)));
    if (a) {
        a->set = &s;
        a->next = undo.adjusts;
        undo.adjusts = a;
    }
    return a;
}

struct [[gnu::packed]] SemCall {
    uint32_t call;
    int32_t id;
    uint32_t nops;
    uint32_t timed;
    int64_t sec;
    int64_t nsec;
    uint32_t pid;
};

struct [[gnu::packed]] SemOperation {
    uint16_t num;
    int16_t op;
    int16_t flg;
};

// A semop: all of its operations, or, of one that cannot be done, none,
// waiting for that one's semaphore (num, zero) but of IPC_NOWAIT.
static int semop(const Header& h, const unsigned char* call, size_t size, uint64_t undo, uint32_t& num, bool& zero, size_t& out) {
    SemCall c;
    static SemOperation ops[SEMOPM];
    static int32_t values[SEMMSL];
    if (size < sizeof c) {
        return EINVAL;
    }
    memcpy(&c, call, sizeof c);
    SemSet* s = semSetOf(c.id);
    if (!s || c.nops == 0) {
        return EINVAL;
    }
    if (c.nops > SEMOPM) {
        return E2BIG;
    }
    if (size != sizeof c + c.nops * sizeof(SemOperation)) {
        return EINVAL;
    }
    memcpy(ops, call + sizeof c, c.nops * sizeof(SemOperation));
    bool alters = false;
    for (uint32_t i = 0; i < c.nops; ++i) {
        if (ops[i].num >= s->nsems) {
            return EFBIG;
        }
        alters = alters || ops[i].op != 0;
    }
    if (!grants(s->perm, h, alters ? 2 : 4)) {
        return EACCES;
    }
    memcpy(values, s->values, s->nsems * sizeof(int32_t));
    for (uint32_t i = 0; i < c.nops; ++i) {
        const SemOperation& o = ops[i];
        const int32_t v = values[o.num] + o.op;
        if (o.op > 0 && v > SEMVMX) {
            return ERANGE;
        }
        if ((o.op < 0 && v < 0) || (o.op == 0 && values[o.num] != 0)) {
            if ((o.flg & IPC_NOWAIT) != 0) {
                return EAGAIN;
            }
            num = o.num;
            zero = o.op == 0;
            return waits;
        }
        values[o.num] = v;
    }
    Xsi* keeper = undo ? xsiOf(undo) : nullptr;
    for (uint32_t i = 0; i < c.nops; ++i) {
        const SemOperation& o = ops[i];
        s->values[o.num] = values[o.num];
        s->pids[o.num] = c.pid;
        if ((o.flg & SEM_UNDO) != 0 && o.op != 0 && keeper) {
            if (Adjust* a = adjustOf(*keeper, *s)) {
                a->by[o.num] -= o.op;
                keeper->pid = c.pid;
            }
        }
    }
    s->otime = seconds();
    out = 0;
    return 0;
}

// A set's semaphore set, SETVAL's and SETALL's: the adjustments of it
// gone.
static void setValue(SemSet& s, uint32_t num, int32_t value, uint32_t pid) {
    s.values[num] = value;
    s.pids[num] = pid;
    for (Xsi* x = xsis; x; x = x->next) {
        for (Adjust* a = x->adjusts; a; a = a->next) {
            if (a->set == &s) {
                a->by[num] = 0;
            }
        }
    }
    s.ctime = seconds();
}

static int waitingFor(const SemSet& s, uint32_t num, bool zero) {
    int n = 0;
    for (const XsiWaiter* w = xsiWaiters; w; w = w->next) {
        n += w->sem && w->on == s.id && w->num == num && w->zero == zero;
    }
    return n;
}

static void removeSet(SemSet* s) {
    for (SemSet** at = &semSets; *at; at = &(*at)->next) {
        if (*at == s) {
            *at = s->next;
            break;
        }
    }
    for (Xsi* x = xsis; x; x = x->next) {
        for (Adjust** at = &x->adjusts; *at;) {
            Adjust* a = *at;
            if (a->set == s) {
                *at = a->next;
                free(a);
            } else {
                at = &a->next;
            }
        }
    }
    removedFor(true, s->id);
    free(s->values);
    free(s->pids);
    free(s);
}

static int semctl(const Header& h, const unsigned char* call, size_t size, size_t& out) {
    struct [[gnu::packed]] {
        uint32_t call;
        int32_t id;
        int32_t num;
        uint32_t cmd;
        uint32_t pid;
        int32_t value;
        uint32_t uid;
        uint32_t gid;
        uint32_t mode;
    } c;
    if (size < sizeof c) {
        return EINVAL;
    }
    memcpy(&c, call, sizeof c);
    SemSet* s = semSetOf(c.id);
    if (!s) {
        return EINVAL;
    }
    const bool one = c.cmd == GETVAL || c.cmd == GETPID || c.cmd == GETNCNT || c.cmd == GETZCNT || c.cmd == SETVAL;
    if (one && (c.num < 0 || uint32_t(c.num) >= s->nsems)) {
        return EINVAL;
    }
    out = 0;
    switch (c.cmd) {
        case SETVAL:
            if (c.value < 0 || c.value > SEMVMX) {
                return ERANGE;
            }
            if (!grants(s->perm, h, 2)) {
                return EACCES;
            }
            setValue(*s, uint32_t(c.num), c.value, c.pid);
            return 0;
        case SETALL: {
            if (size != sizeof c + s->nsems * sizeof(uint16_t)) {
                return EINVAL;
            }
            const unsigned char* values = call + sizeof c;
            for (uint32_t i = 0; i < s->nsems; ++i) {
                uint16_t v;
                memcpy(&v, values + 2 * i, sizeof v);
                if (v > SEMVMX) {
                    return ERANGE;
                }
            }
            if (!grants(s->perm, h, 2)) {
                return EACCES;
            }
            for (uint32_t i = 0; i < s->nsems; ++i) {
                uint16_t v;
                memcpy(&v, values + 2 * i, sizeof v);
                setValue(*s, i, v, c.pid);
            }
            return 0;
        }
        case IPC_SET:
            if (!owned(s->perm, h)) {
                return EPERM;
            }
            s->perm.uid = c.uid;
            s->perm.gid = c.gid;
            s->perm.mode = c.mode & 0777;
            s->ctime = seconds();
            return 0;
        case IPC_RMID:
            if (!owned(s->perm, h)) {
                return EPERM;
            }
            removeSet(s);
            return 0;
        case GETVAL:
        case GETPID:
        case GETNCNT:
        case GETZCNT:
        case GETALL:
        case IPC_STAT:
            break;
        default:
            return EINVAL;
    }
    if (!grants(s->perm, h, 4)) {
        return EACCES;
    }
    const uint32_t n = uint32_t(c.num);
    int32_t value = 0;
    switch (c.cmd) {
        case GETVAL:
            value = s->values[n];
            break;
        case GETPID:
            value = int32_t(s->pids[n]);
            break;
        case GETNCNT:
            value = waitingFor(*s, n, false);
            break;
        case GETZCNT:
            value = waitingFor(*s, n, true);
            break;
        case GETALL:
            for (uint32_t i = 0; i < s->nsems; ++i) {
                const uint16_t v = uint16_t(s->values[i]);
                memcpy(outcome + 2 * i, &v, sizeof v);
            }
            out = 2 * s->nsems;
            return 0;
        default: {
            const SemStat st{s->perm, s->otime, s->ctime, s->nsems};
            memcpy(outcome, &st, sizeof st);
            out = sizeof st;
            return 0;
        }
    }
    memcpy(outcome, &value, sizeof value);
    out = sizeof value;
    return 0;
}

// A call of XSI's: its answer in outcome, out bytes of it, an errno, or
// waits, with what it waits for.
static int perform(const Header& h, const unsigned char* call, size_t size, uint64_t undo, XsiWaiter& w, size_t& out) {
    uint32_t kind = 0;
    if (size >= sizeof kind) {
        memcpy(&kind, call, sizeof kind);
    }
    int32_t id = 0;
    if (size >= 8) {
        memcpy(&id, call + 4, sizeof id);
    }
    w.on = id;
    w.sem = kind == ctlSemop;
    switch (kind) {
        case ctlMsgget:
            return msgget(h, call, size, out);
        case ctlMsgsnd:
            return msgsnd(h, call, size, out);
        case ctlMsgrcv:
            return msgrcv(h, call, size, out);
        case ctlMsgctl:
            return msgctl(h, call, size, out);
        case ctlSemget:
            return semget(h, call, size, out);
        case ctlSemop:
            return semop(h, call, size, undo, w.num, w.zero, out);
        case ctlSemctl:
            return semctl(h, call, size, out);
        default:
            return EINVAL;
    }
}

// A call's answer: the outcome, or the errno.
static void answerOutcome(int fd, int e, size_t size) {
    if (e != 0) {
        return fail(fd, e);
    }
    answer(fd, opOk, 0, outcome, size);
}

// The waiters tried again, in order, until none can go on: after a
// change of what they wait for.
static void retry() {
    for (bool progress = true; progress;) {
        progress = false;
        for (XsiWaiter** at = &xsiWaiters; *at;) {
            XsiWaiter* w = *at;
            size_t size = 0;
            const int e = perform(w->h, w->call, w->size, w->undo, *w, size);
            if (e == waits) {
                at = &w->next;
                continue;
            }
            *at = w->next;
            answerOutcome(w->kept->fd, e, size);
            unkeep(epoll, w->kept);
            free(w->call);
            free(w);
            progress = true;
        }
    }
}

// An ipc/xsi's CTL: its call, done or kept until it can be, a
// semop's adjustments of SEM_UNDO the file's.
static void xsiCall(Xsi& x, Request& r) {
    if (r.h.op != opCtl) {
        return fail(r.reply(), unserved(r.h.op));
    }
    auto* w = static_cast<XsiWaiter*>(calloc(1, sizeof(XsiWaiter)));
    if (!w) {
        return fail(r.reply(), ENOMEM);
    }
    size_t out = 0;
    const int e = perform(r.h, r.body, r.size, x.id, *w, out);
    if (e != waits) {
        free(w);
        answerOutcome(r.reply(), e, out);
        if (e == 0) {
            retry();
        }
        return;
    }
    SemCall c{};
    if (w->sem && r.size >= sizeof c) {
        memcpy(&c, r.body, sizeof c);
        if (c.timed) {
            if (c.nsec < 0 || c.nsec >= 1000000000 || c.sec < 0) {
                free(w);
                return fail(r.reply(), EINVAL);
            }
            w->deadline = monotonic() + uint64_t(c.sec) * 1000000000 + uint64_t(c.nsec);
        }
    }
    w->call = static_cast<unsigned char*>(malloc(r.size));
    if (!w->call) {
        free(w);
        return fail(r.reply(), ENOMEM);
    }
    memcpy(w->call, r.body, r.size);
    w->kind = Kind::XsiWaiter;
    w->h = r.h;
    w->size = uint32_t(r.size);
    w->undo = x.id;
    w->kept = keep(epoll, r, w);
    XsiWaiter** at = &xsiWaiters;
    while (*at) {
        at = &(*at)->next;
    }
    *at = w;
}

static void unwaitXsi(XsiWaiter* w) {
    for (XsiWaiter** at = &xsiWaiters; *at; at = &(*at)->next) {
        if (*at == w) {
            *at = w->next;
            break;
        }
    }
}

// An ipc/xsi closed: as a process's undo file, its adjustments come back
// to the semaphores, as far as 0 and SEMVMX.
static void xsiGone(Xsi* x) {
    bool changed = false;
    while (Adjust* a = x->adjusts) {
        x->adjusts = a->next;
        for (uint32_t i = 0; i < a->set->nsems; ++i) {
            if (a->by[i] != 0) {
                a->set->values[i] = MIN(MAX(a->set->values[i] + a->by[i], 0), SEMVMX);
                a->set->pids[i] = x->pid;
                changed = true;
            }
        }
        free(a);
    }
    for (Xsi** at = &xsis; *at; at = &(*at)->next) {
        if (*at == x) {
            *at = x->next;
            break;
        }
    }
    free(x);
    if (changed) {
        retry();
    }
}

// The waiters past their deadlines: EAGAIN; the time to the next, in
// milliseconds, -1 of none.
static int expire() {
    const uint64_t at = monotonic();
    uint64_t next = 0;
    for (XsiWaiter** p = &xsiWaiters; *p;) {
        XsiWaiter* w = *p;
        if (w->deadline == 0 || w->deadline > at) {
            if (w->deadline != 0 && (next == 0 || w->deadline < next)) {
                next = w->deadline;
            }
            p = &w->next;
            continue;
        }
        *p = w->next;
        fail(w->kept->fd, EAGAIN);
        unkeep(epoll, w->kept);
        free(w->call);
        free(w);
    }
    return next == 0 ? -1 : int((next - at + 999999) / 1000000);
}

// An open of ipc/xsi, its id its pair's.
static void openXsi(const Request& r) {
    auto* x = static_cast<Xsi*>(calloc(1, sizeof(Xsi)));
    if (!x) {
        return fail(r.reply(), ENOMEM);
    }
    x->kind = Kind::Xsi;
    const Endpoint* e = answerOpen(epoll, r, {openedFile, 0, 0, 0, POLLIN | POLLOUT, 0}, x);
    unsigned long long id = 0;
    if (!e || fdid(e->fd, &id) != 0) {
        free(x);
        return;
    }
    x->id = id;
    x->next = xsis;
    xsis = x;
}

static void handle(Endpoint& e, Request& r) {
    if (e.channel) {
        return openXsi(r);
    }
    switch (kindOf(e)) {
        case Kind::Xsi:
            return xsiCall(*static_cast<Xsi*>(e.of), r);
        case Kind::XsiWaiter: {
            auto* w = static_cast<XsiWaiter*>(e.of);
            if (r.h.op == opCancel) {
                unwaitXsi(w);
                fail(r.reply(), EINTR);
                unkeep(epoll, w->kept);
                free(w->call);
                free(w);
            }
            return;
        }
    }
}

// An endpoint whose other end closed; the loop closes it after.
static void gone(Endpoint& e) {
    switch (kindOf(e)) {
        case Kind::Xsi:
            return xsiGone(static_cast<Xsi*>(e.of));
        case Kind::XsiWaiter: {
            auto* w = static_cast<XsiWaiter*>(e.of);
            unwaitXsi(w);
            free(w->call);
            free(w);
            return;
        }
    }
}

int xsiMain(int argc, char** argv) {
    int first;
    const int ready = readyDescriptor(argc, argv, first);
    if (first != argc) {
        dprintf(STDERR_FILENO, "usage: xsi [-r fd]\n");
        return 2;
    }
    epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0) {
        fatal("xsi: epoll_create1: errno %d", errno);
    }
    const Device xsi = enroll("xsi", "ipc/xsi", S_IFCHR | 0666);
    watch(epoll, {xsi.end, true, false, false, nullptr});
    signal(SIGPIPE, SIG_IGN);
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle, gone, nullptr, expire);
    return 0;
}
