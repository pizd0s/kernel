#pragma once

#include <__base.h>
#include <stdint.h>
#include <sys/epoll.h>
#include <sys/socket.h>

/*
 * A pair a server makes for what it serves (lib/protocol/protocol.h):
 * the first end is the server's, a plain one, which the requests of the
 * second come on; the second is born the open file struct served
 * describes, whose calls the kernel makes requests on the first, and goes
 * to who asked, as an OPEN's OK brings it. kind is SERVED_FILE, an open
 * file; SERVED_TERMINAL, a terminal's, which takes a terminal's calls
 * too; SERVED_DIR, a handle of a directory of the VFS's, which calls with
 * a path go to; or SERVED_PATH, a handle of what O_PATH opened that is no
 * directory; with SERVED_TELLS the server tells its readiness (READY).
 * flags are the open's: O_ACCMODE, O_APPEND and O_NONBLOCK, and O_PATH
 * of a directory's handle, which takes no locks then. dev and ino are
 * the file's or the directory's, as its stat has them, or a terminal's
 * device's: they name it to the kernel. The kernel keeps the locks of
 * flock and fcntl by them, so the open files given one dev and ino are
 * one file to lock; a file of ino 0 has no locks. And two opens of a
 * terminal are one terminal by them. id is a handle's, which calls with
 * a path name the root by. Both ends close on exec.
 */
#define SERVED_FILE 1
#define SERVED_TERMINAL 2
#define SERVED_DIR 3
#define SERVED_PATH 4
#define SERVED_TELLS 16

__BEGIN_DECLS

struct served {
    uint32_t kind;
    uint32_t flags;
    uint64_t dev;
    uint64_t ino;
    uint64_t id;
};

int servedpair(const struct served*, int[2]);

/*
 * A server's turn: the answer to the request in hand, then the next
 * request, in one call of the kernel. With reply not -1, answer goes on
 * it as sendmsg sends it, MSG_NOSIGNAL and at once, and reply is closed,
 * answered or not. Then the call waits as epoll_pwait does, timeout
 * milliseconds and with mask, for an endpoint of the set epfd that has a
 * packet or has ended, and receives it into request as recvmsg does with
 * MSG_CMSG_CLOEXEC; *event is the endpoint's. It returns the packet's
 * length, 0 for an endpoint that ended; or -1 and ETIMEDOUT past the
 * timeout, EINTR for a signal taken, and EAGAIN, with *event, for an
 * endpoint ready for something that is no reading. reply is closed and
 * answered whatever the call returns: the call after an error has none.
 */
ssize_t servedturn(int epfd, int reply, const struct msghdr* answer, struct msghdr* request, struct epoll_event* event, int timeout, const sigset_t* mask);

__END_DECLS
