//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "a FIFO another process names: a pipe of its own 1\na terminal another process names: not the controlling terminal 1\n"

// What a server serves is named to the kernel by the server itself and
// the dev and ino it says: a process that says the dev and ino of what is
// another's names nothing of the other's. A FIFO it answers an open with
// has a pipe of its own, not that of the FIFO whose numbers it gave; a
// terminal it makes with the numbers of its controlling terminal is not
// that terminal.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/served.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/uio.h>
#include <sys/wait.h>
#include <termios.h>
#include <unistd.h>

// The protocol's: a request's header, an OK's op, a stat.
struct Header {
    uint32_t op;
    uint32_t arg;
    uint32_t uid;
    uint32_t gid;
};

struct Stat {
    uint64_t ino;
    uint64_t size;
    uint64_t mtime;
    uint32_t mode;
    uint32_t nlink;
    uint32_t uid;
    uint32_t gid;
    uint64_t atime;
    uint64_t dev;
    uint64_t ctime;
};

constexpr uint32_t opOk = 128;

// Answers the open that comes on a handle's end as a file system answers
// the open of a FIFO: with a stat, of the numbers given.
static void answerFifo(int end, const struct stat& numbers) {
    unsigned char packet[512];
    alignas(cmsghdr) char control[CMSG_SPACE(4 * sizeof(int))];
    iovec iov{packet, sizeof packet};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    assert(recvmsg(end, &m, 0) >= ssize_t(sizeof(Header)));
    const cmsghdr* c = CMSG_FIRSTHDR(&m);
    assert(c && c->cmsg_type == SCM_RIGHTS && c->cmsg_len == CMSG_LEN(sizeof(int)));
    int reply;
    memcpy(&reply, CMSG_DATA(c), sizeof reply);

    struct {
        Header h;
        Stat st;
    } answer{{opOk, 0, 0, 0}, {}};

    answer.st.ino = numbers.st_ino;
    answer.st.dev = numbers.st_dev;
    answer.st.mode = S_IFIFO | 0600;
    answer.st.nlink = 1;
    assert(send(reply, &answer, sizeof answer, 0) == ssize_t(sizeof answer));
    close(reply);
}

static void reap(pid_t child) {
    int status;
    assert(waitpid(child, &status, 0) == child && status == 0);
}

int main() {
    alarm(60);

    // A FIFO of the file system's, open here, and a directory's handle this
    // process serves: a child opens a name in it, and the answer is a FIFO
    // of the first one's dev and ino. What the child writes there does not
    // come out of the first.
    assert(mkfifo("names.fifo", 0600) == 0);
    const int real = open("names.fifo", O_RDWR | O_NONBLOCK);
    struct stat numbers;
    assert(real >= 0 && fstat(real, &numbers) == 0);
    const struct served directory{SERVED_DIR, O_RDONLY, 0, 0, 1};
    int handle[2];
    assert(servedpair(&directory, handle) == 0);
    pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        const int fd = openat(handle[1], "named", O_RDWR | O_NONBLOCK);
        assert(fd >= 0 && write(fd, "!", 1) == 1);
        _exit(0);
    }
    answerFifo(handle[0], numbers);
    reap(child);
    char byte;
    printf("a FIFO another process names: a pipe of its own %d\n", read(real, &byte, 1) == -1 && errno == EAGAIN);
    assert(close(real) == 0 && unlink("names.fifo") == 0);

    // A session whose controlling terminal is a pseudoterminal's slave,
    // and a terminal its leader makes itself with the slave's dev and ino.
    const int master = posix_openpt(O_RDWR | O_NOCTTY);
    assert(master >= 0 && grantpt(master) == 0 && unlockpt(master) == 0);
    fflush(stdout);
    child = fork();
    assert(child >= 0);
    if (child == 0) {
        assert(setsid() == getpid());
        const int slave = open(ptsname(master), O_RDWR);
        struct stat tty;
        assert(slave >= 0 && fstat(slave, &tty) == 0 && tcgetsid(slave) == getpid());
        const struct served fake{SERVED_TERMINAL, O_RDWR, tty.st_dev, tty.st_ino, 0};
        int pair[2];
        assert(servedpair(&fake, pair) == 0);
        printf("a terminal another process names: not the controlling terminal %d\n", tcgetsid(pair[1]) == -1 && errno == ENOTTY);
        fflush(stdout);
        _exit(0);
    }
    reap(child);
    return 0;
}
