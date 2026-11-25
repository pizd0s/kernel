//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "object ids: ok\n"

// fdid, the id of the kernel's object of a descriptor, of every kind of
// object a program has descriptors of: one object's descriptors, dup's,
// fork's and SCM_RIGHTS's, have one id; a pipe's ends and a socket
// pair's are the pair's, a FIFO's openers the one pipe's; anything else
// is another id, never 0: a socket of socket(2), before it connects and
// after, its listener and the connection accept gives, a datagram
// socket, an inet socket, memfile's file, eventfd's, timerfd's, epoll's,
// inotify's, a server's open files, two opens of one file two, a
// directory's, a device's, a pseudoterminal's master and slave, a
// message queue's; no descriptor is EBADF.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <mqueue.h>
#include <netinet/in.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/eventfd.h>
#include <sys/inotify.h>
#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/timerfd.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <unistd.h>

static unsigned long long id(int fd) {
    unsigned long long value = 0;
    assert(fd >= 0 && fdid(fd, &value) == 0 && value != 0);
    return value;
}

static unsigned long long seen[64];
static int count;

// An object's id, which no other object seen has had.
static unsigned long long distinct(int fd) {
    const unsigned long long value = id(fd);
    for (int i = 0; i < count; ++i) {
        if (seen[i] == value) {
            fprintf(stderr, "descriptor %d: id %llu is another object's too\n", fd, value);
            abort();
        }
    }
    seen[count++] = value;
    return value;
}

static void same(int a, int b) {
    if (id(a) != id(b)) {
        fprintf(stderr, "descriptors %d and %d: ids %llu and %llu, of one object\n", a, b, id(a), id(b));
        abort();
    }
}

// A descriptor sent over a socket pair and received again.
static int passed(int fd) {
    int pair[2];
    assert(socketpair(AF_UNIX, SOCK_STREAM, 0, pair) == 0);
    char byte = 'x';
    iovec io{&byte, 1};
    alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))] = {};
    msghdr m{};
    m.msg_iov = &io;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    cmsghdr* c = CMSG_FIRSTHDR(&m);
    c->cmsg_level = SOL_SOCKET;
    c->cmsg_type = SCM_RIGHTS;
    c->cmsg_len = CMSG_LEN(sizeof(int));
    memcpy(CMSG_DATA(c), &fd, sizeof fd);
    assert(sendmsg(pair[0], &m, 0) == 1);
    memset(control, 0, sizeof control);
    m.msg_controllen = sizeof control;
    assert(recvmsg(pair[1], &m, 0) == 1);
    c = CMSG_FIRSTHDR(&m);
    assert(c && c->cmsg_type == SCM_RIGHTS);
    int got;
    memcpy(&got, CMSG_DATA(c), sizeof got);
    close(pair[0]);
    close(pair[1]);
    return got;
}

int main() {
    unsigned long long value = 0;
    errno = 0;
    assert(fdid(-1, &value) == -1 && errno == EBADF);
    errno = 0;
    assert(fdid(999, &value) == -1 && errno == EBADF);

    for (int fd = 0; fd < 3; ++fd) {
        assert(id(fd) != 0);
    }

    int p[2];
    assert(pipe(p) == 0);
    distinct(p[0]);
    same(p[0], p[1]);
    const int copy = dup(p[0]);
    same(copy, p[0]);
    const int received = passed(p[1]);
    same(received, p[0]);
    int q[2];
    assert(pipe(q) == 0);
    distinct(q[0]);

    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        const unsigned long long mine = id(p[0]);
        _exit(write(q[1], &mine, sizeof mine) == sizeof mine ? 0 : 1);
    }
    int status = 0;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    unsigned long long theirs = 0;
    assert(read(q[0], &theirs, sizeof theirs) == sizeof theirs && theirs == id(p[0]));

    const int kinds[] = {SOCK_STREAM, SOCK_SEQPACKET, SOCK_DGRAM};
    for (int kind : kinds) {
        int s[2];
        assert(socketpair(AF_UNIX, kind, 0, s) == 0);
        distinct(s[0]);
        same(s[0], s[1]);
    }

    assert(mkfifo("object-ids.fifo", 0600) == 0 && mkfifo("object-ids.other", 0600) == 0);
    const int reader = open("object-ids.fifo", O_RDONLY | O_NONBLOCK);
    const int writer = open("object-ids.fifo", O_WRONLY);
    distinct(reader);
    same(reader, writer);
    distinct(open("object-ids.other", O_RDONLY | O_NONBLOCK));

    const int listener = socket(AF_UNIX, SOCK_STREAM, 0);
    const unsigned long long before = distinct(listener);
    sockaddr_un where{};
    where.sun_family = AF_UNIX;
    strcpy(where.sun_path, "object-ids.socket");
    assert(bind(listener, reinterpret_cast<sockaddr*>(&where), sizeof where) == 0 && listen(listener, 1) == 0);
    assert(id(listener) == before);
    const int client = socket(AF_UNIX, SOCK_STREAM, 0);
    const unsigned long long unconnected = distinct(client);
    assert(connect(client, reinterpret_cast<sockaddr*>(&where), sizeof where) == 0);
    assert(id(client) == unconnected);
    distinct(accept(listener, nullptr, nullptr));
    distinct(socket(AF_UNIX, SOCK_DGRAM, 0));
    const int inet = socket(AF_INET, SOCK_STREAM, 0);
    assert(inet >= 0 || errno == EAFNOSUPPORT);
    if (inet >= 0) {
        distinct(inet);
    }

    static char memory[2][4096];
    distinct(memfile(memory[0], sizeof memory[0], O_RDWR));
    distinct(memfile(memory[1], sizeof memory[1], O_RDWR));
    distinct(eventfd(0, 0));
    distinct(eventfd(0, 0));
    distinct(timerfd_create(CLOCK_MONOTONIC, 0));
    distinct(epoll_create1(0));
    distinct(inotify_init1(0));

    const int file = open("object-ids.file", O_RDWR | O_CREAT, 0600);
    distinct(file);
    same(dup(file), file);
    same(passed(file), file);
    distinct(open("object-ids.file", O_RDONLY));
    distinct(open(".", O_RDONLY | O_DIRECTORY));
    distinct(open(".", O_RDONLY | O_DIRECTORY));
    distinct(open("/dev/null", O_RDWR));
    distinct(open("/dev/null", O_RDWR));

    const int master = posix_openpt(O_RDWR | O_NOCTTY);
    assert(master >= 0 && grantpt(master) == 0 && unlockpt(master) == 0);
    distinct(master);
    distinct(open(ptsname(master), O_RDWR | O_NOCTTY));

    const mqd_t queue = mq_open("/object-ids", O_CREAT | O_RDWR, 0600, nullptr);
    distinct(queue);
    distinct(mq_open("/object-ids", O_RDWR));
    assert(mq_unlink("/object-ids") == 0);

    unlink("object-ids.fifo");
    unlink("object-ids.other");
    unlink("object-ids.socket");
    unlink("object-ids.file");
    puts("object ids: ok");
}
