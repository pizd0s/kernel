//@ compile-flags: -g
//@ stdout: "flush all readers: ok\n"
//@ host: false

// fflush(NULL) flushes every stream whose fflush POSIX defines: of a
// stream reading a file capable of seeking, the file's offset becomes the
// stream's position, as fflush of the stream does (stream_offsets).
//
// Not on the host: musl's fflush(NULL) flushes only the streams with data
// to write.

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

int main() {
    int fd = open("lines", O_RDWR | O_CREAT | O_TRUNC, 0644);
    assert(fd >= 0 && write(fd, "first\nsecond\n", 13) == 13 && lseek(fd, 0, SEEK_SET) == 0);
    FILE* f = fdopen(dup(fd), "r");
    char line[16];
    assert(f && fgets(line, sizeof line, f) && strcmp(line, "first\n") == 0);
    assert(fflush(nullptr) == 0 && lseek(fd, 0, SEEK_CUR) == 6);
    assert(fgets(line, sizeof line, f) && strcmp(line, "second\n") == 0);
    assert(fclose(f) == 0);

    printf("flush all readers: ok\n");
}
