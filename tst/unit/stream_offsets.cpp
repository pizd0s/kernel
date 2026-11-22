//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "stream offsets: ok\n"

// A stream's position and its file's offset (POSIX fflush, fclose, exit):
// fflush of a stream reading a file capable of seeking sets the file's
// offset to the stream's position and drops what ungetc pushed back,
// with no further move; fclose does it to its stream, and exit to all,
// so a process that reads a line by stdio and exits leaves the rest to
// the next reader of the file (fflush(NULL) is flush_all_readers). ftello has 64
// bits, and where long has fewer, ftell and fseek are EOVERFLOW past it.
// Streams of lib/c's beside: freopen with no name, open_wmemstream past
// its first buffer with characters of more bytes than one, fopencookie,
// __fpurge, and __freadptr's look into what is read ahead.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <locale.h>
#include <stdio.h>
#include <stdio_ext.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>
#include <wchar.h>

static off_t offset(int fd) {
    return lseek(fd, 0, SEEK_CUR);
}

static FILE* reader(int fd) {
    assert(lseek(fd, 0, SEEK_SET) == 0);
    FILE* f = fdopen(dup(fd), "r");
    assert(f);
    return f;
}

struct Text {
    char data[16];
    off_t at;
    off_t size;
};

static ssize_t textRead(void* cookie, char* buf, size_t n) {
    Text* t = static_cast<Text*>(cookie);
    size_t left = size_t(t->size - t->at);
    n = n < left ? n : left;
    memcpy(buf, t->data + t->at, n);
    t->at += n;
    return ssize_t(n);
}

static ssize_t textWrite(void* cookie, const char* buf, size_t n) {
    Text* t = static_cast<Text*>(cookie);
    size_t room = sizeof t->data - size_t(t->at);
    n = n < room ? n : room;
    memcpy(t->data + t->at, buf, n);
    t->at += n;
    t->size = t->at > t->size ? t->at : t->size;
    return ssize_t(n);
}

static int textSeek(void* cookie, off_t* at, int whence) {
    Text* t = static_cast<Text*>(cookie);
    off_t base = whence == SEEK_SET ? 0 : whence == SEEK_CUR ? t->at : t->size;
    if (base + *at < 0 || base + *at > t->size) {
        errno = EINVAL;
        return -1;
    }
    *at = t->at = base + *at;
    return 0;
}

static int closed;

static int textClose(void*) {
    closed++;
    return 0;
}

int main() {
    assert(setlocale(LC_ALL, "C.UTF-8") != nullptr);

    int fd = open("lines", O_RDWR | O_CREAT | O_TRUNC, 0644);
    assert(fd >= 0 && write(fd, "first\nsecond\nthird\n", 19) == 19);
    char line[32];

    FILE* f = reader(fd);
    assert(fgets(line, sizeof line, f) && strcmp(line, "first\n") == 0);
    assert(fflush(f) == 0 && offset(fd) == 6);
    assert(fgetc(f) == 's' && ungetc('x', f) == 'x');
    assert(fflush(f) == 0 && offset(fd) == 6);
    assert(fgetc(f) == 's' && fgetc(f) == 'e');
    assert(fgets(line, sizeof line, f) && strcmp(line, "cond\n") == 0);
    assert(fclose(f) == 0 && offset(fd) == 13);

    f = reader(fd);
    assert(fgets(line, sizeof line, f) && strcmp(line, "first\n") == 0);
    assert(fclose(f) == 0 && offset(fd) == 6);

    assert(lseek(fd, 0, SEEK_SET) == 0);
    pid_t child = fork();
    if (child == 0) {
        FILE* c = fdopen(fd, "r");
        assert(c && fgets(line, sizeof line, c) && strcmp(line, "first\n") == 0);
        exit(0);
    }
    int status = 0;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    assert(offset(fd) == 6);

    FILE* g = fopen("big", "w+");
    assert(g && fseeko(g, off_t(3) << 30, SEEK_SET) == 0 && ftello(g) == off_t(3) << 30);
    errno = 0;
    if (sizeof(long) < sizeof(off_t)) {
        assert(ftell(g) == -1 && errno == EOVERFLOW);
        assert(fseek(g, LONG_MAX, SEEK_SET) == 0 && ftell(g) == LONG_MAX);
        assert(fseek(g, 1, SEEK_CUR) == -1 && errno == EOVERFLOW && ftello(g) == LONG_MAX);
    } else {
        assert(ftell(g) == long(3) << 30);
    }
    fclose(g);

    f = fopen("again", "w+");
    assert(f && fputs("abc\n", f) >= 0);
    assert(freopen(nullptr, "r", f) == f);
    rewind(f);
    assert(fgets(line, sizeof line, f) && strcmp(line, "abc\n") == 0);
    assert(fclose(f) == 0);

    wchar_t* wide = nullptr;
    size_t wideSize = 0;
    FILE* m = open_wmemstream(&wide, &wideSize);
    assert(m);
    for (int i = 0; i < 3000; i++) {
        assert(fputwc(i % 3 ? L'é' : L'€', m) != WEOF);
    }
    assert(fflush(m) == 0 && wideSize == 3000);
    assert(fputws(L"end", m) >= 0 && fclose(m) == 0 && wideSize == 3003);
    for (int i = 0; i < 3000; i++) {
        assert(wide[i] == (i % 3 ? L'é' : L'€'));
    }
    assert(wcscmp(wide + 3000, L"end") == 0);
    free(wide);

    Text text = {};
    cookie_io_functions_t io = {textRead, textWrite, textSeek, textClose};
    FILE* c = fopencookie(&text, "w+", io);
    assert(c && fputs("hello", c) >= 0 && fflush(c) == 0);
    assert(text.size == 5 && memcmp(text.data, "hello", 5) == 0);
    assert(fseek(c, 1, SEEK_SET) == 0 && fgetc(c) == 'e' && ftell(c) == 2);
    assert(fclose(c) == 0 && closed == 1);
    cookie_io_functions_t writeless = {textRead, nullptr, nullptr, nullptr};
    text.at = 0;
    c = fopencookie(&text, "r", writeless);
    assert(c && fgets(line, sizeof line, c) && strcmp(line, "hello") == 0 && fgetc(c) == EOF && feof(c));
    errno = 0;
    assert(fseek(c, 0, SEEK_SET) == -1);
    assert(fclose(c) == 0 && closed == 1);

    f = reader(fd);
    assert(fgetc(f) == 'f');
    size_t ahead = 0;
    const char* p = __freadptr(f, &ahead);
    assert(p && ahead == 18 && memcmp(p, "irst\n", 5) == 0);
    __freadptrinc(f, 5);
    assert(fgetc(f) == 's');
    assert(__fpurge(f) == 0 && __freadptr(f, &ahead) == nullptr);
    assert(fclose(f) == 0);

    printf("stream offsets: ok\n");
}
