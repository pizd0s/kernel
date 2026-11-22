//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "name notifications: ok\n"

// inotify, Linux's, of names: a directory's watch tells IN_CREATE and
// IN_DELETE of the names made and removed in it, with IN_ISDIR for a
// directory, those there before it too, and IN_DELETE_SELF and then
// IN_IGNORED when it goes; the mask picks the events; a second watch of
// a directory is the first, its mask replaced or, with IN_MASK_ADD,
// added to, and IN_MASK_CREATE refuses it; inotify_rm_watch and
// IN_ONESHOT end a watch with IN_IGNORED; a read gives whole events, a
// name NUL-padded to 16 bytes, EINVAL for a buffer too small for the
// next, FIONREAD their bytes, EAGAIN with nothing or a wait that a
// child's mkdir ends; poll's IN; the calls' errors.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <stdio.h>
#include <string.h>
#include <sys/eventfd.h>
#include <sys/inotify.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

namespace {
    struct Event {
        int wd;
        unsigned mask;
        char name[32];
    };

    Event events[64];
    int count;

    // The events a read gives once poll finds them.
    int take(int in) {
        alignas(struct inotify_event) char buf[4096];
        count = 0;
        pollfd p = {in, POLLIN, 0};
        assert(poll(&p, 1, 2000) == 1 && p.revents == POLLIN);
        ssize_t n = read(in, buf, sizeof buf);
        assert(n > 0);
        for (char* at = buf; at < buf + n;) {
            auto* e = reinterpret_cast<struct inotify_event*>(at);
            assert(e->cookie == 0 && e->len % 16 == 0 && (e->len == 0 || strlen(e->name) < e->len));
            Event& r = events[count++];
            r.wd = e->wd;
            r.mask = e->mask;
            strcpy(r.name, e->len ? e->name : "");
            at += sizeof(struct inotify_event) + e->len;
        }
        return count;
    }

    bool has(int wd, unsigned mask, const char* name) {
        for (int i = 0; i < count; i++) {
            if (events[i].wd == wd && events[i].mask == mask && strcmp(events[i].name, name) == 0) {
                return true;
            }
        }
        return false;
    }

    void touch(const char* path) {
        int fd = open(path, O_CREAT | O_WRONLY, 0644);
        assert(fd >= 0 && close(fd) == 0);
    }

    bool quiet(int in) {
        char buf[256];
        errno = 0;
        return read(in, buf, sizeof buf) == -1 && errno == EAGAIN;
    }
}

int main() {
    assert(mkdir("watched", 0755) == 0);
    touch("watched/old");
    int in = inotify_init1(IN_NONBLOCK | IN_CLOEXEC);
    assert(in >= 0 && (fcntl(in, F_GETFD) & FD_CLOEXEC) && (fcntl(in, F_GETFL) & O_NONBLOCK));
    int wd = inotify_add_watch(in, "watched", IN_CREATE | IN_DELETE | IN_DELETE_SELF);
    assert(wd > 0 && quiet(in));
    pollfd p = {in, POLLIN, 0};
    assert(poll(&p, 1, 0) == 0);

    touch("watched/a");
    assert(mkdir("watched/d", 0755) == 0);
    int bytes = 0;
    assert(ioctl(in, FIONREAD, &bytes) == 0 && bytes == 64);
    assert(take(in) == 2 && has(wd, IN_CREATE, "a") && has(wd, IN_CREATE | IN_ISDIR, "d") && quiet(in));
    assert(unlink("watched/a") == 0 && rmdir("watched/d") == 0 && unlink("watched/old") == 0);
    assert(take(in) == 3 && has(wd, IN_DELETE, "a") && has(wd, IN_DELETE | IN_ISDIR, "d") && has(wd, IN_DELETE, "old"));

    touch("watched/abcdefghijklmnopqrst");
    alignas(struct inotify_event) char buf[4096];
    errno = 0;
    assert(read(in, buf, 20) == -1 && errno == EINVAL);
    auto* e = reinterpret_cast<struct inotify_event*>(buf);
    assert(read(in, buf, sizeof buf) == 48 && e->wd == wd && e->mask == IN_CREATE && e->len == 32);
    assert(strcmp(e->name, "abcdefghijklmnopqrst") == 0);
    assert(unlink("watched/abcdefghijklmnopqrst") == 0 && take(in) == 1);

    assert(inotify_add_watch(in, "watched", IN_DELETE) == wd);
    touch("watched/b");
    assert(quiet(in));
    assert(unlink("watched/b") == 0 && take(in) == 1 && has(wd, IN_DELETE, "b"));
    assert(inotify_add_watch(in, "watched", IN_CREATE | IN_MASK_ADD) == wd);
    touch("watched/c");
    assert(take(in) == 1 && has(wd, IN_CREATE, "c"));
    assert(unlink("watched/c") == 0 && take(in) == 1 && has(wd, IN_DELETE, "c"));
    errno = 0;
    assert(inotify_add_watch(in, "watched", IN_CREATE | IN_MASK_CREATE) == -1 && errno == EEXIST);
    errno = 0;
    assert(inotify_add_watch(in, "watched", IN_CREATE | IN_MASK_ADD | IN_MASK_CREATE) == -1 && errno == EINVAL);
    errno = 0;
    assert(inotify_add_watch(in, "watched", 0) == -1 && errno == EINVAL);
    errno = 0;
    assert(inotify_add_watch(in, "missing", IN_CREATE) == -1 && errno == ENOENT);
    int other = eventfd(0, 0);
    errno = 0;
    assert(inotify_add_watch(other, "watched", IN_CREATE) == -1 && errno == EINVAL);
    close(other);
    errno = 0;
    assert(inotify_add_watch(-1, "watched", IN_CREATE) == -1 && errno == EBADF);

    assert(mkdir("gone", 0755) == 0);
    int gone = inotify_add_watch(in, "gone", IN_CREATE | IN_DELETE_SELF);
    assert(gone > 0 && gone != wd && rmdir("gone") == 0);
    assert(take(in) == 2 && events[0].wd == gone && events[0].mask == IN_DELETE_SELF);
    assert(events[1].wd == gone && events[1].mask == IN_IGNORED);

    assert(inotify_rm_watch(in, wd) == 0 && take(in) == 1 && has(wd, IN_IGNORED, ""));
    errno = 0;
    assert(inotify_rm_watch(in, wd) == -1 && errno == EINVAL);

    int once = inotify_add_watch(in, "watched", IN_CREATE | IN_ONESHOT);
    assert(once > 0);
    touch("watched/o1");
    touch("watched/o2");
    assert(take(in) == 2 && events[0].wd == once && events[0].mask == IN_CREATE && strcmp(events[0].name, "o1") == 0);
    assert(has(once, IN_IGNORED, ""));
    assert(unlink("watched/o1") == 0 && unlink("watched/o2") == 0 && quiet(in));

    int blocking = inotify_init();
    int late = inotify_add_watch(blocking, "watched", IN_CREATE);
    assert(blocking >= 0 && late > 0);
    pid_t pid = fork();
    if (pid == 0) {
        usleep(20000);
        _exit(mkdir("watched/late", 0755) == 0 ? 0 : 1);
    }
    assert(pid > 0 && read(blocking, buf, sizeof buf) == 32 && e->wd == late && e->mask == (IN_CREATE | IN_ISDIR));
    assert(strcmp(e->name, "late") == 0);
    int status;
    assert(waitpid(pid, &status, 0) == pid && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    assert(close(blocking) == 0 && rmdir("watched/late") == 0 && close(in) == 0);

    printf("name notifications: ok\n");
}
