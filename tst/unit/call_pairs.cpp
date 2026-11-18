//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "a call and its answer: 'call' then 'answer', a socket's\none call at a time: EBUSY before the answer, EBUSY before it is received, EPIPE for an answer to none, EPIPE for a second\nnothing to receive: EAGAIN, EAGAIN\ndescriptors ride both ways: 'to the server' 'to the asker'\nthe asker gone: the end, a hangup\nthe server gone: the end of the call in hand, EPIPE for the next\nsent on, the call in hand: answered by who got it, EPIPE for its second, EPIPE for the next call's, which its pair answers\nsent on and let go of: the end, and the pair serves the next call\nsent on and never received: the end\n"

// A call pair, socketpair's SOCK_CALL (lib/c's sys/socket.h): the first
// end serves, the second asks, a call and its answer at a time and
// nothing queued. A serving end sent with a packet is the call in hand:
// who receives it answers that call alone, and when it lets go of it
// unanswered, or nobody receives it, who asked reads the end, the pair's
// own serving end open all the while.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/uio.h>
#include <unistd.h>

static const char* errorName(int e) {
    switch (e) {
        case 0:
            return "no error";
        case EBUSY:
            return "EBUSY";
        case EPIPE:
            return "EPIPE";
        case EAGAIN:
            return "EAGAIN";
        default:
            return "another error";
    }
}

// The error of a send of text, 0 for none.
static int sent(int fd, const char* text) {
    return send(fd, text, strlen(text), MSG_NOSIGNAL) == ssize_t(strlen(text)) ? 0 : errno;
}

// What a receive gives: the packet's text, "" at the end.
static const char* received(int fd) {
    static char text[64];
    const ssize_t n = recv(fd, text, sizeof text - 1, 0);
    assert(n >= 0);
    text[n] = 0;
    return text;
}

// A packet of text with one descriptor.
static void sendWith(int to, const char* text, int fd) {
    alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))] = {};
    iovec iov{const_cast<char*>(text), strlen(text)};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    cmsghdr* c = CMSG_FIRSTHDR(&m);
    c->cmsg_level = SOL_SOCKET;
    c->cmsg_type = SCM_RIGHTS;
    c->cmsg_len = CMSG_LEN(sizeof(int));
    memcpy(CMSG_DATA(c), &fd, sizeof(int));
    assert(sendmsg(to, &m, MSG_NOSIGNAL) == ssize_t(strlen(text)));
}

// The descriptor that came with a packet, whose text is dropped.
static int receiveWith(int from) {
    char text[64];
    alignas(cmsghdr) char control[CMSG_SPACE(sizeof(int))];
    iovec iov{text, sizeof text};
    msghdr m{};
    m.msg_iov = &iov;
    m.msg_iovlen = 1;
    m.msg_control = control;
    m.msg_controllen = sizeof control;
    assert(recvmsg(from, &m, 0) >= 0);
    const cmsghdr* c = CMSG_FIRSTHDR(&m);
    assert(c && c->cmsg_type == SCM_RIGHTS && c->cmsg_len == CMSG_LEN(sizeof(int)));
    int fd;
    memcpy(&fd, CMSG_DATA(c), sizeof fd);
    return fd;
}

static void pair(int sv[2]) {
    assert(socketpair(AF_UNIX, SOCK_CALL | SOCK_CLOEXEC, 0, sv) == 0);
}

int main() {
    alarm(60);
    int sv[2];
    char call[16];
    char answer[16];

    pair(sv);
    assert(sent(sv[1], "call") == 0);
    strcpy(call, received(sv[0]));
    assert(sent(sv[0], "answer") == 0);
    strcpy(answer, received(sv[1]));
    struct stat st;
    assert(fstat(sv[0], &st) == 0);
    printf("a call and its answer: '%s' then '%s', %s\n", call, answer, S_ISSOCK(st.st_mode) ? "a socket's" : "no socket's");

    const int none = sent(sv[0], "answer");
    assert(sent(sv[1], "first") == 0);
    const int early = sent(sv[1], "second");
    assert(strcmp(received(sv[0]), "first") == 0 && sent(sv[0], "answer") == 0);
    const int unreceived = sent(sv[1], "second");
    const int twice = sent(sv[0], "answer");
    assert(strcmp(received(sv[1]), "answer") == 0 && sent(sv[1], "second") == 0);
    assert(strcmp(received(sv[0]), "second") == 0 && sent(sv[0], "done") == 0 && strcmp(received(sv[1]), "done") == 0);
    printf("one call at a time: %s before the answer, %s before it is received, %s for an answer to none, %s for a second\n", errorName(early),
           errorName(unreceived), errorName(none), errorName(twice));

    char byte;
    const int serving = recv(sv[0], &byte, 1, MSG_DONTWAIT) == -1 ? errno : 0;
    const int asking = recv(sv[1], &byte, 1, MSG_DONTWAIT) == -1 ? errno : 0;
    printf("nothing to receive: %s, %s\n", errorName(serving), errorName(asking));

    // A pipe's end goes with the call, another's with the answer.
    int there[2];
    int back[2];
    char first[32] = {};
    char second[32] = {};
    assert(pipe(there) == 0 && pipe(back) == 0);
    sendWith(sv[1], "call", there[1]);
    int got = receiveWith(sv[0]);
    assert(write(got, "to the server", 13) == 13 && read(there[0], first, sizeof first) == 13);
    close(got);
    sendWith(sv[0], "answer", back[1]);
    got = receiveWith(sv[1]);
    assert(write(got, "to the asker", 12) == 12 && read(back[0], second, sizeof second) == 12);
    close(got);
    printf("descriptors ride both ways: '%s' '%s'\n", first, second);

    close(sv[1]);
    pollfd watched{sv[0], POLLIN, 0};
    assert(poll(&watched, 1, 0) == 1);
    printf("the asker gone: %s, %s\n", recv(sv[0], &byte, 1, 0) == 0 ? "the end" : "no end", watched.revents & POLLHUP ? "a hangup" : "no hangup");
    close(sv[0]);

    pair(sv);
    assert(sent(sv[1], "call") == 0);
    close(sv[0]);
    const bool ended = recv(sv[1], &byte, 1, 0) == 0;
    printf("the server gone: %s, %s for the next\n", ended ? "the end of the call in hand" : "no end", errorName(sent(sv[1], "call")));
    close(sv[1]);

    // Sent with a packet, the serving end is the call in hand.
    int carrier[2];
    pair(sv);
    assert(socketpair(AF_UNIX, SOCK_SEQPACKET | SOCK_CLOEXEC, 0, carrier) == 0);
    assert(sent(sv[1], "call") == 0 && strcmp(received(sv[0]), "call") == 0);
    sendWith(carrier[0], "handed on", sv[0]);
    const int handed = receiveWith(carrier[1]);
    assert(sent(handed, "another's") == 0);
    strcpy(answer, received(sv[1]));
    const int again = sent(handed, "another's");
    assert(sent(sv[1], "next") == 0 && strcmp(received(sv[0]), "next") == 0);
    const int later = sent(handed, "another's");
    assert(sent(sv[0], "its pair's") == 0);
    printf("sent on, the call in hand: %s, %s for its second, %s for the next call's, which %s\n",
           strcmp(answer, "another's") == 0 ? "answered by who got it" : "not answered", errorName(again), errorName(later),
           strcmp(received(sv[1]), "its pair's") == 0 ? "its pair answers" : "nobody answers");
    close(handed);

    assert(sent(sv[1], "call") == 0 && strcmp(received(sv[0]), "call") == 0);
    sendWith(carrier[0], "handed on", sv[0]);
    close(receiveWith(carrier[1]));
    const bool dropped = recv(sv[1], &byte, 1, 0) == 0;
    assert(sent(sv[1], "next") == 0 && strcmp(received(sv[0]), "next") == 0 && sent(sv[0], "served") == 0);
    printf("sent on and let go of: %s, and %s\n", dropped ? "the end" : "no end",
           strcmp(received(sv[1]), "served") == 0 ? "the pair serves the next call" : "the pair is dead");

    assert(sent(sv[1], "call") == 0 && strcmp(received(sv[0]), "call") == 0);
    sendWith(carrier[0], "handed on", sv[0]);
    close(carrier[0]);
    close(carrier[1]);
    printf("sent on and never received: %s\n", recv(sv[1], &byte, 1, 0) == 0 ? "the end" : "no end");
    return 0;
}
