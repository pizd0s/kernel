//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "one name, one file: busy 1, a range busy 1, free after the close 1\nanother ino: free 1\nanother dev: free 1\nanother process, the same name: free 1, and busy among its own 1\nno ino: EINVAL 1\n"

// The kernel keeps the locks of flock and fcntl by what names a file: the
// process that made the open file with servedpair, and the dev and ino it
// gave it there. The open files a process gave one dev and ino are one
// file to lock; those of another ino or dev are other files; and so are
// those of another process, whatever dev and ino it gives: a server names
// its own files only. A file made with no ino has no locks.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <sys/file.h>
#include <sys/served.h>
#include <sys/wait.h>
#include <unistd.h>

// An open file of a dev and ino, served by nobody: a lock is no request.
static int file(uint64_t dev, uint64_t ino) {
    const struct served what{SERVED_FILE, O_RDWR, dev, ino, 0};
    int sv[2];
    assert(servedpair(&what, sv) == 0);
    return sv[1];
}

static int busy(int fd) {
    return flock(fd, LOCK_EX | LOCK_NB) == -1 && errno == EAGAIN;
}

static int taken(int fd) {
    return flock(fd, LOCK_EX | LOCK_NB) == 0;
}

int main() {
    const int a = file(7, 42);
    const int b = file(7, 42);
    assert(flock(a, LOCK_EX) == 0);
    const int held = busy(b);
    struct flock range{};
    range.l_type = F_WRLCK;
    range.l_whence = SEEK_SET;
    range.l_len = 8;
    assert(fcntl(a, F_SETLK, &range) == 0);
    const int ranged = fcntl(b, F_SETLK, &range) == -1 && errno == EAGAIN;
    assert(close(a) == 0);
    const int after = taken(b) && fcntl(b, F_SETLK, &range) == 0;
    printf("one name, one file: busy %d, a range busy %d, free after the close %d\n", held, ranged, after);

    printf("another ino: free %d\n", taken(file(7, 43)));
    printf("another dev: free %d\n", taken(file(8, 42)));

    // A child is another process: its file of the name b has is another.
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        const int mine = file(7, 42);
        const int free = taken(mine);
        printf("another process, the same name: free %d, and busy among its own %d\n", free, busy(file(7, 42)));
        fflush(stdout);
        _exit(0);
    }
    int status;
    assert(waitpid(child, &status, 0) == child && status == 0);

    const int nameless = file(7, 0);
    printf("no ino: EINVAL %d\n", flock(nameless, LOCK_EX) == -1 && errno == EINVAL);
    return 0;
}
