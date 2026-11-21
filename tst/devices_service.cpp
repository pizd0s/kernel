// A service of one device for tst/test_devices.py: it registers name at
// rootfs's /dev/init with the mode given in octal, says so, and serves the
// device in a child whose pid is /run/<name>.pid, a "/" of the name "_"
// there: the device's bytes are
// the text given, read at any offset into the reader's memory, through the
// memfd a READ brings. ro registers on an endpoint of /dev/init open to
// read only.
//
// wait makes the device a mailbox whose reads wait: a READ that finds no
// mail is held until a WRITE brings some, which goes to it, and a CANCEL
// ends it with EINTR; a WRITE that finds no reader leaves mail. Its open
// files tell their readiness: readable while there is mail; a READ that
// must not wait finds EAGAIN.
//
// keep makes the device's reads keep the memfd they brought, after their
// answer: a WRITE then writes the text into every memfd kept, and is EIO
// when that fails.
//
// copy reads n bytes of a device at off into a memfd of 16 bytes of its
// memory, at to, with copy_file_range, and prints the count, the offsets
// after it and the bytes; put writes a text from a memfd of its memory to
// a device with copy_file_range and prints the count. wait reads a
// mailbox with a SIGALRM due in a second, its handler SA_RESTART or not,
// and prints what the read gave and whether the handler ran. pass makes a
// memfd of 16 bytes of its memory, and a child of it, which has the memfd
// with the fork, copies a device's bytes into it with copy_file_range and
// prints the count; it waits for the child and ends, the memfd's memory
// with it as far as it is its own.
//
//     devices_service <name> <mode> <text> [ro|wait|keep]
//     devices_service copy <device> <off> <to> <n>
//     devices_service put <device> <text>
//     devices_service wait <device> <restart>
//     devices_service pass <device>

#include <lib/protocol/service.h>

#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/mman.h>
#include <sys/param.h>
#include <sys/wait.h>
#include <unistd.h>

static const char* text;
static size_t length;
static Device device;
static int epoll = -1;

// The mailbox's: the mail a WRITE left, and the READ that waits for some,
// its endpoint, the memfd of the reader's memory and the length.
static bool mailbox = false;
static char mail[256];
static size_t mailLength = 0;

namespace {
    struct Reader {
        Endpoint* at;
        int memfd;
        uint32_t length;
    };
}

static Reader reader{};
static int readerTag;

// The memfds the reads of a device that keeps them brought.
static bool keeping = false;
static int kept[16];
static size_t keptCount = 0;

// The endpoints of the mailbox's open files, which hear its readiness.
static int opens[16];
static size_t openCount = 0;

static void tellAll() {
    const uint32_t events = POLLOUT | (mailLength > 0 ? POLLIN : 0);
    for (size_t i = 0; i < openCount; ++i) {
        tell(opens[i], events);
    }
}

static void deliver(int memfd, uint32_t room, int to) {
    const size_t part = MIN(size_t(room), mailLength);
    pwrite(memfd, mail, part, 0);
    memmove(mail, mail + part, mailLength - part);
    mailLength -= part;
    const uint32_t done = uint32_t(part);
    answer(to, opOk, 0, &done, sizeof done);
    tellAll();
}

static void letReaderGo() {
    close(reader.memfd);
    unkeep(epoll, reader.at);
    reader = {};
}

static void readMail(const Endpoint& e, Request& r) {
    Io io;
    if (!e.reads) {
        return fail(r.reply(), EBADF);
    }
    if (r.memfd() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    if (mailLength > 0) {
        return deliver(r.memfd(), r.h.arg, r.reply());
    }
    if (e.nonblock) {
        return fail(r.reply(), EAGAIN);
    }
    if (reader.at) {
        return fail(r.reply(), EBUSY);
    }
    // The READ waits: where it is answered hears of it and the memfd is ours
    // until it ends.
    reader = {keep(epoll, r, &readerTag), r.memfd(), r.h.arg};
    r.fds[0] = -1;
}

static void writeMail(const Endpoint& e, const Request& r) {
    Io io;
    if (!e.writes) {
        return fail(r.reply(), EBADF);
    }
    if (r.memfd() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    const size_t part = MIN(size_t(r.h.arg), sizeof mail - mailLength);
    const ssize_t got = pread(r.memfd(), mail + mailLength, part, 0);
    mailLength += got > 0 ? size_t(got) : 0;
    const uint32_t done = uint32_t(got > 0 ? got : 0);
    answer(r.reply(), opOk, 0, &done, sizeof done);
    if (reader.at && mailLength > 0) {
        deliver(reader.memfd, reader.length, reader.at->fd);
        letReaderGo();
    } else {
        tellAll();
    }
}

static void gone(Endpoint& e) {
    if (e.of == &readerTag) {
        // The reader went: serve closes its endpoint itself.
        close(reader.memfd);
        reader = {};
        return;
    }
    for (size_t i = 0; i < openCount; ++i) {
        if (opens[i] == e.fd) {
            opens[i] = opens[--openCount];
            break;
        }
    }
}

static void readText(Endpoint& e, const Request& r) {
    Io io;
    if (!e.reads) {
        return fail(r.reply(), EBADF);
    }
    if (r.memfd() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    const uint64_t off = offsetOf(e, io, false, length);
    const size_t part = off < length ? MIN(size_t(r.h.arg), length - size_t(off)) : 0;
    pwrite(r.memfd(), text + MIN(off, uint64_t(length)), part, 0);
    if ((io.flags & ioAt) == 0) {
        e.pos = off + part;
    }
    const uint32_t done = uint32_t(part);
    answer(r.reply(), opOk, 0, &done, sizeof done);
}

static void readKept(Endpoint& e, Request& r) {
    readText(e, r);
    if (r.memfd() >= 0 && keptCount < sizeof kept / sizeof kept[0]) {
        kept[keptCount++] = r.memfd();
        r.fds[0] = -1;
    }
}

static void writeKept(const Endpoint& e, const Request& r) {
    Io io;
    if (!e.writes) {
        return fail(r.reply(), EBADF);
    }
    if (r.memfd() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    for (size_t i = 0; i < keptCount; ++i) {
        if (pwrite(kept[i], text, length, 0) != ssize_t(length)) {
            return fail(r.reply(), EIO);
        }
    }
    answer(r.reply(), opOk, 0, &r.h.arg, sizeof r.h.arg);
}

static void handle(Endpoint& e, Request& r) {
    if (e.channel) {
        if (!mailbox) {
            answerOpen(epoll, r, {openedFile, 0, 0, 0, 0, POLLIN | POLLOUT}, e.of);
            return;
        }
        if (openCount == sizeof opens / sizeof opens[0]) {
            fail(r.reply(), ENFILE);
            return;
        }
        const uint32_t events = POLLOUT | (mailLength > 0 ? POLLIN : 0);
        if (const Endpoint* opened = answerOpen(epoll, r, {openedFile, 0, 0, 0, 0, events}, e.of)) {
            opens[openCount++] = opened->fd;
        }
        return;
    }
    if (e.of == &readerTag) {
        if (r.h.op == opCancel) {
            fail(e.fd, EINTR);
            letReaderGo();
        }
        return;
    }
    switch (r.h.op) {
        case opRead:
            if (mailbox) {
                readMail(e, r);
            } else if (keeping) {
                readKept(e, r);
            } else {
                readText(e, r);
            }
            break;
        case opWrite:
            if (mailbox) {
                writeMail(e, r);
            } else if (keeping) {
                writeKept(e, r);
            } else {
                fail(r.reply(), EINVAL);
            }
            break;
        case opFstat:
            answer(r.reply(), opOk, 0, &device.st, sizeof device.st);
            break;
        case opSeek:
            if (mailbox) {
                fail(r.reply(), ESPIPE);
            } else {
                answerSeek(e, r, length);
            }
            break;
        default:
            fail(r.reply(), unserved(r.h.op));
    }
}

static int copy(const char* path, long long off, long long at, long long n) {
    char bytes[16];
    memset(bytes, '.', sizeof bytes);
    const int source = open(path, O_RDONLY);
    const int memfd = memfd_create(bytes, sizeof bytes, O_RDWR);
    off_t from = off;
    off_t to = at;
    const ssize_t got = copy_file_range(source, &from, memfd, &to, size_t(n), 0);
    printf("%zd %lld %lld %.16s\n", got, (long long)from, (long long)to, bytes);
    return 0;
}

static int put(const char* path, const char* bytes) {
    const int target = open(path, O_WRONLY);
    const int memfd = memfd_create(const_cast<char*>(bytes), strlen(bytes), O_RDONLY);
    printf("%zd\n", copy_file_range(memfd, nullptr, target, nullptr, strlen(bytes), 0));
    return 0;
}

static int pass(const char* path) {
    char bytes[16];
    const int memfd = memfd_create(bytes, sizeof bytes, O_RDWR);
    fflush(stdout);
    const pid_t child = fork();
    if (child == 0) {
        const int source = open(path, O_RDONLY);
        off_t from = 0;
        off_t to = 0;
        printf("%zd\n", copy_file_range(source, &from, memfd, &to, sizeof bytes, 0));
        exit(0);
    }
    int status;
    waitpid(child, &status, 0);
    return 0;
}

static volatile sig_atomic_t rang = 0;

static void ring(int) {
    rang = 1;
}

static int waitFor(const char* path, bool restart) {
    struct sigaction action{};
    action.sa_handler = ring;
    action.sa_flags = restart ? SA_RESTART : 0;
    sigaction(SIGALRM, &action, nullptr);
    const int fd = open(path, O_RDONLY);
    alarm(1);
    char bytes[16];
    const ssize_t got = read(fd, bytes, sizeof bytes);
    const int e = errno;
    alarm(0);
    if (got < 0) {
        printf("%s %d\n", strerror(e), int(rang));
    } else {
        printf("%.*s %d\n", int(got), bytes, int(rang));
    }
    return 0;
}

int main(int argc, char** argv) {
    if (argc == 4 && strcmp(argv[1], "wait") == 0) {
        return waitFor(argv[2], strcmp(argv[3], "1") == 0);
    }
    if (argc == 6 && strcmp(argv[1], "copy") == 0) {
        return copy(argv[2], atoll(argv[3]), atoll(argv[4]), atoll(argv[5]));
    }
    if (argc == 4 && strcmp(argv[1], "put") == 0) {
        return put(argv[2], argv[3]);
    }
    if (argc == 3 && strcmp(argv[1], "pass") == 0) {
        return pass(argv[2]);
    }
    if (argc < 4) {
        dprintf(STDERR_FILENO, "usage: devices_service <name> <mode> <text> [ro]\n");
        return 2;
    }
    const char* name = argv[1];
    text = argv[3];
    length = strlen(text);
    mailbox = argc > 4 && strcmp(argv[4], "wait") == 0;
    keeping = argc > 4 && strcmp(argv[4], "keep") == 0;
    const bool readOnly = argc > 4 && strcmp(argv[4], "ro") == 0;
    const int registry = open("/dev/init", readOnly ? O_RDONLY : O_RDWR);
    const int e = registry < 0 ? errno : enroll(registry, name, uint32_t(strtoul(argv[2], nullptr, 8)), uint32_t(geteuid()), uint32_t(getegid()), device);
    if (e != 0) {
        printf("register %s: %s\n", name, strerror(e));
        return 1;
    }
    printf("registered %s\n", name);
    fflush(stdout);
    const pid_t child = fork();
    if (child != 0) {
        char path[64];
        snprintf(path, sizeof path, "/run/%s.pid", name);
        for (char* c = path + 5; *c; ++c) {
            if (*c == '/') {
                *c = '_';
            }
        }
        FILE* f = fopen(path, "w");
        fprintf(f, "%d", int(child));
        fclose(f);
        return 0;
    }
    epoll = epoll_create1(0);
    watch(epoll, {device.end, true, false, false, nullptr});
    serve(epoll, handle, gone);
    return 0;
}
