// watchdir of devfs's directories, by a program that registers the names
// itself at /dev/init/ctl: the names there as a watch starts and the end of
// them, a name added, one removed and one back, which is another; a
// watch that does not wait, and poll of it; a directory that goes with
// its last name, whose watch tells the name gone and then ends; and the
// errors: no directory, no descriptor, flags it does not take, a read too
// short for a record.
//
// With names and a directory, a watch of a directory "watched" it makes
// there, on any file system that changes: its names as it starts, a name
// added, one removed, a rename, a name that came and went between two
// reads, which is not told, a read that waits until a child's change or
// a signal, and a watched directory removed, whose watch ends; a name
// opened with O_CREAT again, which is not told, a link, a rename of a
// name to another of its file, which changes nothing, a rename over a
// watched directory, whose watch ends, and a watched directory removed
// from one nobody watches.
//
//     watch_dir
//     watch_dir names <directory>

#include <lib/protocol/service.h>

#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/wait.h>
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

    void alarmed(int) {
    }

    int names(const char* path) {
        char base[256];
        char at[300];
        snprintf(base, sizeof base, "%s/watched", path);
        auto in = [&](const char* name) {
            snprintf(at, sizeof at, "%s/%s", base, name);
            return at;
        };
        auto touch = [&](const char* name) {
            close(open(in(name), O_CREAT | O_WRONLY | O_CLOEXEC, 0644));
        };
        if (mkdir(base, 0755) != 0) {
            printf("mkdir %s: %s\n", base, strerror(errno));
            return 1;
        }
        touch("a");
        const int dir = open(base, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
        const int w = watchdir(dir, O_CLOEXEC);
        const int quick = watchdir(dir, O_NONBLOCK | O_CLOEXEC);
        if (dir < 0 || w < 0 || quick < 0) {
            printf("watchdir %s: %s\n", base, strerror(errno));
            return 1;
        }
        show("names", w);
        show("quick", quick);
        touch("b");
        show("names", w);
        unlink(in("a"));
        show("names", w);
        char from[300];
        snprintf(from, sizeof from, "%s/b", base);
        rename(from, in("c"));
        show("names", w);
        touch("x");
        unlink(in("x"));
        show("quick", quick);
        show("quick", quick);

        const pid_t child = fork();
        if (child == 0) {
            usleep(20000);
            mkdir(in("d"), 0755);
            _exit(0);
        }
        show("names", w);
        waitpid(child, nullptr, 0);
        struct sigaction action {};
        action.sa_handler = alarmed;
        sigaction(SIGALRM, &action, nullptr);
        itimerval timer{};
        timer.it_value.tv_usec = 20000;
        setitimer(ITIMER_REAL, &timer, nullptr);
        show("names", w);

        mkdir(in("e"), 0755);
        show("names", w);
        const int e = open(in("e"), O_RDONLY | O_DIRECTORY | O_CLOEXEC);
        const int sub = watchdir(e, O_CLOEXEC);
        show("sub", sub);
        close(e);
        rmdir(in("e"));
        show("sub", sub);
        show("names", w);
        unlink(in("c"));
        rmdir(in("d"));
        show("names", w);

        touch("f");
        show("names", w);
        touch("f");
        char other[300];
        snprintf(other, sizeof other, "%s/g", base);
        link(in("f"), other);
        show("names", w);
        rename(in("f"), other);
        pollfd p{w, POLLIN, 0};
        printf("names: poll %d\n", poll(&p, 1, 0));
        mkdir(in("h"), 0755);
        mkdir(in("i"), 0755);
        show("names", w);
        const int h = open(in("h"), O_RDONLY | O_DIRECTORY | O_CLOEXEC);
        const int hw = watchdir(h, O_CLOEXEC);
        close(h);
        show("h", hw);
        char from2[300];
        snprintf(from2, sizeof from2, "%s/i", base);
        rename(from2, in("h"));
        show("h", hw);
        show("names", w);
        char lone[300];
        snprintf(lone, sizeof lone, "%s/lone", path);
        mkdir(lone, 0755);
        const int l = open(lone, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
        const int lw = watchdir(l, O_CLOEXEC);
        close(l);
        show("lone", lw);
        rmdir(lone);
        show("lone", lw);
        unlink(in("f"));
        unlink(other);
        rmdir(in("h"));
        show("names", w);
        close(lw);
        close(hw);
        close(sub);
        close(quick);
        close(w);
        close(dir);
        rmdir(base);
        return 0;
    }
}

int main(int argc, char** argv) {
    if (argc == 3 && strcmp(argv[1], "names") == 0) {
        return names(argv[2]);
    }
    registry = open("/dev/init/ctl", O_RDWR | O_CLOEXEC);
    if (registry < 0) {
        printf("/dev/init/ctl: %s\n", strerror(errno));
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
    fails("no descriptor", -1, 0);
    fails("flags", dir, O_RDWR | O_APPEND);
    return 0;
}
