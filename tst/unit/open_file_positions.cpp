//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "open file positions: ok\n"

// An open file's position and its flags are the open file description's,
// which its server keeps: two opens have their own, a dup and a child
// share one; O_APPEND set by fcntl on one of them is the others' too; a
// write past the end leaves a hole; a directory reads its entries again
// from its start; what O_PATH opens has no position.

#include <assert.h>
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

static void reads(int fd, size_t n, const char* want) {
    char got[16] = {};
    assert(read(fd, got, n) == ssize_t(strlen(want)) && memcmp(got, want, strlen(want)) == 0);
}

static off_t sizeOf(int fd) {
    struct stat st{};
    assert(fstat(fd, &st) == 0);
    return st.st_size;
}

static int entries(DIR* d) {
    int n = 0;
    while (readdir(d)) {
        ++n;
    }
    return n;
}

int main() {
    char work[] = "positions-XXXXXX";
    assert(mkdtemp(work) == work && chdir(work) == 0);
    int fd = open("data", O_WRONLY | O_CREAT | O_EXCL, 0600);
    assert(fd >= 0 && write(fd, "0123456789", 10) == 10 && close(fd) == 0);

    const int a = open("data", O_RDONLY);
    const int b = open("data", O_RDONLY);
    assert(a >= 0 && b >= 0);
    reads(a, 3, "012");
    reads(b, 2, "01");
    reads(a, 2, "34");
    const int d = dup(a);
    reads(d, 2, "56");
    assert(lseek(a, 0, SEEK_CUR) == 7 && lseek(b, 0, SEEK_CUR) == 2);

    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        reads(a, 2, "78");
        _exit(0);
    }
    int status;
    assert(waitpid(child, &status, 0) == child && status == 0);
    assert(lseek(d, 0, SEEK_CUR) == 9);
    reads(a, 4, "9");
    reads(a, 4, "");

    errno = 0;
    assert(lseek(a, -1, SEEK_SET) == -1 && errno == EINVAL);
    errno = 0;
    assert(lseek(a, 0, 99) == -1 && errno == EINVAL);
    assert(lseek(a, 0, SEEK_END) == 10 && lseek(a, -3, SEEK_END) == 7 && lseek(d, 2, SEEK_CUR) == 9);
    char one;
    assert(pread(a, &one, 1, 1) == 1 && one == '1' && lseek(a, 0, SEEK_CUR) == 9);

    const int w = open("data", O_WRONLY);
    const int v = dup(w);
    assert(w >= 0 && v >= 0 && (fcntl(w, F_GETFL) & O_APPEND) == 0);
    assert(fcntl(w, F_SETFL, O_APPEND) == 0 && (fcntl(v, F_GETFL) & O_APPEND) != 0);
    assert(write(w, "ab", 2) == 2 && sizeOf(w) == 12 && lseek(w, 0, SEEK_CUR) == 12);
    assert(lseek(v, 0, SEEK_SET) == 0 && write(v, "c", 1) == 1 && sizeOf(w) == 13 && lseek(w, 0, SEEK_CUR) == 13);
    assert(fcntl(v, F_SETFL, 0) == 0 && (fcntl(w, F_GETFL) & O_APPEND) == 0);
    assert(lseek(w, 0, SEEK_SET) == 0 && write(w, "X", 1) == 1 && sizeOf(w) == 13);
    assert(pread(a, &one, 1, 0) == 1 && one == 'X');
    assert(lseek(w, 20, SEEK_SET) == 20 && write(w, "Z", 1) == 1 && sizeOf(w) == 21);
    assert(pread(a, &one, 1, 20) == 1 && one == 'Z' && pread(a, &one, 1, 15) == 1 && one == 0);
    const int appends = open("data", O_WRONLY | O_APPEND);
    assert(appends >= 0 && (fcntl(appends, F_GETFL) & O_APPEND) != 0);
    assert(lseek(appends, 0, SEEK_SET) == 0 && write(appends, "q", 1) == 1 && sizeOf(appends) == 22);
    assert(lseek(appends, 0, SEEK_CUR) == 22);

    const int zero = open("/dev/zero", O_RDONLY);
    assert(zero >= 0 && lseek(zero, 100, SEEK_SET) == 0);

    const int path = open("data", O_PATH);
    assert(path >= 0 && (fcntl(path, F_GETFL) & O_PATH) != 0);
    errno = 0;
    assert(read(path, &one, 1) == -1 && errno == EBADF);
    errno = 0;
    assert(lseek(path, 0, SEEK_SET) == -1 && errno == EBADF);
    errno = 0;
    assert(flock(path, LOCK_EX) == -1 && errno == EBADF);

    assert(mkdir("dir", 0700) == 0);
    for (int i = 0; i < 5; ++i) {
        char name[16];
        snprintf(name, sizeof name, "dir/f%d", i);
        fd = open(name, O_WRONLY | O_CREAT, 0600);
        assert(fd >= 0 && close(fd) == 0);
    }
    DIR* first = opendir("dir");
    DIR* second = opendir("dir");
    assert(first && second && entries(first) == 7 && entries(second) == 7);
    rewinddir(first);
    assert(entries(first) == 7 && closedir(first) == 0 && closedir(second) == 0);

    const int opened[] = {a, b, d, w, v, appends, zero, path};
    for (const int o : opened) {
        assert(close(o) == 0);
    }
    printf("open file positions: ok\n");
    return 0;
}
