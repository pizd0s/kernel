// watchdir of devfs's directories, by a program that registers the names
// itself at /dev/init: the names there as a watch starts and the end of
// them, a name added, one removed and one back, which is another; a
// watch that does not wait, and poll of it; a directory that goes with
// its last name, whose watch tells the name gone and then ends; and the
// errors: no directory, a file system that does not watch, no descriptor,
// flags it does not take, a read too short for a record.
//
//     watch_dir

#include <lib/protocol/service.h>

#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/watchdir.h>
#include <unistd.h>

namespace {
    int registry = -1;

    Device made(const char* name) {
        Device d{};
        if (const int e = enroll(registry, name, S_IFCHR | 0600, uint32_t(geteuid()), uint32_t(getegid()), d)) {
            printf("register %s: %s\n", name, strerror(e));
        }
        return d;
    }

    // One read of a watch, its records a line each, or its end or error.
    void show(const char* label, int w) {
        unsigned char b[512];
        const ssize_t n = read(w, b, sizeof b);
        if (n < 0) {
            printf("%s: %s\n", label, strerror(errno));
            return;
        }
        if (n == 0) {
            printf("%s: end\n", label);
            return;
        }
        static const char* const kinds[] = {"?", "existing", "idle", "added", "removed"};
        for (ssize_t at = 0; at < n;) {
            const unsigned kind = b[at];
            const char* name = reinterpret_cast<const char*>(b + at + 1);
            printf("%s: %s%s%s\n", label, kinds[kind < 5 ? kind : 0], *name ? " " : "", name);
            at += 2 + ssize_t(strlen(name));
        }
    }

    void fails(const char* label, int fd, int flags) {
        errno = 0;
        const int w = watchdir(fd, flags);
        printf("%s: %d %s\n", label, w < 0 ? -1 : 0, strerror(errno));
    }
}

int main() {
    registry = open("/dev/init", O_RDWR | O_CLOEXEC);
    if (registry < 0) {
        printf("/dev/init: %s\n", strerror(errno));
        return 1;
    }
    Device a = made("w/a");
    const int dir = open("/dev/w", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    const int w = watchdir(dir, O_CLOEXEC);
    if (dir < 0 || w < 0) {
        printf("watchdir /dev/w: %s\n", strerror(errno));
        return 1;
    }
    show("w", w);
    Device c = made("w/b/c");
    show("w", w);
    close(a.end);
    show("w", w);
    a = made("w/a");
    show("w", w);

    const int quick = watchdir(dir, O_NONBLOCK | O_CLOEXEC);
    show("quick", quick);
    show("quick", quick);
    pollfd p{quick, POLLIN, 0};
    printf("quick: poll %d\n", poll(&p, 1, 0));
    const Device d = made("w/d");
    printf("quick: poll %d %s\n", poll(&p, 1, 5000), (p.revents & POLLIN) != 0 ? "POLLIN" : "-");
    show("quick", quick);
    show("w", w);

    const int sub = open("/dev/w/b", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    const int b = watchdir(sub, 0);
    show("b", b);
    close(c.end);
    show("b", b);
    show("b", b);
    show("w", w);

    unsigned char one;
    close(d.end);
    printf("short: %zd %s\n", read(w, &one, 1), strerror(errno));
    show("w", w);

    fails("a device", open("/dev/null", O_RDONLY | O_CLOEXEC), 0);
    fails("/run", open("/run", O_RDONLY | O_DIRECTORY | O_CLOEXEC), 0);
    fails("no descriptor", -1, 0);
    fails("flags", dir, O_RDWR | O_APPEND);
    return 0;
}
