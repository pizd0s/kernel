// The guests' memory is one fixed part the kernel places their regions in
// and moves them within (bin/vm/pool.go). This program runs as the first
// process of a vm whose memory test_memory_pool.py makes small.
//
// Six children, each forked once the one before holds its block, lie one
// after another after the parent. A filler then takes all the memory left,
// until malloc fails, and a fork finds no room. The first, third and fifth
// children end: three holes, each about one child's region, between the
// others. The second grows by two and a half regions, more than the holes
// on either side of it together: the growth takes the three holes
// together, which the fourth child, between the third's hole and the
// fifth's, has to move for. With the argument "running" it runs a loop
// that makes no call as it moves, else it waits in a read. Every child
// keeps what it holds, and what the growth gives reads zero.

#include <assert.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

enum { count = 6, held = 8 << 20, rounds = 48 };

static void fill(unsigned char* block, size_t size, unsigned seed) {
    for (size_t i = 0; i < size; i++) {
        block[i] = (unsigned char)(i * 31 + seed);
    }
}

static int same(const unsigned char* block, size_t size, unsigned seed) {
    for (size_t i = 0; i < size; i++) {
        if (block[i] != (unsigned char)(i * 31 + seed)) {
            return 0;
        }
    }
    return 1;
}

static void tell(int fd, char c) {
    assert(write(fd, &c, 1) == 1);
}

static char hear(int fd) {
    char c;
    assert(read(fd, &c, 1) == 1);
    return c;
}

static void say(const char* text) {
    assert(write(1, text, strlen(text)) == (ssize_t)strlen(text));
}

static void child(unsigned k, int in, int out) {
    unsigned char* block = malloc(held);
    if (!block) {
        _exit(2);
    }
    fill(block, held, k);
    size_t region = __builtin_wasm_memory_size(0) * 65536;
    assert(write(out, &region, sizeof region) == sizeof region);
    for (;;) {
        char command = hear(in);
        if (command == 'g') {
            size_t more;
            assert(read(in, &more, sizeof more) == sizeof more);
            unsigned char* extra = malloc(more);
            char answer = 'm';
            if (extra) {
                int zero = 1;
                for (size_t i = 0; i < more; i++) {
                    zero &= extra[i] == 0;
                }
                fill(extra, more, k + 64);
                answer = !zero ? 'z' : same(block, held, k) && same(extra, more, k + 64) ? 'y' : 'n';
            }
            tell(out, answer);
        } else if (command == 'b') {
            int kept = 1;
            for (int round = 0; round < rounds; round++) {
                kept &= same(block, held, k);
            }
            tell(out, kept ? 'y' : 'n');
        } else if (command == 'v') {
            tell(out, same(block, held, k) ? 'y' : 'n');
        } else {
            _exit(same(block, held, k) ? 0 : 1);
        }
    }
}

static void filler(int in, int out) {
    size_t taken = 0;
    while (malloc(1 << 20)) {
        taken++;
    }
    assert(write(out, &taken, sizeof taken) == sizeof taken);
    hear(in);
    _exit(0);
}

int main(int argc, char** argv) {
    int running = argc > 1 && strcmp(argv[1], "running") == 0;
    int in[count + 1][2], out[count + 1][2];
    pid_t pid[count + 1];
    size_t region = 0;
    char line[160];

    for (int k = 0; k <= count; k++) {
        assert(pipe(in[k]) == 0 && pipe(out[k]) == 0);
        pid[k] = fork();
        assert(pid[k] >= 0);
        if (pid[k] == 0 && k < count) {
            child((unsigned)k, in[k][0], out[k][1]);
        }
        if (pid[k] == 0) {
            filler(in[k][0], out[k][1]);
        }
        if (k < count) {
            assert(read(out[k][0], &region, sizeof region) == sizeof region);
        }
    }

    size_t taken;
    assert(read(out[count][0], &taken, sizeof taken) == sizeof taken);
    snprintf(line, sizeof line, "filler: %s\n", taken > 0 ? "took the memory left" : "took nothing");
    say(line);

    pid_t none = fork();
    if (none == 0) {
        _exit(0);
    }
    say(none < 0 && errno == ENOMEM ? "fork with no room: ENOMEM\n" : "fork with no room: forked\n");

    int ended = 0;
    for (int k = 0; k < count; k += 2) {
        int status;
        tell(in[k][1], 'x');
        assert(waitpid(pid[k], &status, 0) == pid[k]);
        ended += WIFEXITED(status) && WEXITSTATUS(status) == 0;
    }
    snprintf(line, sizeof line, "ended: %d children kept their blocks\n", ended);
    say(line);

    size_t more = region * 5 / 2;
    if (running) {
        tell(in[3][1], 'b');
    }
    tell(in[1][1], 'g');
    assert(write(in[1][1], &more, sizeof more) == sizeof more);
    snprintf(line, sizeof line, "grown: %c\n", hear(out[1][0]));
    say(line);
    if (!running) {
        tell(in[3][1], 'v');
    }
    snprintf(line, sizeof line, "fourth: %c\n", hear(out[3][0]));
    say(line);
    tell(in[5][1], 'v');
    snprintf(line, sizeof line, "sixth: %c\n", hear(out[5][0]));
    say(line);

    int kept = 0;
    const int rest[] = {1, 3, 5, count};
    for (int n = 0; n < 4; n++) {
        int k = rest[n], status;
        tell(in[k][1], 'x');
        assert(waitpid(pid[k], &status, 0) == pid[k]);
        kept += WIFEXITED(status) && WEXITSTATUS(status) == 0;
    }
    snprintf(line, sizeof line, "end: %d of 4 ended clean\n", kept);
    say(line);
    return 0;
}
