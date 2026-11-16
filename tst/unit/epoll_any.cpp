//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "a file in a set: in 1 out 1, again 1; edge-triggered: 1, then 0\n"

// Any object can be a member of a set of epoll's: a regular file, which
// never changes, is ready whenever asked, so a level-triggered member is
// reported at every wait, and an edge-triggered one once.

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <sys/epoll.h>
#include <unistd.h>

// The members ready now, at most one.
static int ready(int ep, epoll_event* got) {
    return epoll_wait(ep, got, 1, 0);
}

int main() {
    const int ep = epoll_create1(EPOLL_CLOEXEC);
    assert(ep >= 0);
    const int file = open("epoll-file", O_CREAT | O_RDWR | O_TRUNC, 0600);
    assert(file >= 0);
    epoll_event event{};
    event.events = EPOLLIN | EPOLLOUT;
    event.data.u64 = 7;
    assert(epoll_ctl(ep, EPOLL_CTL_ADD, file, &event) == 0);
    epoll_event got{};
    assert(ready(ep, &got) == 1 && got.data.u64 == 7);
    const int in = (got.events & EPOLLIN) != 0;
    const int out = (got.events & EPOLLOUT) != 0;
    const int again = ready(ep, &got);
    event.events = EPOLLIN | EPOLLET;
    assert(epoll_ctl(ep, EPOLL_CTL_MOD, file, &event) == 0);
    const int edge = ready(ep, &got);
    const int then = ready(ep, &got);
    printf("a file in a set: in %d out %d, again %d; edge-triggered: %d, then %d\n", in, out, again, edge, then);
    assert(close(file) == 0 && unlink("epoll-file") == 0);
    return 0;
}
