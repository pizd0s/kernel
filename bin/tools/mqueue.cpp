// mqueue: POSIX's message queues of the system, a service of rootfs's
// /dev/init/ctl (lib/protocol/service.h), its calls ctls
// (lib/protocol/calls.h). An open of ipc/mq is a control file: its
// ctlMqOpen opens a queue by its name, or makes it, answered with a
// descriptor of it, and its ctlMqUnlink takes the name away. A queue's
// ctlMqSend is a message's, its ctlMqReceive takes the first of the
// highest priority, each waiting where the queue is full or empty but of
// O_NONBLOCK, as long as its deadline lets it, ETIMEDOUT past it; its
// readiness is POLLIN while there is a message and POLLOUT while there
// is room. Who asks is the header's ids, and a message says who sent it.
// A queue unlinked stays while a descriptor of it is open. A
// notification is a signal of the system's (sigqueueinfo), SI_MESGQ, to
// the process that asked for it, of the sender's, as a message comes to
// an empty queue no one waits for, and once.
//
//     mqueue [-r fd]
//
// -r: a descriptor to close once the device is there; rootfs starts the
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
    // descriptors, which keep it once it is unlinked; the sends and
    // receives that wait, in order; and the notification asked of it, a
    // description's.
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
        Queue* next;  // among the linked
        Queue* among; // among all, unlinked too
    };

    // An open of ipc/mq.
    struct Control {
        Kind kind;
    };

    // A descriptor of a queue: its open file's endpoint.
    struct Description {
        Kind kind;
        Queue* q;
        uint32_t access;
        Endpoint* end;
        Description* next; // among its queue's
    };

    // A send or a receive kept: where it is answered, of which
    // description; a send's message, a receive's room; its deadline, of
    // CLOCK_REALTIME, 0 for none.
    struct Waiter {
        Kind kind;
        Endpoint* kept;
        Description* d;
        Message* message;
        uint32_t want;
        uint64_t deadline;
        Waiter* next;
    };

    struct [[gnu::packed]] Open {
        uint32_t call;
        uint32_t flags;
        uint32_t mode;
        uint32_t maxmsg;
        uint32_t msgsize;
    };

    // What a send or a receive waits until: timed, then the time.
    struct [[gnu::packed]] Until {
        uint32_t timed;
        int64_t sec;
        int64_t nsec;
    };

    // A send's: its priority, who sends it, until when, then the message.
    struct [[gnu::packed]] Sent {
        uint32_t call;
        uint32_t prio;
        uint32_t pid;
        uint32_t uid;
        Until until;
    };

    // A receive's: the room for the message, until when.
    struct [[gnu::packed]] Received {
        uint32_t call;
        uint32_t room;
        Until until;
    };
}

constexpr uint32_t defaultMaxmsg = 10;
constexpr uint32_t defaultMsgsize = 8192;
constexpr uint32_t maxMaxmsg = 65536;
constexpr uint32_t maxMsgsize = 32768;
constexpr uint64_t maxBytes = 1 << 26;
constexpr uint32_t prioMax = 32768;

static int epoll = -1;
static Queue* queues;
static Queue* allQueues;
static uint64_t inodes;

static uint64_t now() {
    timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return uint64_t(ts.tv_sec) * 1000000000 + uint64_t(ts.tv_nsec);
}

static Kind kindOf(const Endpoint& e) {
    return *static_cast<const Kind*>(e.of);
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
    for (Queue** at = &allQueues; *at; at = &(*at)->among) {
        if (*at == q) {
            *at = q->among;
            break;
        }
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

// A receive's answer: the priority, then the message.
static void answerMessage(int fd, const Message& m) {
    static unsigned char out[4 + maxMsgsize];
    memcpy(out, &m.prio, sizeof m.prio);
    memcpy(out + sizeof m.prio, m.data, m.size);
    answer(fd, opOk, 0, out, sizeof m.prio + m.size);
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
        answer(w->kept->fd, opOk, 0);
        enqueue(q, w->message);
        w->message = nullptr;
        doneWaiting(w);
    }
    return m;
}

// A descriptor of a queue, answered on fd: OK 1, an open file of the
// access and of O_NONBLOCK of the flags.
static void openQueue(Queue& q, uint32_t access, uint32_t flags, const Request& r) {
    auto* d = static_cast<Description*>(calloc(1, sizeof(Description)));
    int pair[2];
    if (!d || socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, pair) != 0) {
        free(d);
        return fail(r.reply(), ENOMEM);
    }
    d->kind = Kind::Description;
    d->q = &q;
    d->access = access;
    Endpoint opened{pair[0], false, access != O_WRONLY, access != O_RDONLY, d};
    opened.nonblock = (flags & O_NONBLOCK) != 0;
    d->end = watch(epoll, opened);
    ++q.refs;
    d->next = q.descriptions;
    q.descriptions = d;
    const Header h{opOk, 1, 0, 0, 0, 0};
    const Opened what{openedFile, access, 0, 0, eventsOf(q), 0};
    const iovec parts[] = {{const_cast<Header*>(&h), sizeof h}, {const_cast<Opened*>(&what), sizeof what}};
    sendPacket(r.reply(), parts, 2, &pair[1], 1);
    close(pair[1]);
}

static void controlOpen(const Request& r) {
    Open o;
    if (r.size <= sizeof o || r.body[r.size - 1] != 0) {
        return fail(r.reply(), EINVAL);
    }
    memcpy(&o, r.body, sizeof o);
    const char* name = reinterpret_cast<const char*>(r.body + sizeof o);
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
        return openQueue(*q, access, o.flags, r);
    }
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
    q->among = allQueues;
    allQueues = q;
    openQueue(*q, access, o.flags, r);
}

static void controlUnlink(const Request& r) {
    if (r.size <= 4 || r.body[r.size - 1] != 0) {
        return fail(r.reply(), EINVAL);
    }
    Queue* q = named(reinterpret_cast<const char*>(r.body + 4));
    if (!q) {
        return fail(r.reply(), ENOENT);
    }
    if (r.h.uid != 0 && r.h.uid != q->uid) {
        return fail(r.reply(), EACCES);
    }
    unlinkQueue(q);
    answer(r.reply(), opOk, 0);
}

static void controlCall(const Request& r) {
    uint32_t call = 0;
    if (r.h.op != opCtl) {
        return fail(r.reply(), unserved(r.h.op));
    }
    r.at(0, call);
    switch (call) {
        case ctlMqOpen:
            return controlOpen(r);
        case ctlMqUnlink:
            return controlUnlink(r);
        default:
            return fail(r.reply(), ENOTTY);
    }
}

// The deadline of a call that would wait, of CLOCK_REALTIME, 0 for none,
// into at: 0, or the errno, EINVAL of a time that is none and ETIMEDOUT
// of one past.
static int deadlineOf(const Until& u, uint64_t& at) {
    at = 0;
    if (!u.timed) {
        return 0;
    }
    if (u.nsec < 0 || u.nsec >= 1000000000) {
        return EINVAL;
    }
    if (u.sec < 0) {
        return ETIMEDOUT;
    }
    at = uint64_t(u.sec) * 1000000000 + uint64_t(u.nsec);
    return at <= now() ? ETIMEDOUT : 0;
}

// A call kept in a list of waiters, last.
static void wait(Waiter*& list, Waiter* w, Request& r) {
    w->kind = Kind::Waiter;
    w->kept = keep(epoll, r, w);
    Waiter** at = &list;
    while (*at) {
        at = &(*at)->next;
    }
    *at = w;
}

static void queueSend(Description& d, Request& r) {
    Queue& q = *d.q;
    Sent by;
    if (d.access == O_RDONLY) {
        return fail(r.reply(), EBADF);
    }
    if (r.size < sizeof by) {
        return fail(r.reply(), EINVAL);
    }
    memcpy(&by, r.body, sizeof by);
    const size_t size = r.size - sizeof by;
    if (size > q.msgsize) {
        return fail(r.reply(), EMSGSIZE);
    }
    if (by.prio >= prioMax) {
        return fail(r.reply(), EINVAL);
    }
    uint64_t deadline = 0;
    if (q.count >= q.maxmsg) {
        if (d.end->nonblock) {
            return fail(r.reply(), EAGAIN);
        }
        if (const int e = deadlineOf(by.until, deadline)) {
            return fail(r.reply(), e);
        }
    }
    auto* m = static_cast<Message*>(malloc(sizeof(Message) + size));
    if (!m) {
        return fail(r.reply(), ENOMEM);
    }
    m->prio = by.prio;
    m->size = uint32_t(size);
    memcpy(m->data, r.body + sizeof by, size);
    if (q.count >= q.maxmsg) {
        auto* w = static_cast<Waiter*>(calloc(1, sizeof(Waiter)));
        if (!w) {
            free(m);
            return fail(r.reply(), ENOMEM);
        }
        w->d = &d;
        w->message = m;
        w->deadline = deadline;
        return wait(q.senders, w, r);
    }
    answer(r.reply(), opOk, 0);
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
    Received c;
    if (d.access == O_WRONLY) {
        return fail(r.reply(), EBADF);
    }
    if (r.size != sizeof c) {
        return fail(r.reply(), EINVAL);
    }
    memcpy(&c, r.body, sizeof c);
    if (c.room < q.msgsize) {
        return fail(r.reply(), EMSGSIZE);
    }
    if (q.count > 0) {
        Message* m = take(q);
        answerMessage(r.reply(), *m);
        free(m);
        ready(q);
        return;
    }
    if (d.end->nonblock) {
        return fail(r.reply(), EAGAIN);
    }
    uint64_t deadline = 0;
    if (const int e = deadlineOf(c.until, deadline)) {
        return fail(r.reply(), e);
    }
    auto* w = static_cast<Waiter*>(calloc(1, sizeof(Waiter)));
    if (!w) {
        return fail(r.reply(), ENOMEM);
    }
    w->d = &d;
    w->want = c.room;
    w->deadline = deadline;
    wait(q.receivers, w, r);
}

// A notification a process asks once while it lives: pid, how, signal
// and value; or its own taken away, pid.
static void queueNotify(Description& d, uint32_t call, const Request& r) {
    Queue& q = *d.q;
    uint32_t value[4] = {};
    if (!r.at(4, value[0])) {
        return fail(r.reply(), EINVAL);
    }
    if (call == ctlMqUnnotify) {
        if (q.notified && q.notifier == value[0]) {
            q.notified = nullptr;
        }
        return answer(r.reply(), opOk, 0);
    }
    if (!r.at(4, value)) {
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
    uint32_t call = 0;
    switch (r.h.op) {
        case opCtl:
            break;
        case opFstat: {
            const Queue& q = *d.q;
            const Stat st{q.ino, 0, q.made, S_IFREG | q.mode, 1, q.uid, q.gid, q.made, 0, q.made};
            return answer(r.reply(), opOk, 0, &st, sizeof st);
        }
        default:
            return fail(r.reply(), unserved(r.h.op));
    }
    r.at(0, call);
    switch (call) {
        case ctlMqSend:
            return queueSend(d, r);
        case ctlMqReceive:
            return queueReceive(d, r);
        case ctlMqAttributes: {
            const Queue& q = *d.q;
            const uint32_t attr[4] = {q.maxmsg, q.msgsize, q.count, d.end->nonblock ? uint32_t(O_NONBLOCK) : 0};
            return answer(r.reply(), opOk, 0, attr, sizeof attr);
        }
        case ctlMqNotify:
        case ctlMqUnnotify:
            return queueNotify(d, call, r);
        default:
            return fail(r.reply(), ENOTTY);
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

// An open of ipc/mq: a control file.
static void handle(Endpoint& e, Request& r) {
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
            return controlCall(r);
        case Kind::Description:
            return queueCall(*static_cast<Description*>(e.of), r);
        case Kind::Waiter:
            return cancelled(static_cast<Waiter*>(e.of), r);
    }
}

// The waiters of a list past their deadlines: ETIMEDOUT; the next
// deadline, the earliest, into next.
static void expireOf(Waiter*& list, uint64_t at, uint64_t& next) {
    for (Waiter *w = list, *after; w; w = after) {
        after = w->next;
        if (w->deadline == 0 || w->deadline > at) {
            next = w->deadline != 0 && (next == 0 || w->deadline < next) ? w->deadline : next;
            continue;
        }
        unwait(list, w);
        fail(w->kept->fd, ETIMEDOUT);
        doneWaiting(w);
    }
}

// The waiters past their deadlines; the time to the next, in
// milliseconds, -1 of none.
static int expire() {
    const uint64_t at = now();
    uint64_t next = 0;
    for (Queue* q = allQueues; q; q = q->among) {
        expireOf(q->senders, at, next);
        expireOf(q->receivers, at, next);
    }
    return next == 0 ? -1 : int((next - at + 999999) / 1000000);
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
        case Kind::Control:
            free(e.of);
            return;
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
    }
}

int mqueueMain(int argc, char** argv) {
    int first;
    const int ready = readyDescriptor(argc, argv, first);
    if (first != argc) {
        dprintf(STDERR_FILENO, "usage: mqueue [-r fd]\n");
        return 2;
    }
    epoll = epoll_create1(EPOLL_CLOEXEC);
    if (epoll < 0) {
        fatal("mqueue: epoll_create1: errno %d", errno);
    }
    const Device mq = enroll("mqueue", "ipc/mq", S_IFCHR | 0666);
    watch(epoll, {mq.end, true, false, false, nullptr});
    signal(SIGPIPE, SIG_IGN);
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle, gone, nullptr, expire);
    return 0;
}
