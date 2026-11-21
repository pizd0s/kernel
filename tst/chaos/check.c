#include <assert.h>
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/mman.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/wait.h>
#include <unistd.h>

// A handler that takes memory, which may move it: the kernel's
// memory-move point moves it as every handler starts anyway.
static void* taken[16];
static int takes;

static void take(int sig) {
    (void)sig;
    if (takes < 16) {
        char* block = malloc(1 << 20);
        assert(block);
        memset(block, takes, 1 << 20);
        taken[takes++] = block;
    }
}

// A child that writes text into fd once the parent waits for it.
static pid_t later(int fd, const char* text) {
    pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        usleep(100000);
        assert(write(fd, text, strlen(text)) == (ssize_t)strlen(text));
        _exit(0);
    }
    return child;
}

// A wait that a handler interrupts while the memory may move under it: an
// epoll_wait, then a read of a pipe, each into memory of the heap; what
// they bring is what was sent.
static int waitsMoved(void) {
    struct sigaction action = {0};
    action.sa_handler = take;
    action.sa_flags = SA_RESTART;
    assert(sigaction(SIGALRM, &action, NULL) == 0);
    struct itimerval timer = {{0, 20000}, {0, 20000}};
    int pipes[2];
    assert(pipe(pipes) == 0);
    int ep = epoll_create1(0);
    struct epoll_event event = {.events = EPOLLIN, .data.u64 = 0x1122334455667788ull};
    assert(ep >= 0 && epoll_ctl(ep, EPOLL_CTL_ADD, pipes[0], &event) == 0);
    struct epoll_event* events = calloc(4, sizeof *events);
    char* bytes = calloc(16, 1);
    assert(events && bytes);
    assert(setitimer(ITIMER_REAL, &timer, NULL) == 0);
    pid_t first = later(pipes[1], "moved");
    int n;
    do {
        n = epoll_wait(ep, events, 4, -1);
    } while (n < 0 && errno == EINTR);
    int wrong = n != 1 || events[0].data.u64 != 0x1122334455667788ull || (events[0].events & EPOLLIN) == 0;
    ssize_t got = read(pipes[0], bytes, 16);
    wrong |= got != 5 || memcmp(bytes, "moved", 5) != 0;
    pid_t second = later(pipes[1], "again");
    do {
        got = read(pipes[0], bytes, 16);
    } while (got < 0 && errno == EINTR);
    wrong |= got != 5 || memcmp(bytes, "again", 5) != 0;
    struct itimerval off = {{0, 0}, {0, 0}};
    assert(setitimer(ITIMER_REAL, &off, NULL) == 0);
    int status;
    assert(waitpid(first, &status, 0) == first && waitpid(second, &status, 0) == second);
    free(events);
    free(bytes);
    assert(close(ep) == 0 && close(pipes[0]) == 0 && close(pipes[1]) == 0);
    return wrong;
}

// The code a handler interrupts at any of its instructions while the memory
// moves: calls through pointers, lib/c's string functions and the heap's
// bytes, which must give what they give with no interruption. The handler
// takes a little memory, which the memory outgrows its room by.
static volatile int ticks;
static void* grabbed[8];

static void tick(int sig) {
    (void)sig;
    if (ticks < 8) {
        char* block = malloc(1 << 18);
        assert(block);
        memset(block, ticks, 1 << 18);
        grabbed[ticks] = block;
    }
    ticks++;
}

static unsigned mix(unsigned x) {
    return x * 2654435761u ^ x >> 13;
}

static unsigned twist(unsigned x) {
    return (x << 7 | x >> 25) + 0x9e3779b9u;
}

static unsigned (*volatile steps[2])(unsigned) = {mix, twist};

static unsigned stir(char* text, char* copy, unsigned x) {
    for (int i = 0; i < 64; i++) {
        x = steps[x & 1](x);
        text[x % 4000] = (char)('a' + x % 26);
    }
    memcpy(copy, text, 4001);
    x += (unsigned)strlen(copy);
    char* q = memchr(copy, 'q', 4000);
    x += q ? (unsigned)(q - copy) : 7;
    char* z = strchr(copy, 'z');
    x += z ? (unsigned)(z - copy) : 11;
    return x;
}

static unsigned stirred(char* text, char* copy, int rounds) {
    memset(text, 'b', 4000);
    text[4000] = 0;
    unsigned x = 1;
    for (int r = 0; r < rounds; r++) {
        x = stir(text, copy, x);
    }
    return x;
}

// The loop runs until it has been interrupted 32 times, then once more as
// many rounds with no timer.
static int loopsMoved(void) {
    char* text = malloc(4001);
    char* copy = malloc(4001);
    assert(text && copy);
    struct sigaction action = {0};
    action.sa_handler = tick;
    action.sa_flags = SA_RESTART;
    assert(sigaction(SIGALRM, &action, NULL) == 0);
    struct itimerval timer = {{0, 2000}, {0, 2000}};
    memset(text, 'b', 4000);
    text[4000] = 0;
    unsigned got = 1;
    int rounds = 0;
    assert(setitimer(ITIMER_REAL, &timer, NULL) == 0);
    for (; ticks < 32 && rounds < 10000000; rounds++) {
        got = stir(text, copy, got);
    }
    struct itimerval off = {{0, 0}, {0, 0}};
    assert(setitimer(ITIMER_REAL, &off, NULL) == 0);
    unsigned want = stirred(text, copy, rounds);
    free(text);
    free(copy);
    if (got != want || ticks < 32) {
        fprintf(stderr, "loops: %#x, not %#x, after %d interruptions in %d rounds\n", got, want, ticks, rounds);
        return 1;
    }
    return 0;
}

// The stack grows into blocks the kernel takes from the heap, each block a
// growth of the memory, which the rule moves: the frames of every block,
// of a descent that took them and of one that finds them taken, keep the
// arrays they hold.
static volatile int broken;

__attribute__((noinline)) static unsigned climb(int depth, unsigned seed) {
    volatile unsigned a[64];
    for (int i = 0; i < 64; i++) {
        a[i] = mix(seed + (unsigned)i);
    }
    unsigned r = depth > 0 ? climb(depth - 1, a[depth % 64]) : seed;
    for (int i = 0; i < 64; i++) {
        if (a[i] != mix(seed + (unsigned)i)) {
            broken = 1;
        }
    }
    return r + a[63];
}

static int stacksMoved(void) {
    unsigned first = climb(4000, 3);
    unsigned again = climb(4000, 3);
    if (broken || first != again) {
        fprintf(stderr, "stacks: %#x, then %#x, broken %d\n", first, again, broken);
        return 1;
    }
    return 0;
}

// With memory-compact every growth of a memory slides every region of the
// guests' memory that can move toward its start, each by the process that
// runs on it: a child that waits in a read and one that runs a loop which
// makes no call move as the parent grows its memory, and keep what they
// hold.
static int compactMoved(void) {
    enum { size = 1 << 20 };
    int ready[2], go[2];
    assert(pipe(ready) == 0 && pipe(go) == 0);
    pid_t children[2];
    for (int k = 0; k < 2; ++k) {
        children[k] = fork();
        assert(children[k] >= 0);
        if (children[k] == 0) {
            unsigned char* block = malloc(size);
            assert(block);
            for (int i = 0; i < size; ++i) {
                block[i] = (unsigned char)mix((unsigned)(i + k));
            }
            assert(write(ready[1], "r", 1) == 1);
            int kept = 1;
            if (k == 0) {
                char c;
                assert(read(go[0], &c, 1) == 1);
            } else {
                for (int round = 0; round < 64; ++round) {
                    for (int i = 0; i < size; ++i) {
                        kept &= block[i] == (unsigned char)mix((unsigned)(i + k));
                    }
                }
            }
            for (int i = 0; i < size; ++i) {
                kept &= block[i] == (unsigned char)mix((unsigned)(i + k));
            }
            _exit(kept ? 0 : 1);
        }
    }
    char c;
    for (int k = 0; k < 2; ++k) {
        assert(read(ready[0], &c, 1) == 1);
    }
    for (int k = 0; k < 8; ++k) {
        assert(__builtin_wasm_memory_grow(0, 1) != (size_t)-1);
    }
    assert(write(go[1], "g", 1) == 1);
    int wrong = 0;
    for (int k = 0; k < 2; ++k) {
        int status;
        assert(waitpid(children[k], &status, 0) == children[k]);
        wrong |= !WIFEXITED(status) || WEXITSTATUS(status) != 0;
    }
    assert(close(ready[0]) == 0 && close(ready[1]) == 0 && close(go[0]) == 0 && close(go[1]) == 0);
    if (wrong) {
        fprintf(stderr, "compaction: a child's block changed as it moved\n");
    }
    return wrong;
}

static int attempt(const char* operation) {
    if (strcmp(operation, "memory-compact") == 0) {
        return compactMoved();
    }
    if (strcmp(operation, "memory-move") == 0) {
        int wrong = loopsMoved();
        wrong |= waitsMoved();
        wrong |= stacksMoved();
        return wrong;
    }
    if (strcmp(operation, "system") == 0) {
        // A shell that cannot be loaded ends with 127, as system reports.
        int status = system("exit 7");
        if (WIFEXITED(status) && WEXITSTATUS(status) == 127) {
            return 1;
        }
        assert(WIFEXITED(status) && WEXITSTATUS(status) == 7);
        return 0;
    }
    if (strcmp(operation, "daemon") == 0) {
        int saved[3];
        for (int fd = 0; fd < 3; ++fd) {
            saved[fd] = dup(fd);
            assert(saved[fd] >= 0);
        }
        int result = daemon(1, 0), error = errno;
        for (int fd = 0; fd < 3; ++fd) {
            assert(dup2(saved[fd], fd) == fd && close(saved[fd]) == 0);
        }
        assert(result == 0 || (result == -1 && error == EIO));
        return result < 0;
    }
    if (strcmp(operation, "chdir") == 0) {
        int result = chdir("/");
        assert(result == 0 || (result == -1 && errno == EIO));
        return result < 0;
    }
    if (strcmp(operation, "relative-open") == 0 || strcmp(operation, "fexecve") == 0) {
        int execute = strcmp(operation, "fexecve") == 0;
        int fd = open(execute ? "/dev/null" : "/", O_RDONLY);
        assert(fd >= 0);
        int result;
        if (execute) {
            char* args[] = {"null", NULL};
            result = fexecve(fd, args, NULL);
        } else {
            result = openat(fd, "dev/null", O_RDONLY);
        }
        int failed = result < 0 && errno == EIO;
        assert(failed || (execute ? result == -1 && errno == EACCES : result >= 0));
        if (!execute && result >= 0) {
            assert(close(result) == 0);
        }
        assert(close(fd) == 0);
        return failed;
    }
    if (strcmp(operation, "fstat") == 0 || strcmp(operation, "fstatat") == 0 || strcmp(operation, "stat") == 0 || strcmp(operation, "lstat") == 0) {
        struct stat metadata;
        memset(&metadata, 0x5a, sizeof(metadata));
        int result;
        if (strcmp(operation, "fstat") == 0) {
            int fd = open("/dev/null", O_RDONLY);
            assert(fd >= 0);
            result = fstat(fd, &metadata);
            int error = errno;
            assert(close(fd) == 0);
            errno = error;
        } else if (strcmp(operation, "fstatat") == 0) {
            result = fstatat(AT_FDCWD, "/dev/null", &metadata, 0);
        } else if (strcmp(operation, "stat") == 0) {
            result = stat("/dev/null", &metadata);
        } else {
            result = lstat("/dev/null", &metadata);
        }
        if (result < 0) {
            assert(errno == EIO);
            const unsigned char* bytes = (const unsigned char*)&metadata;
            for (unsigned i = 0; i < sizeof(metadata); ++i) {
                assert(bytes[i] == 0x5a);
            }
            return 1;
        }
        assert(result == 0 && S_ISCHR(metadata.st_mode));
        return 0;
    }
    if (strcmp(operation, "fdopendir") == 0) {
        int fd = open("/", O_RDONLY | O_DIRECTORY);
        assert(fd >= 0);
        DIR* dir = fdopendir(fd);
        if (dir == NULL) {
            assert(errno == EIO && close(fd) == 0);
            return 1;
        }
        assert(closedir(dir) == 0);
        return 0;
    }
    if (strcmp(operation, "msync") == 0) {
        int fd = open("/run/mapping", O_CREAT | O_TRUNC | O_RDWR, 0600);
        assert(fd >= 0 && ftruncate(fd, 4096) == 0);
        char* mapping = mmap(NULL, 4096, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
        assert(mapping != MAP_FAILED);
        mapping[0] = 'x';
        int result = msync(mapping, 4096, MS_SYNC);
        int failed = result < 0;
        assert(failed ? errno == EIO : result == 0);
        assert(mapping[0] == 'x' && munmap(mapping, 4096) == 0);
        char byte;
        assert(pread(fd, &byte, 1, 0) == 1 && byte == 'x');
        assert(close(fd) == 0 && unlink("/run/mapping") == 0);
        return failed;
    }
    if (strcmp(operation, "memory-grow") == 0) {
        size_t before = __builtin_wasm_memory_size(0);
        size_t old = __builtin_wasm_memory_grow(0, 1);
        if (old == (size_t)-1) {
            assert(__builtin_wasm_memory_size(0) == before);
            return 1;
        }
        assert(old == before && __builtin_wasm_memory_size(0) == before + 1);
        volatile char* last = (volatile char*)((before + 1) * 65536 - 1);
        *last = 'x';
        assert(*last == 'x');
        return 0;
    }
    if (strcmp(operation, "fork") == 0) {
        pid_t child = fork();
        if (child < 0) {
            assert(errno == EAGAIN);
            return 1;
        }
        if (child == 0) {
            _exit(7);
        }
        int status;
        assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 7);
        return 0;
    }
    if (strcmp(operation, "openat") == 0) {
        int fd = open("/dev/null", O_RDONLY);
        if (fd < 0) {
            assert(errno == EIO);
            return 1;
        }
        assert(close(fd) == 0);
        return 0;
    }
    if (strcmp(operation, "read") == 0) {
        char byte;
        ssize_t size = read(0, &byte, 1);
        if (size < 0) {
            assert(errno == EIO);
            return 1;
        }
        assert(size == 1 && byte == 'x');
        return 0;
    }
    assert(strcmp(operation, "write") == 0);
    int fd = open("/dev/null", O_WRONLY);
    assert(fd >= 0);
    ssize_t size = write(fd, "x", 1);
    int failed = size < 0;
    assert(failed ? errno == EIO : size == 1);
    assert(close(fd) == 0);
    return failed;
}

int main(int argc, char** argv) {
    assert(argc == 2);
    int failures = 0;
    for (int i = 0; i < 16; ++i) {
        failures += attempt(argv[1]);
    }
    printf("%s: failures=%d\n", argv[1], failures);
    return 0;
}
