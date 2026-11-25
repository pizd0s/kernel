// ipc: POSIX's message queues and XSI's message queues and semaphore
// sets of the system, a service of rootfs's /dev/init/ctl
// (lib/protocol/service.h). An open of ipc/mq is a control
// file, whose SEND opens or unlinks a queue by its name and whose ACCEPT
// then gives a descriptor of the queue opened: its SEND a message's, its
// RECV the first of the highest priority, waiting where the queue is full
// or empty but of O_NONBLOCK or MSG_DONTWAIT, its readiness POLLIN while
// there is a message and POLLOUT while there is room. The calls and their
// bodies are lib/protocol's; who asks is the header's ids, and a message
// says who sent it. A queue unlinked stays while a descriptor of it is
// open. A notification is a signal of the system's (sigqueueinfo),
// SI_MESGQ, to the process that asked for it, of the sender's, as a
// message comes to an empty queue no one waits for, and once.
//
// An open of ipc/xsi takes a call of XSI's in its SEND and answers it to
// its RECV, which waits as long as the call does, a deadline's EAGAIN or
// a CANCEL's EINTR ending it, and EIDRM the removal of what it waits for.
// A semop of SEM_UNDO brings the process's undo file, an open of
// ipc/xsi its lib/c keeps, which the kernel's id names (fdid): its
// adjustments are that file's, and come back to the semaphores when it
// closes.
//
//     ipc [-r fd]
//
// -r: a descriptor to close once the devices are there; rootfs starts the
// shell when it has been.

#include <bin/tools/tools.h>
#include <lib/protocol/service.h>

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/ipc.h>
#include <sys/msg.h>
#include <sys/param.h>
#include <sys/sem.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

namespace {
    // What an endpoint is of: each of the structures begins with its kind.
    enum class Kind : uint8_t {
        Control,
        Description,
        Waiter,
        Xsi,
        XsiWaiter,
    };

    struct Message {
        Message* next;
        uint32_t prio;
        uint32_t size;
        unsigned char data[];
    };

    struct Description;
    struct Waiter;

    // A queue: its name, its owner and mode, its limits; its messages,
    // the highest priority first and in order within one; its open
    // descriptors and the controls that opened it, which keep it once it
    // is unlinked; the sends and receives that wait, in order; and the
    // notification asked of it, a description's.
    struct Queue {
        char name[NAME_MAX + 1];
        uint32_t mode;
        uint32_t uid;
        uint32_t gid;
        uint32_t maxmsg;
        uint32_t msgsize;
        uint64_t ino;
        uint64_t made;
        Message* messages;
        uint32_t count;
        bool linked;
        uint32_t refs;
        Description* descriptions;
        Waiter* senders;
        Waiter* receivers;
        Description* notified;
        uint32_t notifier;
        uint32_t notify;
        uint32_t signo;
        uint32_t value;
        Queue* next; // among the linked
    };

    // An open of ipc/mq: the queue its last SEND opened and how, which
    // its ACCEPT gives.
    struct Control {
        Kind kind;
        Queue* opened;
        uint32_t access;
    };

    // A descriptor of a queue: its open file's endpoint.
    struct Description {
        Kind kind;
        Queue* q;
        uint32_t access;
        Endpoint* end;
        Description* next; // among its queue's
    };

    // A SEND or a RECV kept: where it is answered, of which description;
    // a SEND's message, a RECV's room.
    struct Waiter {
        Kind kind;
        Endpoint* kept;
        Description* d;
        Message* message;
        uint32_t want;
        Waiter* next;
    };
}

constexpr uint32_t defaultMaxmsg = 10;
constexpr uint32_t defaultMsgsize = 8192;
constexpr uint32_t maxMaxmsg = 65536;
constexpr uint32_t maxMsgsize = 32768;
constexpr uint64_t maxBytes = 1 << 26;
constexpr uint32_t prioMax = 32768;

// The header of a SEND's message: its priority and who sends it.
struct Sent {
    uint32_t prio;
    uint32_t pid;
    uint32_t uid;
};

static int epoll = -1;
static Queue* queues;
static uint64_t inodes;

static uint64_t now() {
    timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return uint64_t(ts.tv_sec) * 1000000000 + uint64_t(ts.tv_nsec);
}

static Kind kindOf(const Endpoint& e) {
    return *static_cast<const Kind*>(e.of);
}

static void answerCount(int fd, uint32_t count) {
    answer(fd, opOk, 0, &count, sizeof count);
}

// The data of a SEND, after its address, which it has none of.
static bool sentData(const Request& r, const unsigned char*& data, size_t& size) {
    uint32_t length;
    if (!r.at(0, length) || r.size - 4 < length) {
        return false;
    }
    data = r.body + 4 + length;
    size = r.size - 4 - length;
    return true;
}

static Queue* named(const char* name) {
    for (Queue* q = queues; q; q = q->next) {
        if (strcmp(q->name, name) == 0) {
            return q;
        }
    }
    return nullptr;
}

// A queue let go of: it goes with its last reference once unlinked.
static void release(Queue* q) {
    if (--q->refs > 0 || q->linked) {
        return;
    }
    while (Message* m = q->messages) {
        q->messages = m->next;
        free(m);
    }
    free(q);
}

static void unlinkQueue(Queue* q) {
    for (Queue** at = &queues; *at; at = &(*at)->next) {
        if (*at == q) {
            *at = q->next;
            break;
        }
    }
    q->linked = false;
    ++q->refs;
    release(q);
}

static uint32_t eventsOf(const Queue& q) {
    return (q.count > 0 ? POLLIN : 0) | (q.count < q.maxmsg ? POLLOUT : 0);
}

// The queue's readiness, to every descriptor of it (READY).
static void ready(const Queue& q) {
    for (const Description* d = q.descriptions; d; d = d->next) {
        tell(d->end->fd, eventsOf(q));
    }
}

static bool alive(uint32_t pid) {
    return kill(pid_t(pid), 0) == 0 || errno == EPERM;
}

// A message come to an empty queue no one waits for: the notification,
// once.
static void notifyOf(Queue& q, const Sent& by) {
    if (!q.notified) {
        return;
    }
    if (q.notify == SIGEV_SIGNAL) {
        siginfo_t info{};
        info.si_signo = int(q.signo);
        info.si_code = SI_MESGQ;
        info.si_pid = pid_t(by.pid);
        info.si_uid = uid_t(by.uid);
        info.si_value.sival_int = int(q.value);
        sigqueueinfo(pid_t(q.notifier), int(q.signo), &info);
    }
    q.notified = nullptr;
}

// Puts a message among the queue's, after those of its priority.
static void enqueue(Queue& q, Message* m) {
    Message** at = &q.messages;
    while (*at && (*at)->prio >= m->prio) {
        at = &(*at)->next;
    }
    m->next = *at;
    *at = m;
    ++q.count;
}

static void unwait(Waiter*& list, Waiter* w) {
    for (Waiter** at = &list; *at; at = &(*at)->next) {
        if (*at == w) {
            *at = w->next;
            break;
        }
    }
}

// A RECV's answer: flags, no address, the priority and the message.
static void answerMessage(int fd, const Message& m) {
    static unsigned char out[12 + maxMsgsize];
    const uint32_t head[3] = {0, 0, m.prio};
    memcpy(out, head, sizeof head);
    memcpy(out + sizeof head, m.data, m.size);
    answer(fd, opOk, 0, out, sizeof head + m.size);
}

static void doneWaiting(Waiter* w) {
    unkeep(epoll, w->kept);
    free(w->message);
    free(w);
}

// The first message taken, and the first send that waits let in.
static Message* take(Queue& q) {
    Message* m = q.messages;
    q.messages = m->next;
    --q.count;
    if (Waiter* w = q.senders) {
        q.senders = w->next;
        answerCount(w->kept->fd, w->message->size + sizeof(Sent));
        enqueue(q, w->message);
        w->message = nullptr;
        doneWaiting(w);
    }
    return m;
}

static void controlOpen(Control& c, const Request& r, const unsigned char* data, size_t size) {
    struct {
        uint32_t call;
        uint32_t flags;
        uint32_t mode;
        uint32_t maxmsg;
        uint32_t msgsize;
    } o;
    if (size <= sizeof o || data[size - 1] != 0) {
        return fail(r.reply(), EINVAL);
    }
    memcpy(&o, data, sizeof o);
    const char* name = reinterpret_cast<const char*>(data + sizeof o);
    const size_t length = strlen(name);
    const uint32_t access = o.flags & O_ACCMODE;
    if (length == 0 || strchr(name, '/') || access == O_ACCMODE) {
        return fail(r.reply(), EINVAL);
    }
    if (length > NAME_MAX) {
        return fail(r.reply(), ENAMETOOLONG);
    }
    Queue* q = named(name);
    if (q && (o.flags & (O_CREAT | O_EXCL)) == (O_CREAT | O_EXCL)) {
        return fail(r.reply(), EEXIST);
    }
    if (!q && (o.flags & O_CREAT) == 0) {
        return fail(r.reply(), ENOENT);
    }
    if (q) {
        const uint32_t want = access == O_RDONLY ? 4 : access == O_WRONLY ? 2 : 6;
        const uint32_t bits = r.h.uid == q->uid ? q->mode >> 6 : r.h.gid == q->gid ? q->mode >> 3 : q->mode;
        if (r.h.uid != 0 && (bits & want) != want) {
            return fail(r.reply(), EACCES);
        }
    } else {
        const uint32_t maxmsg = o.maxmsg ? o.maxmsg : defaultMaxmsg;
        const uint32_t msgsize = o.msgsize ? o.msgsize : defaultMsgsize;
        if (maxmsg > maxMaxmsg || msgsize > maxMsgsize || uint64_t(maxmsg) * msgsize > maxBytes || int32_t(maxmsg) < 0) {
            return fail(r.reply(), EINVAL);
        }
        q = static_cast<Queue*>(calloc(1, sizeof(Queue)));
        if (!q) {
            return fail(r.reply(), ENOMEM);
        }
        memcpy(q->name, name, length + 1);
        q->mode = o.mode & 0777;
        q->uid = r.h.uid;
        q->gid = r.h.gid;
        q->maxmsg = maxmsg;
        q->msgsize = msgsize;
        q->ino = ++inodes;
        q->made = now();
        q->linked = true;
        q->next = queues;
        queues = q;
    }
    ++q->refs;
    if (c.opened) {
        release(c.opened);
    }
    c.opened = q;
    c.access = access;
    answerCount(r.reply(), uint32_t(size));
}

static void controlUnlink(const Request& r, const unsigned char* data, size_t size) {
    if (size <= 4 || data[size - 1] != 0) {
        return fail(r.reply(), EINVAL);
    }
    Queue* q = named(reinterpret_cast<const char*>(data + 4));
    if (!q) {
        return fail(r.reply(), ENOENT);
    }
    if (r.h.uid != 0 && r.h.uid != q->uid) {
        return fail(r.reply(), EACCES);
    }
    unlinkQueue(q);
    answerCount(r.reply(), uint32_t(size));
}

// The ACCEPT of a control file: a descriptor of the queue its SEND
// opened, of the access asked then and the ACCEPT's O_NONBLOCK.
static void controlAccept(Control& c, Request& r) {
    Queue* q = c.opened;
    if (!q) {
        return fail(r.reply(), EINVAL);
    }
    auto* d = static_cast<Description*>(calloc(1, sizeof(Description)));
    if (!d) {
        return fail(r.reply(), ENOMEM);
    }
    d->kind = Kind::Description;
    d->q = q;
    d->access = c.access;
    // answerOpen's flags are an OPEN's: the access, and the ACCEPT's
    // O_NONBLOCK.
    r.h.arg = (r.h.arg & O_NONBLOCK) | c.access;
    d->end = answerOpen(epoll, r, {openedFile, 0, 0, 0, eventsOf(*q), 0}, d);
    if (!d->end) {
        free(d);
        return;
    }
    c.opened = nullptr; // its reference is the description's
    d->next = q->descriptions;
    q->descriptions = d;
}

static void controlCall(Control& c, Request& r) {
    const unsigned char* data;
    size_t size;
    uint32_t call;
    switch (r.h.op) {
        case opSend:
            if (!sentData(r, data, size) || size < 4) {
                return fail(r.reply(), EINVAL);
            }
            memcpy(&call, data, sizeof call);
            if (call == mqOpen) {
                return controlOpen(c, r, data, size);
            }
            if (call == mqUnlink) {
                return controlUnlink(r, data, size);
            }
            return fail(r.reply(), EINVAL);
        case opAccept:
            return controlAccept(c, r);
        default:
            return fail(r.reply(), unserved(r.h.op));
    }
}

static void queueSend(Description& d, Request& r) {
    Queue& q = *d.q;
    const unsigned char* data;
    size_t size;
    Sent by;
    if (d.access == O_RDONLY) {
        return fail(r.reply(), EBADF);
    }
    if (!sentData(r, data, size) || size < sizeof by) {
        return fail(r.reply(), EINVAL);
    }
    memcpy(&by, data, sizeof by);
    if (size - sizeof by > q.msgsize) {
        return fail(r.reply(), EMSGSIZE);
    }
    if (by.prio >= prioMax) {
        return fail(r.reply(), EINVAL);
    }
    if (q.count >= q.maxmsg && (d.end->nonblock || (r.h.arg & MSG_DONTWAIT) != 0)) {
        return fail(r.reply(), EAGAIN);
    }
    auto* m = static_cast<Message*>(malloc(sizeof(Message) + size - sizeof by));
    if (!m) {
        return fail(r.reply(), ENOMEM);
    }
    m->prio = by.prio;
    m->size = uint32_t(size - sizeof by);
    memcpy(m->data, data + sizeof by, m->size);
    if (q.count >= q.maxmsg) {
        auto* w = static_cast<Waiter*>(calloc(1, sizeof(Waiter)));
        if (!w) {
            free(m);
            return fail(r.reply(), ENOMEM);
        }
        w->kind = Kind::Waiter;
        w->d = &d;
        w->message = m;
        w->kept = keep(epoll, r, w);
        Waiter** at = &q.senders;
        while (*at) {
            at = &(*at)->next;
        }
        *at = w;
        return;
    }
    answerCount(r.reply(), uint32_t(size));
    if (Waiter* w = q.receivers) {
        q.receivers = w->next;
        answerMessage(w->kept->fd, *m);
        free(m);
        doneWaiting(w);
        return;
    }
    enqueue(q, m);
    if (q.count == 1) {
        notifyOf(q, by);
    }
    ready(q);
}

static void queueReceive(Description& d, Request& r) {
    Queue& q = *d.q;
    uint32_t want;
    if (d.access == O_WRONLY) {
        return fail(r.reply(), EBADF);
    }
    if (!r.at(0, want)) {
        return fail(r.reply(), EINVAL);
    }
    if (want < 4 || want - 4 < q.msgsize) {
        return fail(r.reply(), EMSGSIZE);
    }
    if (q.count > 0) {
        Message* m = take(q);
        answerMessage(r.reply(), *m);
        free(m);
        ready(q);
        return;
    }
    if (d.end->nonblock || (r.h.arg & MSG_DONTWAIT) != 0) {
        return fail(r.reply(), EAGAIN);
    }
    auto* w = static_cast<Waiter*>(calloc(1, sizeof(Waiter)));
    if (!w) {
        return fail(r.reply(), ENOMEM);
    }
    w->kind = Kind::Waiter;
    w->d = &d;
    w->want = want;
    w->kept = keep(epoll, r, w);
    Waiter** at = &q.receivers;
    while (*at) {
        at = &(*at)->next;
    }
    *at = w;
}

// getsockopt of mqAttributes: maxmsg, msgsize, curmsgs and the
// description's flags.
static void queueOption(Description& d, const Request& r) {
    uint32_t level;
    uint32_t name;
    if (!r.at(0, level) || !r.at(4, name)) {
        return fail(r.reply(), EINVAL);
    }
    if (level != ipcLevel || name != mqAttributes) {
        return fail(r.reply(), ENOPROTOOPT);
    }
    const Queue& q = *d.q;
    const uint32_t attr[4] = {q.maxmsg, q.msgsize, q.count, d.end->nonblock ? uint32_t(O_NONBLOCK) : 0};
    answer(r.reply(), opOk, 0, attr, sizeof attr);
}

// setsockopt of mqNotify, pid, how, signal and value, which a process
// asks once while it lives; mqUnnotify, pid, which takes its own away.
static void queueSetOption(Description& d, const Request& r) {
    Queue& q = *d.q;
    uint32_t level;
    uint32_t name;
    uint32_t value[4] = {};
    if (!r.at(0, level) || !r.at(4, name) || !r.at(8, value[0])) {
        return fail(r.reply(), EINVAL);
    }
    if (level != ipcLevel || (name != mqNotify && name != mqUnnotify)) {
        return fail(r.reply(), ENOPROTOOPT);
    }
    if (name == mqUnnotify) {
        if (q.notified && q.notifier == value[0]) {
            q.notified = nullptr;
        }
        return answer(r.reply(), opOk, 0);
    }
    if (!r.at(8, value)) {
        return fail(r.reply(), EINVAL);
    }
    if (q.notified && alive(q.notifier)) {
        return fail(r.reply(), EBUSY);
    }
    q.notified = &d;
    q.notifier = value[0];
    q.notify = value[1];
    q.signo = value[2];
    q.value = value[3];
    answer(r.reply(), opOk, 0);
}

static void queueCall(Description& d, Request& r) {
    switch (r.h.op) {
        case opSend:
            return queueSend(d, r);
        case opRecv:
            return queueReceive(d, r);
        case opGetsockopt:
            return queueOption(d, r);
        case opSetsockopt:
            return queueSetOption(d, r);
        case opSocktype: {
            // Of a message too big for a packet: EMSGSIZE, as a datagram's.
            const uint32_t type = SOCK_SEQPACKET;
            return answer(r.reply(), opOk, 0, &type, sizeof type);
        }
        case opFstat: {
            const Queue& q = *d.q;
            const Stat st{q.ino, 0, q.made, S_IFREG | q.mode, 1, q.uid, q.gid, q.made, 0, q.made};
            return answer(r.reply(), opOk, 0, &st, sizeof st);
        }
        default:
            return fail(r.reply(), unserved(r.h.op));
    }
}

// A waiter's CANCEL: EINTR, and it waits no more.
static void cancelled(Waiter* w, const Request& r) {
    if (r.h.op != opCancel) {
        return;
    }
    Queue& q = *w->d->q;
    unwait(w->message ? q.senders : q.receivers, w);
    fail(r.reply(), EINTR);
    doneWaiting(w);
}

// ---- XSI's IPC -------------------------------------------------------------------

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

    // An open of ipc/xsi: its pair's id; the call its SEND brought, the
    // undo file's id with it; as a process's undo file, its adjustments
    // and the process's pid.
    struct Xsi {
        Kind kind;
        uint64_t id;
        unsigned char* call;
        uint32_t size;
        uint64_t undo;
        Adjust* adjusts;
        uint32_t pid;
        Xsi* next; // among all
    };

    // A RECV whose call waits: who asked it, its deadline, 0 for none, and
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
    w.sem = kind == xsiSemop;
    switch (kind) {
        case xsiMsgget:
            return msgget(h, call, size, out);
        case xsiMsgsnd:
            return msgsnd(h, call, size, out);
        case xsiMsgrcv:
            return msgrcv(h, call, size, out);
        case xsiMsgctl:
            return msgctl(h, call, size, out);
        case xsiSemget:
            return semget(h, call, size, out);
        case xsiSemop:
            return semop(h, call, size, undo, w.num, w.zero, out);
        case xsiSemctl:
            return semctl(h, call, size, out);
        default:
            return EINVAL;
    }
}

// A RECV's answer: flags, no address, the outcome.
static void answerOutcome(int fd, int e, size_t size) {
    if (e != 0) {
        return fail(fd, e);
    }
    static unsigned char out[8 + sizeof outcome];
    memset(out, 0, 8);
    memcpy(out + 8, outcome, size);
    answer(fd, opOk, 0, out, 8 + size);
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

// An ipc/xsi's call: a SEND's kept for its RECV, the undo file's id with
// it.
static void xsiCall(Xsi& x, Request& r) {
    const unsigned char* data;
    size_t size;
    switch (r.h.op) {
        case opSend: {
            if (!sentData(r, data, size) || size < 4 || r.count > 2) {
                return fail(r.reply(), EINVAL);
            }
            unsigned long long undo = 0;
            if (r.count == 2 && fdid(r.fds[0], &undo) != 0) {
                return fail(r.reply(), EBADF);
            }
            auto* call = static_cast<unsigned char*>(malloc(size));
            if (!call) {
                return fail(r.reply(), ENOMEM);
            }
            memcpy(call, data, size);
            free(x.call);
            x.call = call;
            x.size = uint32_t(size);
            x.undo = undo;
            return answerCount(r.reply(), uint32_t(size));
        }
        case opRecv: {
            if (!x.call) {
                return fail(r.reply(), EINVAL);
            }
            auto* w = static_cast<XsiWaiter*>(calloc(1, sizeof(XsiWaiter)));
            if (!w) {
                return fail(r.reply(), ENOMEM);
            }
            size_t out = 0;
            const int e = perform(r.h, x.call, x.size, x.undo, *w, out);
            if (e != waits) {
                free(w);
                answerOutcome(r.reply(), e, out);
                free(x.call);
                x.call = nullptr;
                if (e == 0) {
                    retry();
                }
                return;
            }
            SemCall c{};
            if (w->sem && x.size >= sizeof c) {
                memcpy(&c, x.call, sizeof c);
                if (c.timed) {
                    if (c.nsec < 0 || c.nsec >= 1000000000 || c.sec < 0) {
                        free(w);
                        return fail(r.reply(), EINVAL);
                    }
                    w->deadline = monotonic() + uint64_t(c.sec) * 1000000000 + uint64_t(c.nsec);
                }
            }
            w->kind = Kind::XsiWaiter;
            w->h = r.h;
            w->call = x.call;
            w->size = x.size;
            w->undo = x.undo;
            x.call = nullptr;
            w->kept = keep(epoll, r, w);
            XsiWaiter** at = &xsiWaiters;
            while (*at) {
                at = &(*at)->next;
            }
            *at = w;
            return;
        }
        default:
            return fail(r.reply(), unserved(r.h.op));
    }
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
    free(x->call);
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

static int mqTag;
static int xsiTag;

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
    if (e.channel && e.of == &xsiTag) {
        return openXsi(r);
    }
    if (e.channel) {
        auto* c = static_cast<Control*>(calloc(1, sizeof(Control)));
        if (!c) {
            return fail(r.reply(), ENOMEM);
        }
        c->kind = Kind::Control;
        if (!answerOpen(epoll, r, {openedFile, 0, 0, 0, POLLIN | POLLOUT, 0}, c)) {
            free(c);
        }
        return;
    }
    switch (kindOf(e)) {
        case Kind::Control:
            return controlCall(*static_cast<Control*>(e.of), r);
        case Kind::Description:
            return queueCall(*static_cast<Description*>(e.of), r);
        case Kind::Waiter:
            return cancelled(static_cast<Waiter*>(e.of), r);
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

// A description closed: its notification and the calls it keeps go, and
// its reference to the queue.
static void forgetWaiters(Waiter*& list, const Description* d) {
    for (Waiter *w = list, *next; w; w = next) {
        next = w->next;
        if (w->d == d) {
            unwait(list, w);
            doneWaiting(w);
        }
    }
}

static void descriptionGone(Description* d) {
    Queue* q = d->q;
    forgetWaiters(q->senders, d);
    forgetWaiters(q->receivers, d);
    if (q->notified == d) {
        q->notified = nullptr;
    }
    for (Description** at = &q->descriptions; *at; at = &(*at)->next) {
        if (*at == d) {
            *at = d->next;
            break;
        }
    }
    free(d);
    release(q);
}

// An endpoint whose other end closed; the loop closes it after.
static void gone(Endpoint& e) {
    switch (kindOf(e)) {
        case Kind::Control: {
            auto* c = static_cast<Control*>(e.of);
            if (c->opened) {
                release(c->opened);
            }
            free(c);
            return;
        }
        case Kind::Description:
            return descriptionGone(static_cast<Description*>(e.of));
        case Kind::Waiter: {
            auto* w = static_cast<Waiter*>(e.of);
            Queue& q = *w->d->q;
            unwait(w->message ? q.senders : q.receivers, w);
            free(w->message);
            free(w);
            return;
        }
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

int ipcMain(int argc, char** argv) {
    int first;
    const int ready = readyDescriptor(argc, argv, first);
    if (first != argc) {
        dprintf(STDERR_FILENO, "usage: ipc [-r fd]\n");
        return 2;
    }
    epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0) {
        fatal("ipc: epoll_create1: errno %d", errno);
    }
    const Device mq = enroll("ipc", "ipc/mq", S_IFCHR | 0666);
    watch(epoll, {mq.end, true, false, false, &mqTag});
    const Device xsi = enroll("ipc", "ipc/xsi", S_IFCHR | 0666);
    watch(epoll, {xsi.end, true, false, false, &xsiTag});
    signal(SIGPIPE, SIG_IGN);
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle, gone, nullptr, expire);
    return 0;
}
