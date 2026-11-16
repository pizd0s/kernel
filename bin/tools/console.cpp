// console: runs a program on a new pseudoterminal joined to the console,
// as a terminal emulator does: what comes from the console goes to the
// terminal's master, what the master reads goes to the console, and the
// console's window is the terminal's, again as SIGWINCH says it changed.
// The program leads a session the terminal controls (forkpty). console
// ends with the program, once it wrote out what the program left, its
// status the program's: an exit's, or 128 and the signal that ended it.
// rootfs runs the shell so on a console with a window, a host's terminal
// (bin/vm/kernel_console.go).
//
//     console <path> <arg0> [arg...]

#include <bin/tools/tools.h>
#include <lib/protocol/service.h>

#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <pty.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/wait.h>
#include <unistd.h>

static volatile sig_atomic_t resized = 0;
static volatile sig_atomic_t ended = 0;

static void resize(int) {
    resized = 1;
}

static void end(int) {
    ended = 1;
}

static bool writeAll(int fd, const char* bytes, size_t n) {
    while (n > 0) {
        const ssize_t wrote = write(fd, bytes, n);
        if (wrote < 0 && errno == EINTR) {
            continue;
        }
        if (wrote <= 0) {
            return false;
        }
        bytes += wrote;
        n -= size_t(wrote);
    }
    return true;
}

// Moves what the master has to the console: false once it has no more to
// give, its slave closed.
static bool fromMaster(int master) {
    char bytes[4096];
    const ssize_t n = read(master, bytes, sizeof bytes);
    if (n > 0) {
        return writeAll(STDOUT_FILENO, bytes, size_t(n));
    }
    return n < 0 && (errno == EAGAIN || errno == EINTR);
}

int consoleMain(int argc, char** argv) {
    if (argc < 3) {
        dprintf(STDERR_FILENO, "usage: console <path> <arg0> [arg...]\n");
        return 2;
    }
    winsize size{};
    const bool window = ioctl(STDIN_FILENO, TIOCGWINSZ, &size) == 0;
    int master;
    const pid_t child = forkpty(&master, nullptr, nullptr, window ? &size : nullptr);
    if (child < 0) {
        fatal("console: forkpty: %s", strerror(errno));
    }
    if (child == 0) {
        execv(argv[1], argv + 2);
        dprintf(STDERR_FILENO, "console: %s: %s\n", argv[1], strerror(errno));
        _exit(127);
    }
    struct sigaction action{};
    action.sa_handler = resize;
    sigaction(SIGWINCH, &action, nullptr);
    action.sa_handler = end;
    sigaction(SIGCHLD, &action, nullptr);
    fcntl(master, F_SETFL, O_NONBLOCK);
    char pending[4096];
    size_t held = 0;
    bool input = true;
    while (!ended) {
        if (resized) {
            resized = 0;
            if (ioctl(STDIN_FILENO, TIOCGWINSZ, &size) == 0) {
                ioctl(master, TIOCSWINSZ, &size);
            }
        }
        pollfd fds[2] = {
            {STDIN_FILENO, short(input && held < sizeof pending ? POLLIN : 0), 0},
            {master, short(POLLIN | (held > 0 ? POLLOUT : 0)), 0},
        };
        if (poll(fds, 2, -1) < 0) {
            if (errno == EINTR) {
                continue;
            }
            break;
        }
        if ((fds[0].revents & (POLLIN | POLLHUP | POLLERR)) != 0) {
            const ssize_t n = read(STDIN_FILENO, pending + held, sizeof pending - held);
            if (n > 0) {
                held += size_t(n);
            } else if (n == 0 || errno != EINTR) {
                input = false;
            }
        }
        if (held > 0 && (fds[1].revents & POLLOUT) != 0) {
            const ssize_t n = write(master, pending, held);
            if (n > 0) {
                memmove(pending, pending + n, held - size_t(n));
                held -= size_t(n);
            }
        }
        if ((fds[1].revents & (POLLIN | POLLHUP | POLLERR)) != 0 && !fromMaster(master)) {
            break;
        }
    }
    // The program ended: what it left on the terminal goes out first.
    while (fromMaster(master)) {
        pollfd more{master, POLLIN, 0};
        if (poll(&more, 1, 0) <= 0) {
            break;
        }
    }
    int status = 0;
    while (waitpid(child, &status, 0) < 0 && errno == EINTR) {
    }
    return WIFEXITED(status) ? WEXITSTATUS(status) : 128 + WTERMSIG(status);
}
