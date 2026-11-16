//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "a user's terminal: uid 1000 gid 1000 mode 20600, the user opens it 1\nanother user: EACCES\nroot opens it: 1\na session's terminal, its leader another user now: /dev/tty opened, its path EACCES\n"

// A pseudoterminal's slave is its opener's, as Linux's devpts makes it:
// its owner and group are the opener's, its mode 0600, so only the opener
// opens it by its path, and root, as ever. /dev/tty opens a session's
// terminal for whoever is in the session, as Linux's does: the path's
// permissions are none of its business.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <pty.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

static const char* errorName(int e) {
    switch (e) {
        case EACCES:
            return "EACCES";
        case ENOENT:
            return "ENOENT";
        case ENXIO:
            return "ENXIO";
        default:
            return "another error";
    }
}

static void become(uid_t user) {
    assert(setgid(user) == 0 && setuid(user) == 0);
}

static void waitFor(pid_t child) {
    int status;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
}

// What a child told on a pipe, to its end.
static void readAll(int fd, char* text, size_t room) {
    size_t n = 0;
    ssize_t got;
    while (n < room - 1 && (got = read(fd, text + n, room - 1 - n)) > 0) {
        n += size_t(got);
    }
    text[n] = 0;
}

int main() {
    alarm(60);
    assert(getuid() == 0);

    // A user opens a terminal, and keeps it until the test is done with it.
    int named[2];
    int done[2];
    assert(pipe(named) == 0 && pipe(done) == 0);
    fflush(stdout);
    const pid_t user = fork();
    assert(user >= 0);
    if (user == 0) {
        close(named[0]);
        close(done[1]);
        become(1000);
        const int master = posix_openpt(O_RDWR | O_NOCTTY);
        assert(master >= 0 && grantpt(master) == 0 && unlockpt(master) == 0);
        const char* name = ptsname(master);
        assert(name);
        struct stat st;
        assert(stat(name, &st) == 0);
        const int slave = open(name, O_RDWR | O_NOCTTY);
        dprintf(named[1], "%s\na user's terminal: uid %u gid %u mode %o, the user opens it %d\n", name, unsigned(st.st_uid), unsigned(st.st_gid), unsigned(st.st_mode), slave >= 0);
        close(named[1]);
        char c;
        assert(read(done[0], &c, 1) == 0);
        _exit(0);
    }
    close(named[1]);
    close(done[0]);
    char text[256];
    readAll(named[0], text, sizeof text);
    close(named[0]);
    char* line = strchr(text, '\n');
    assert(line);
    *line++ = 0;
    char name[64];
    snprintf(name, sizeof name, "%s", text);
    fputs(line, stdout);

    fflush(stdout);
    const pid_t other = fork();
    assert(other >= 0);
    if (other == 0) {
        become(1001);
        const int fd = open(name, O_RDWR | O_NOCTTY);
        printf("another user: %s\n", fd >= 0 ? "opened" : errorName(errno));
        fflush(stdout);
        _exit(0);
    }
    waitFor(other);
    const int fd = open(name, O_RDWR | O_NOCTTY);
    printf("root opens it: %d\n", fd >= 0);
    close(fd);
    close(done[1]);
    waitFor(user);

    // root's terminal controls a session whose leader becomes another
    // user: /dev/tty opens it, its path does not.
    int told[2];
    assert(pipe(told) == 0);
    fflush(stdout);
    int master;
    const pid_t leader = forkpty(&master, nullptr, nullptr, nullptr);
    assert(leader >= 0);
    if (leader == 0) {
        close(told[0]);
        char path[64];
        assert(ttyname_r(STDIN_FILENO, path, sizeof path) == 0);
        become(1001);
        const int controlling = open("/dev/tty", O_RDWR | O_NOCTTY);
        const char* opened = controlling >= 0 ? "opened" : errorName(errno);
        const int byPath = open(path, O_RDWR | O_NOCTTY);
        dprintf(told[1], "a session's terminal, its leader another user now: /dev/tty %s, its path %s\n", opened, byPath >= 0 ? "opened" : errorName(errno));
        _exit(0);
    }
    close(told[1]);
    readAll(told[0], text, sizeof text);
    fputs(text, stdout);
    waitFor(leader);
    close(master);
    return 0;
}
