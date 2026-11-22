//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "statuses 3 4 5 6\nprogram messages: ok\n"
//@ stderr: "check: open a: No such file or directory\ncheck: plain 1\ncheck: Permission denied\ncheck: \ncheck: vwarn: Interrupted system call\ncheck: vwarnx\ncheck: err 3: Not a directory\ncheck: errx\ncheck: verr: Is a directory\ncheck: verrx\n"

// err.h's messages, BSD's: the program's name, the message, and for warn
// and err ": " and what errno named at the call, a line on stderr; err and
// errx then exit with their status. The name is
// program_invocation_short_name, set here so the host's and the system's
// agree.

#include <assert.h>
#include <err.h>
#include <errno.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/wait.h>
#include <unistd.h>

namespace {
    void viaWarn(const char* format, ...) {
        va_list ap;
        va_start(ap, format);
        vwarn(format, ap);
        va_end(ap);
    }

    void viaWarnx(const char* format, ...) {
        va_list ap;
        va_start(ap, format);
        vwarnx(format, ap);
        va_end(ap);
    }

    [[noreturn]] void viaErr(int status, const char* format, ...) {
        va_list ap;
        va_start(ap, format);
        verr(status, format, ap);
    }

    [[noreturn]] void viaErrx(int status, const char* format, ...) {
        va_list ap;
        va_start(ap, format);
        verrx(status, format, ap);
    }

    // The status a child ends with that runs f.
    int status(void (*f)()) {
        fflush(stdout);
        pid_t pid = fork();
        assert(pid >= 0);
        if (pid == 0) {
            f();
            _exit(99);
        }
        int s;
        assert(waitpid(pid, &s, 0) == pid && WIFEXITED(s));
        return WEXITSTATUS(s);
    }
}

int main() {
    program_invocation_short_name = (char*)"check";
    errno = ENOENT;
    warn("open %s", "a");
    assert(errno == ENOENT);
    warnx("plain %d", 1);
    errno = EACCES;
    warn(nullptr);
    warnx(nullptr);
    errno = EINTR;
    viaWarn("v%s", "warn");
    viaWarnx("v%s", "warnx");
    int a = status([] {
        errno = ENOTDIR;
        err(3, "err %d", 3);
    });
    int b = status([] { errx(4, "errx"); });
    int c = status([] {
        errno = EISDIR;
        viaErr(5, "verr");
    });
    int d = status([] { viaErrx(6, "verrx"); });
    printf("statuses %d %d %d %d\n", a, b, c, d);
    printf("program messages: ok\n");
}
