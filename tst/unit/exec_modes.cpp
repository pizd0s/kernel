//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "bad modes, classes and sinks are refused\na trace: the image's calls with their errors, its child, its signal, till it ends the trace\nthe mode goes through a fork and an exec\na full sink stops the process, a read lets it go on, no line lost\na sink whose reader is gone ends the trace\nthe sanitizer of a mode reports to the standard error, through a shell\nthe sanitizer's report of a traced process goes to its sink\n"

// The modes of a process's images (lib/c's sys/execmode.h): a process sets
// them for the images it execs from then on and for its children's. A
// trace is lines in a sink, here a pipe, written by the kernel as the
// process calls, execs, takes signals and ends, and as its children start; the process waits
// for room in the sink as it would in a write of its own, and goes on
// untraced once nobody reads it. The sanitizer's mode compiles an image
// with its checks, whatever started the system.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/execmode.h>
#include <sys/wait.h>
#include <unistd.h>

static const char* self = "/tests/unit/exec_modes";
static const int manyCalls = 20000;

static void handler(int) {
}

// The traced image: a call that fails, a child, a signal, and the end of
// its own trace, after which nothing is written.
static int calls() {
    assert(open("/tmp/none", O_RDONLY) == -1 && errno == ENOENT);
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        _exit(3);
    }
    int status;
    assert(waitpid(child, &status, 0) == child && WEXITSTATUS(status) == 3);
    assert(signal(SIGUSR1, handler) != SIG_ERR && raise(SIGUSR1) == 0);
    assert(execmode(0, 0, -1) == 0);
    assert(access("/tmp/untraced", F_OK) == -1);
    return 5;
}

// Calls enough to fill a pipe many times over, then a byte to done.
static int many(int done) {
    for (int i = 0; i < manyCalls; ++i) {
        getppid();
    }
    assert(write(done, "d", 1) == 1);
    return 9;
}

static int overflow() {
    char* volatile block = static_cast<char*>(malloc(20));
    block[20] = 1;
    return 0;
}

// Everything written to fd till its end.
static size_t drain(int fd, char* text, size_t cap) {
    size_t size = 0;
    for (;;) {
        const ssize_t n = read(fd, text + size, cap - 1 - size);
        assert(n >= 0);
        if (n == 0) {
            break;
        }
        size += n;
        assert(size < cap - 1);
    }
    text[size] = 0;
    return size;
}

// Whether text has the lines of pid that begin with each of parts, in
// their order; a part's "..." stands for anything up to its rest.
static bool has(const char* text, pid_t pid, const char* const* parts, int count) {
    char prefix[32];
    snprintf(prefix, sizeof(prefix), "[%d] ", int(pid));
    int found = 0;
    for (const char* line = text; *line && found < count; line = strchr(line, '\n') + 1) {
        const size_t length = strcspn(line, "\n");
        if (strncmp(line, prefix, strlen(prefix)) != 0) {
            continue;
        }
        const char* at = line + strlen(prefix);
        const char* end = line + length;
        const char* part = parts[found];
        const char* gap = strstr(part, "...");
        const size_t head = gap ? size_t(gap - part) : strlen(part);
        const char* tail = gap ? gap + 3 : "";
        if (size_t(end - at) >= head + strlen(tail) && strncmp(at, part, head) == 0 &&
            strncmp(end - strlen(tail), tail, strlen(tail)) == 0 && (gap || size_t(end - at) == head)) {
            ++found;
        }
    }
    return found == count;
}

// The pid whose line says it is a child of pid.
static pid_t childOf(const char* text, pid_t pid) {
    char suffix[32];
    snprintf(suffix, sizeof(suffix), "] child of %d\n", int(pid));
    const char* at = strstr(text, suffix);
    while (at && at > text && at[-1] != '[') {
        --at;
    }
    return at ? atoi(at) : -1;
}

// A child that sets the mode, closes its own descriptor of the sink, which
// the kernel keeps, and execs argv; sink and done are what it gets as
// such, the rest of the pipes closed.
static pid_t start(unsigned modes, unsigned classes, int sink, const int* closed, int nclosed, const char* const* argv) {
    const pid_t child = fork();
    assert(child >= 0);
    if (child != 0) {
        return child;
    }
    for (int i = 0; i < nclosed; ++i) {
        close(closed[i]);
    }
    if (execmode(modes, classes, sink) != 0) {
        _exit(100);
    }
    if (sink > STDERR_FILENO) {
        close(sink);
    }
    execv(argv[0], const_cast<char* const*>(argv));
    _exit(101);
}

static int ended(pid_t child) {
    int status;
    assert(waitpid(child, &status, 0) == child);
    return status;
}

int main(int argc, char** argv) {
    if (argc == 2 && strcmp(argv[1], "calls") == 0) {
        return calls();
    }
    if (argc == 3 && strcmp(argv[1], "many") == 0) {
        return many(atoi(argv[2]));
    }
    if (argc == 2 && strcmp(argv[1], "overflow") == 0) {
        return overflow();
    }
    static char text[1 << 20];

    assert(execmode(4, 0, -1) == -1 && errno == EINVAL);
    assert(execmode(EXEC_TRACE, 32, STDERR_FILENO) == -1 && errno == EINVAL);
    assert(execmode(EXEC_TRACE, TRACE_SYSTEM, -1) == -1 && errno == EINVAL);
    assert(execmode(EXEC_TRACE, 0, STDERR_FILENO) == -1 && errno == EINVAL);
    assert(execmode(EXEC_TRACE, TRACE_SYSTEM, 99) == -1 && errno == EBADF);
    assert(execmode(0, TRACE_SYSTEM, -1) == -1 && errno == EINVAL);
    assert(execmode(EXEC_ASAN, 0, STDERR_FILENO) == -1 && errno == EINVAL);
    assert(execmode(0, 0, -1) == 0);
    printf("bad modes, classes and sinks are refused\n");

    {
        int sink[2];
        assert(pipe(sink) == 0);
        const char* const command[] = {self, "calls", nullptr};
        const pid_t child = start(EXEC_TRACE, TRACE_SYSTEM | TRACE_PROCESS | TRACE_SIGNALS, sink[1], sink, 1, command);
        close(sink[1]);
        drain(sink[0], text, sizeof(text));
        close(sink[0]);
        const int status = ended(child);
        assert(WIFEXITED(status) && WEXITSTATUS(status) == 5);
        const char* const lines[] = {
            "exec \"/tests/unit/exec_modes\"",
            "open(\"/tmp/none\", ...) = -1 ENOENT (no such file or directory)",
            "waitpid(...",
            "signal SIGUSR1",
        };
        assert(has(text, child, lines, 4));
        const pid_t grandchild = childOf(text, child);
        const char* const childLines[] = {"child of ...", "exit 3"};
        assert(grandchild > 0 && has(text, grandchild, childLines, 2));
        const char* const after[] = {"access(..."};
        const char* const end[] = {"exit 5"};
        assert(!has(text, child, after, 1) && !has(text, child, end, 1));
        printf("a trace: the image's calls with their errors, its child, its signal, till it ends the trace\n");
    }

    {
        int sink[2];
        assert(pipe(sink) == 0);
        const char* const command[] = {"/bin/sh", "-c", "/bin/true; exit 7", nullptr};
        const pid_t child = start(EXEC_TRACE, TRACE_PROCESS, sink[1], sink, 1, command);
        close(sink[1]);
        drain(sink[0], text, sizeof(text));
        close(sink[0]);
        const int status = ended(child);
        assert(WIFEXITED(status) && WEXITSTATUS(status) == 7);
        const char* const lines[] = {"exec \"/bin/sh\"", "exit 7"};
        assert(has(text, child, lines, 2));
        const pid_t grandchild = childOf(text, child);
        const char* const childLines[] = {"child of ...", "exec \"/bin/true\"", "exit 0"};
        assert(grandchild > 0 && has(text, grandchild, childLines, 3));
        printf("the mode goes through a fork and an exec\n");
    }

    for (int reads = 1; reads >= 0; --reads) {
        int sink[2], done[2];
        assert(pipe(sink) == 0 && pipe(done) == 0);
        char number[16];
        snprintf(number, sizeof(number), "%d", done[1]);
        const char* const command[] = {self, "many", number, nullptr};
        const int closed[] = {sink[0], done[0]};
        const pid_t child = start(EXEC_TRACE, TRACE_SYSTEM, sink[1], closed, 2, command);
        close(sink[1]);
        close(done[1]);
        pollfd waited{done[0], POLLIN, 0};
        assert(poll(&waited, 1, 300) == 0);
        if (reads) {
            drain(sink[0], text, sizeof(text));
            int count = 0;
            for (const char* at = text; (at = strstr(at, "] getppid() = ")); ++at) {
                ++count;
            }
            assert(count == manyCalls);
        }
        close(sink[0]);
        char byte;
        assert(read(done[0], &byte, 1) == 1 && byte == 'd');
        close(done[0]);
        const int status = ended(child);
        assert(WIFEXITED(status) && WEXITSTATUS(status) == 9);
        printf(reads ? "a full sink stops the process, a read lets it go on, no line lost\n" : "a sink whose reader is gone ends the trace\n");
    }

    for (int traced = 0; traced < 2; ++traced) {
        int sink[2], error[2];
        assert(pipe(sink) == 0 && pipe(error) == 0);
        const pid_t child = fork();
        assert(child >= 0);
        if (child == 0) {
            close(sink[0]);
            close(error[0]);
            assert(dup2(error[1], STDERR_FILENO) == STDERR_FILENO);
            close(error[1]);
            if (traced) {
                assert(execmode(EXEC_ASAN | EXEC_TRACE, TRACE_FAULTS, sink[1]) == 0);
                execl(self, self, "overflow", nullptr);
            } else {
                assert(execmode(EXEC_ASAN, 0, -1) == 0);
                execl("/bin/sh", "sh", "-c", "/tests/unit/exec_modes overflow; exit $?", nullptr);
            }
            _exit(101);
        }
        close(sink[1]);
        close(error[1]);
        static char errors[1 << 16];
        drain(sink[0], text, sizeof(text));
        drain(error[0], errors, sizeof(errors));
        close(sink[0]);
        close(error[0]);
        const int status = ended(child);
        const char* report = "asan: 1-byte store at ";
        const char* where = "past the end of the 20-byte block at ";
        if (traced) {
            assert(WIFSIGNALED(status) && WTERMSIG(status) == SIGSEGV);
            assert(strstr(text, report) && strstr(text, where) && errors[0] == 0);
            printf("the sanitizer's report of a traced process goes to its sink\n");
        } else {
            assert(WIFEXITED(status) && WEXITSTATUS(status) == 128 + SIGSEGV);
            assert(strstr(errors, report) && strstr(errors, where) && text[0] == 0);
            printf("the sanitizer of a mode reports to the standard error, through a shell\n");
        }
    }
    assert(access("/tmp/none", F_OK) == -1 && overflow() == 0);
    return 0;
}
