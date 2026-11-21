//@ compile-flags: -g
//@ stdout: "pipe ring: ok\n"

// A ring of processes over pipes, each taking a message from the pipe
// before it and writing it on, changed, to the pipe after it, and between
// two reading a file of the working directory's file system (tmpfs, in
// the system): every message keeps its sum and its count of hops, and
// every read of the file has its text, the buffers of calls one after
// another, of the pipes' and of a file system's, never the one of another
// call.

#include <assert.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

namespace {
    constexpr int processes = 8;
    constexpr int rounds = 300;
    constexpr size_t size = 64;

    struct Message {
        uint32_t hops;
        uint32_t sum;
        unsigned char body[size - 8];
    };

    uint32_t sumOf(const Message& m) {
        uint32_t s = m.hops;
        for (unsigned char c : m.body) {
            s = s * 31 + c;
        }
        return s;
    }

    const char text[] = "a ring of processes, a byte at a time\n";

    void readText() {
        char got[256];
        const int fd = open("ring.txt", O_RDONLY);
        assert(fd >= 0);
        assert(read(fd, got, sizeof got) == ssize_t(sizeof text - 1) && memcmp(got, text, sizeof text - 1) == 0);
        assert(close(fd) == 0);
    }

    // Takes a message, checks it, changes it and passes it on.
    void hop(int in, int out) {
        Message m;
        assert(read(in, &m, sizeof m) == sizeof m);
        assert(m.sum == sumOf(m));
        readText();
        ++m.hops;
        m.body[m.hops % sizeof m.body] ^= 0x5a;
        m.sum = sumOf(m);
        assert(write(out, &m, sizeof m) == sizeof m);
    }
}

int main() {
    const int fd = open("ring.txt", O_WRONLY | O_CREAT | O_TRUNC, 0644);
    assert(fd >= 0 && write(fd, text, sizeof text - 1) == ssize_t(sizeof text - 1) && close(fd) == 0);

    int pipes[processes][2];
    for (auto& p : pipes) {
        assert(pipe(p) == 0);
    }
    for (int i = 1; i < processes; ++i) {
        if (fork() == 0) {
            for (int r = 0; r < rounds; ++r) {
                hop(pipes[i][0], pipes[(i + 1) % processes][1]);
            }
            _exit(0);
        }
    }
    Message m{};
    for (size_t i = 0; i < sizeof m.body; ++i) {
        m.body[i] = (unsigned char)i;
    }
    m.sum = sumOf(m);
    assert(write(pipes[1][1], &m, sizeof m) == sizeof m);
    // The message goes around rounds times, the last ending here.
    for (int r = 0; r + 1 < rounds; ++r) {
        hop(pipes[0][0], pipes[1][1]);
    }
    assert(read(pipes[0][0], &m, sizeof m) == sizeof m);
    assert(m.sum == sumOf(m) && m.hops == uint32_t(processes * rounds - 1));
    for (int i = 1; i < processes; ++i) {
        int status;
        assert(wait(&status) > 0 && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    }
    assert(unlink("ring.txt") == 0);
    puts("pipe ring: ok");
}
