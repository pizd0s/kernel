//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "directory watches: ok\n"

// watchdir of a directory of /run, tmpfs's, as <sys/watchdir.h> says:
// its names as the watch starts, then WATCH_IDLE, then what was added and
// removed since the last read; a name that came and went between two
// reads not told, one that went and came back removed and added, a
// rename a removal and an addition; a read with nothing to tell EAGAIN
// with O_NONBLOCK, else a wait that a child's change ends, or a signal,
// EINTR; poll's IN, and HUP once the directory is gone, whose watch then
// tells what is left and reads 0; EINVAL for a buffer too small for the
// next record; a big directory's names over several reads; ENOTDIR of
// what is no directory, a socket's too, EBADF of O_PATH's, and a watch no events file of
// inotify's. host: false:
// watchdir is the system's.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/inotify.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/wait.h>
#include <sys/watchdir.h>
#include <unistd.h>

namespace {
    struct Record {
        int kind;
        char name[64];
    };

    Record records[2048];
    int count;

    // The records of one read, or its result if not a positive count.
    long take(int w, size_t room = 65536) {
        static char buf[65536];
        count = 0;
        ssize_t n = read(w, buf, room);
        for (ssize_t at = 0; at < n;) {
            Record& r = records[count++];
            r.kind = buf[at];
            strcpy(r.name, buf + at + 1);
            at += 2 + strlen(buf + at + 1);
        }
        return n;
    }

    bool has(int kind, const char* name) {
        for (int i = 0; i < count; i++) {
            if (records[i].kind == kind && strcmp(records[i].name, name) == 0) {
                return true;
            }
        }
        return false;
    }

    void touch(const char* path) {
        int fd = open(path, O_CREAT | O_WRONLY, 0644);
        assert(fd >= 0 && close(fd) == 0);
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
    assert(mkdir("w", 0755) == 0);
    touch("w/a");
    touch("w/b");
    int dir = open("w", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    int w = watchdir(dir, O_NONBLOCK | O_CLOEXEC);
    assert(w >= 0 && revents(w) == POLLIN);
    assert(take(w) > 0 && count == 3 && has(WATCH_EXISTING, "a") && has(WATCH_EXISTING, "b"));
    assert(records[2].kind == WATCH_IDLE && records[2].name[0] == 0);
    errno = 0;
    assert(take(w) == -1 && errno == EAGAIN && revents(w) == 0);

    int pipes[2];
    int ends[2];
    assert(pipe(pipes) == 0 && socketpair(AF_UNIX, SOCK_STREAM, 0, ends) == 0);
    const int file = open("w/a", O_RDONLY);
    const int path = open("w", O_PATH | O_DIRECTORY);
    const struct {
        int fd;
        int flags;
        int error;
    } wrong[] = {{pipes[0], 0, ENOTDIR}, {ends[0], 0, ENOTDIR}, {file, 0, ENOTDIR}, {path, 0, EBADF}, {-1, 0, EBADF}, {dir, O_APPEND, EINVAL}};
    for (const auto& c : wrong) {
        errno = 0;
        assert(watchdir(c.fd, c.flags) == -1 && errno == c.error);
    }
    errno = 0;
    assert(inotify_add_watch(w, "w", IN_CREATE) == -1 && errno == EINVAL);
    close(pipes[0]);
    close(pipes[1]);
    close(ends[0]);
    close(ends[1]);
    close(file);
    close(path);

    touch("w/c");
    assert(revents(w) == POLLIN && take(w) > 0 && count == 1 && has(WATCH_ADDED, "c"));
    touch("w/gone");
    assert(unlink("w/gone") == 0);
    errno = 0;
    assert(take(w) == -1 && errno == EAGAIN);
    assert(unlink("w/c") == 0);
    touch("w/c");
    assert(take(w) > 0 && count == 2 && records[0].kind == WATCH_REMOVED && records[1].kind == WATCH_ADDED);
    assert(rename("w/c", "w/d") == 0);
    assert(take(w) > 0 && count == 2 && has(WATCH_REMOVED, "c") && has(WATCH_ADDED, "d"));
    touch("w/long-name");
    errno = 0;
    assert(take(w, 4) == -1 && errno == EINVAL && take(w) > 0 && has(WATCH_ADDED, "long-name"));

    int blocking = watchdir(dir, 0);
    assert(blocking >= 0 && take(blocking) > 0 && records[count - 1].kind == WATCH_IDLE);
    pid_t pid = fork();
    if (pid == 0) {
        usleep(20000);
        touch("w/late");
        _exit(0);
    }
    assert(take(blocking) > 0 && count == 1 && has(WATCH_ADDED, "late"));
    int status;
    assert(waitpid(pid, &status, 0) == pid && WIFEXITED(status));

    struct sigaction action = {};
    action.sa_handler = alarmed;
    assert(sigaction(SIGALRM, &action, nullptr) == 0);
    itimerval timer = {};
    timer.it_value.tv_usec = 20000;
    assert(setitimer(ITIMER_REAL, &timer, nullptr) == 0);
    errno = 0;
    assert(take(blocking) == -1 && errno == EINTR);
    touch("w/after");
    assert(take(blocking) > 0 && has(WATCH_ADDED, "after"));
    assert(close(blocking) == 0);

    assert(take(w) > 0 && has(WATCH_ADDED, "late") && has(WATCH_ADDED, "after"));
    const char* names[] = {"a", "b", "d", "long-name", "late", "after"};
    for (const char* name : names) {
        char path[64];
        snprintf(path, sizeof path, "w/%s", name);
        assert(unlink(path) == 0);
    }
    assert(take(w) > 0 && count == 6 && revents(w) == 0);
    assert(close(dir) == 0 && rmdir("w") == 0);
    assert(revents(w) == (POLLIN | POLLHUP) && take(w) == 0 && take(w) == 0);
    assert(close(w) == 0);

    assert(mkdir("big", 0755) == 0);
    for (int i = 0; i < 1500; i++) {
        char path[64];
        snprintf(path, sizeof path, "big/name-%04d", i);
        touch(path);
    }
    int b = open("big", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    int bw = watchdir(b, O_NONBLOCK);
    assert(bw >= 0);
    int existing = 0;
    bool idle = false;
    for (int reads = 0; !idle; reads++) {
        assert(reads < 100 && take(bw, 4096) > 0);
        for (int i = 0; i < count; i++) {
            existing += records[i].kind == WATCH_EXISTING;
            idle = idle || records[i].kind == WATCH_IDLE;
        }
    }
    assert(existing == 1500);
    assert(unlink("big/name-0700") == 0 && take(bw) > 0 && count == 1 && has(WATCH_REMOVED, "name-0700"));
    assert(close(bw) == 0 && close(b) == 0);

    printf("directory watches: ok\n");
}
