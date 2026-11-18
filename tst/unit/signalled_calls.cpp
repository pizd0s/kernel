//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "calls under signals: all answered, handlers ran\n"

// A signal a process takes while a call of its waits for a server's
// answer runs its handler and leaves the call as it is: the answer is the
// call's, whatever the server makes of the signal's word to it (CANCEL,
// lib/protocol/protocol.h). A child signals the process as fast as it
// can while it stats, opens, reads and lists, and every call answers as
// it does undisturbed.

#include <assert.h>
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

static volatile sig_atomic_t handled;

static void handler(int) {
    handled = handled + 1;
}

int main() {
    alarm(120);
    struct sigaction action{};
    action.sa_handler = handler;
    action.sa_flags = SA_RESTART;
    assert(sigaction(SIGUSR1, &action, nullptr) == 0);
    char name[] = "signalled_calls_XXXXXX";
    const int made = mkstemp(name);
    assert(made >= 0 && write(made, "twelve bytes", 12) == 12 && close(made) == 0);
    fflush(stdout);
    const pid_t parent = getpid();
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        for (int i = 0; i < 20000; ++i) {
            if (kill(parent, SIGUSR1) != 0) {
                _exit(1);
            }
        }
        _exit(0);
    }
    int status = 0;
    long calls = 0;
    bool answered = true;
    while (waitpid(child, &status, WNOHANG) == 0) {
        struct stat st;
        char bytes[16];
        answered = answered && stat(".", &st) == 0 && S_ISDIR(st.st_mode);
        answered = answered && stat(name, &st) == 0 && st.st_size == 12;
        answered = answered && stat("signalled_calls_none", &st) == -1 && errno == ENOENT;
        const int fd = open(name, O_RDONLY);
        answered = answered && fd >= 0 && fstat(fd, &st) == 0 && st.st_size == 12;
        answered = answered && read(fd, bytes, sizeof bytes) == 12 && memcmp(bytes, "twelve bytes", 12) == 0;
        answered = answered && close(fd) == 0;
        DIR* dir = opendir(".");
        bool found = false;
        for (dirent* e = dir ? readdir(dir) : nullptr; e; e = readdir(dir)) {
            found = found || strcmp(e->d_name, name) == 0;
        }
        answered = answered && dir && found && closedir(dir) == 0;
        if (!answered) {
            printf("call %ld failed: errno %d\n", calls, errno);
            break;
        }
        ++calls;
    }
    kill(child, SIGKILL);
    waitpid(child, &status, 0);
    unlink(name);
    printf("calls under signals: %s, %s\n", answered && calls > 0 ? "all answered" : "some failed", handled > 0 ? "handlers ran" : "no handler ran");
    return 0;
}
