//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "a call is one edge: told once, not again while it waits, 'call' received\nnothing after the answer: no edge, EAGAIN\nthe asker's going after its answer: an edge, the end\na call and its asker's going before the wait: one edge, the end received, the call gone with its asker, EPIPE for an answer\na call answered late: no edge for the answer, one for the next call\n"

// A server watches a connection, the serving end of a call pair
// (lib/c's sys/socket.h, SOCK_CALL), for its edges, EPOLLET, as the
// protocol says its servers do (lib/protocol/protocol.h): a call, and its
// asker's going, are each told once; two that came before the wait are
// told as one, and then the call nobody received is gone with its asker:
// the server receives the end, and an answer is EPIPE. A receive that
// does not wait finds what else came with a call, or nothing.

#include <assert.h>
#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <unistd.h>

// How many events a wait that does not sleep finds.
static int edges(int ep) {
    epoll_event ev{};
    const int n = epoll_wait(ep, &ev, 1, 0);
    assert(n >= 0);
    return n;
}

// What a receive that does not wait gives: the text, "" at the end, or
// nullptr with nothing there.
static const char* taken(int fd) {
    static char text[64];
    const ssize_t n = recv(fd, text, sizeof text - 1, MSG_DONTWAIT);
    if (n < 0) {
        assert(errno == EAGAIN);
        return nullptr;
    }
    text[n] = 0;
    return text;
}

static int sent(int fd, const char* text) {
    return send(fd, text, strlen(text), MSG_NOSIGNAL) == ssize_t(strlen(text)) ? 0 : errno;
}

// A call pair whose serving end, the first, is in a set for its edges.
static int watched(int sv[2]) {
    assert(socketpair(AF_UNIX, SOCK_CALL | SOCK_CLOEXEC, 0, sv) == 0);
    const int ep = epoll_create1(EPOLL_CLOEXEC);
    epoll_event ev{};
    ev.events = EPOLLIN | EPOLLET;
    assert(ep >= 0 && epoll_ctl(ep, EPOLL_CTL_ADD, sv[0], &ev) == 0);
    // The member's joining is no edge of the pair's: nothing to receive.
    edges(ep);
    return ep;
}

int main() {
    alarm(60);
    int sv[2];
    char got[64];

    int ep = watched(sv);
    assert(sent(sv[1], "call") == 0);
    const int first = edges(ep);
    const int again = edges(ep);
    strcpy(got, taken(sv[0]));
    printf("a call is one edge: %s, %s, '%s' received\n", first == 1 ? "told once" : "not told", again == 0 ? "not again while it waits" : "told again", got);

    assert(sent(sv[0], "answer") == 0 && strcmp(taken(sv[1]), "answer") == 0);
    const int after = edges(ep);
    printf("nothing after the answer: %s, %s\n", after == 0 ? "no edge" : "an edge", taken(sv[0]) == nullptr ? "EAGAIN" : "a packet");

    close(sv[1]);
    const int gone = edges(ep);
    const char* end = taken(sv[0]);
    printf("the asker's going after its answer: %s, %s\n", gone == 1 ? "an edge" : "no edge", end && !*end ? "the end" : "no end");
    close(sv[0]);
    close(ep);

    ep = watched(sv);
    assert(sent(sv[1], "call") == 0);
    close(sv[1]);
    const int both = edges(ep);
    end = taken(sv[0]);
    const bool ended = end && !*end;
    end = taken(sv[0]);
    printf("a call and its asker's going before the wait: %s, %s, %s, %s for an answer\n", both == 1 && edges(ep) == 0 ? "one edge" : "not one edge",
           ended ? "the end received" : "no end received", end && !*end ? "the call gone with its asker" : "the call there still",
           sent(sv[0], "answer") == EPIPE ? "EPIPE" : "no EPIPE");
    close(sv[0]);
    close(ep);

    ep = watched(sv);
    assert(sent(sv[1], "call") == 0 && edges(ep) == 1 && strcmp(taken(sv[0]), "call") == 0);
    // Kept, and nothing came with it: a receive that does not wait says so.
    assert(taken(sv[0]) == nullptr);
    assert(sent(sv[0], "late") == 0 && strcmp(taken(sv[1]), "late") == 0);
    const int quiet = edges(ep);
    assert(sent(sv[1], "next") == 0);
    printf("a call answered late: %s, %s\n", quiet == 0 ? "no edge for the answer" : "an edge for the answer", edges(ep) == 1 ? "one for the next call" : "none for the next call");
    return 0;
}
