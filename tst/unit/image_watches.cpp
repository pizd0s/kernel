//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "image watches: ok\n"

// watchdir and inotify of a directory of the image, erofs's, which never
// changes: a watch tells the names a reading of the directory finds,
// each once, over as many reads as they take, then WATCH_IDLE, and
// nothing after; a read then EAGAIN with O_NONBLOCK, else a wait that
// only a signal ends, EINTR, and poll never finds it readable; a buffer
// too small for the next record EINVAL; inotify watches it and never
// tells. host: false: the image and watchdir are the system's.

#include <assert.h>
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/inotify.h>
#include <sys/time.h>
#include <sys/watchdir.h>
#include <unistd.h>

namespace {
    char names[1024][64];
    int count;

    bool listed(const char* name) {
        for (int i = 0; i < count; i++) {
            if (strcmp(names[i], name) == 0) {
                return true;
            }
        }
        return false;
    }

    short revents(int fd) {
        pollfd p = {fd, POLLIN, 0};
        assert(poll(&p, 1, 0) >= 0);
        return p.revents;
    }

    void alarmed(int) {
    }
}

int main() {
    const char* path = "/tests/unit";
    DIR* d = opendir(path);
    assert(d);
    for (dirent* e; (e = readdir(d));) {
        if (strcmp(e->d_name, ".") != 0 && strcmp(e->d_name, "..") != 0) {
            assert(count < 1024 && strlen(e->d_name) < 64);
            strcpy(names[count++], e->d_name);
        }
    }
    closedir(d);
    assert(count > 50);

    int dir = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    int w = watchdir(dir, O_NONBLOCK | O_CLOEXEC);
    assert(w >= 0 && revents(w) == POLLIN);
    char buf[512];
    errno = 0;
    assert(read(w, buf, 4) == -1 && errno == EINVAL);
    int told = 0;
    bool idle = false;
    for (int reads = 0; !idle; reads++) {
        assert(reads < 1000);
        ssize_t n = read(w, buf, sizeof buf);
        assert(n > 0);
        for (ssize_t at = 0; at < n;) {
            const char* name = buf + at + 1;
            if (buf[at] == WATCH_EXISTING) {
                assert(listed(name));
                told++;
            } else {
                assert(buf[at] == WATCH_IDLE && *name == 0 && at + 2 == n);
                idle = true;
            }
            at += 2 + strlen(name);
        }
    }
    assert(told == count && revents(w) == 0);
    errno = 0;
    assert(read(w, buf, sizeof buf) == -1 && errno == EAGAIN);

    int waits = watchdir(dir, O_CLOEXEC);
    assert(waits >= 0);
    while (true) {
        ssize_t n = read(waits, buf, sizeof buf);
        assert(n > 0);
        if (buf[n - 2] == WATCH_IDLE) {
            break;
        }
    }
    struct sigaction action = {};
    action.sa_handler = alarmed;
    assert(sigaction(SIGALRM, &action, nullptr) == 0);
    itimerval timer = {};
    timer.it_value.tv_usec = 20000;
    assert(setitimer(ITIMER_REAL, &timer, nullptr) == 0);
    errno = 0;
    assert(read(waits, buf, sizeof buf) == -1 && errno == EINTR);
    assert(close(waits) == 0 && close(w) == 0 && close(dir) == 0);

    int in = inotify_init1(IN_NONBLOCK);
    assert(in >= 0 && inotify_add_watch(in, path, IN_CREATE | IN_DELETE) > 0);
    errno = 0;
    assert(read(in, buf, sizeof buf) == -1 && errno == EAGAIN && revents(in) == 0);
    assert(close(in) == 0);

    printf("image watches: ok\n");
}
