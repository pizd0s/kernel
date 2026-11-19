// spawn: runs a command in a mode of the kernel's (execmode, sys/execmode.h):
// compiled with the checks of the address sanitizer, or traced, what it
// does written to spawn's standard error or to a file. The command's
// children run so too. spawn becomes the command, or with --wait runs it
// as a child and ends as it ends.
//
//     spawn [--asan] [--strace[=class,...]] [--out file] [--exec | --wait] -- command [arg...]
//
// The classes of a trace are system, the calls of the system; library, the
// calls of libc's own functions; process, children, execs and the end;
// signals; faults. --strace alone is all of them but library.

#include <bin/tools/tools.h>

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/execmode.h>
#include <sys/wait.h>
#include <unistd.h>

namespace {
    struct Class {
        const char* name;
        unsigned bit;
    };
}

static const Class classes[] = {
    {"system", TRACE_SYSTEM}, {"library", TRACE_LIBRARY}, {"process", TRACE_PROCESS}, {"signals", TRACE_SIGNALS}, {"faults", TRACE_FAULTS},
};

static int usage() {
    dprintf(STDERR_FILENO, "usage: spawn [--asan] [--strace[=class,...]] [--out file] [--exec | --wait] -- command [arg...]\nclasses:");
    for (const Class& c : classes) {
        dprintf(STDERR_FILENO, " %s", c.name);
    }
    dprintf(STDERR_FILENO, "\n");
    return 2;
}

// The classes a list names, 0 for one that names none of them.
static unsigned classesOf(const char* list) {
    unsigned out = 0;
    while (*list) {
        const size_t n = strcspn(list, ",");
        unsigned bit = 0;
        for (const Class& c : classes) {
            if (strlen(c.name) == n && strncmp(c.name, list, n) == 0) {
                bit = c.bit;
            }
        }
        if (bit == 0) {
            return 0;
        }
        out |= bit;
        list += n + (list[n] == ',');
    }
    return out;
}

int spawnMain(int argc, char** argv) {
    unsigned modes = 0;
    unsigned traced = 0;
    const char* out = nullptr;
    bool wait = false;
    int at = 1;
    for (; at < argc && strcmp(argv[at], "--") != 0; ++at) {
        const char* a = argv[at];
        if (strcmp(a, "--asan") == 0) {
            modes |= EXEC_ASAN;
        } else if (strcmp(a, "--strace") == 0) {
            traced = TRACE_SYSTEM | TRACE_PROCESS | TRACE_SIGNALS | TRACE_FAULTS;
        } else if (strncmp(a, "--strace=", 9) == 0) {
            if ((traced = classesOf(a + 9)) == 0) {
                return usage();
            }
        } else if (strcmp(a, "--out") == 0 && at + 1 < argc) {
            out = argv[++at];
        } else if (strcmp(a, "--exec") == 0) {
            wait = false;
        } else if (strcmp(a, "--wait") == 0) {
            wait = true;
        } else {
            return usage();
        }
    }
    if (at + 1 >= argc || (out && traced == 0)) {
        return usage();
    }
    char** command = argv + at + 1;

    int sink = -1;
    if (traced) {
        modes |= EXEC_TRACE;
        sink = out ? open(out, O_WRONLY | O_CREAT | O_APPEND | O_CLOEXEC, 0644) : STDERR_FILENO;
        if (sink < 0) {
            dprintf(STDERR_FILENO, "spawn: %s: %s\n", out, strerror(errno));
            return 1;
        }
    }

    const pid_t child = wait ? fork() : 0;
    if (child < 0) {
        dprintf(STDERR_FILENO, "spawn: fork: %s\n", strerror(errno));
        return 1;
    }
    if (child == 0) {
        if (execmode(modes, traced, sink) != 0) {
            dprintf(STDERR_FILENO, "spawn: execmode: %s\n", strerror(errno));
            _exit(126);
        }
        execvp(command[0], command);
        dprintf(STDERR_FILENO, "spawn: %s: %s\n", command[0], strerror(errno));
        _exit(errno == ENOENT ? 127 : 126);
    }
    int status = 0;
    while (waitpid(child, &status, 0) < 0) {
        if (errno != EINTR) {
            return 1;
        }
    }
    return WIFSIGNALED(status) ? 128 + WTERMSIG(status) : WEXITSTATUS(status);
}
