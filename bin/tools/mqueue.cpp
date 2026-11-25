// mqueue: POSIX's message queues of the system, a service of rootfs's
// /dev/init/ctl (lib/protocol/service.h). An open of ipc/mq is a control
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
        answerSent(w->kept->fd, w->message->size + sizeof(Sent));
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
    answerSent(r.reply(), uint32_t(size));
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
    answerSent(r.reply(), uint32_t(size));
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
    answerSent(r.reply(), uint32_t(size));
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
            return controlCall(*static_cast<Control*>(e.of), r);
        case Kind::Description:
            return queueCall(*static_cast<Description*>(e.of), r);
        case Kind::Waiter:
            return cancelled(static_cast<Waiter*>(e.of), r);
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
    serve(epoll, handle, gone);
    return 0;
}
