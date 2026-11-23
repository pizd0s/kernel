// console: runs a program on a new pseudoterminal joined to the console,
// as a terminal emulator does: what comes from the console goes to the
// terminal's master, what the master reads goes to the console, and the
// console's window, where it has one, is the terminal's, again as
// SIGWINCH says it changed. The program leads a session the terminal
// controls. The console is the descriptors console has, 0, 1
// and 2, a terminal of the host's or pipes, which console reads and
// writes as they are. console ends with the program, once it wrote out
// what the program left, its status the program's: an exit's, or 128 and
// the signal that ended it. rootfs runs the shell so.
//
// The kernel gives the console's foreground group, console's, what the
// host's signals of a terminal's keys are, SIGINT, SIGQUIT and SIGTSTP:
// console types the terminal's character of each, VINTR, VQUIT and VSUSP,
// where it has one, as the host's terminal would have.
//
// console registers /dev/console, the system's console, before the
// program starts: a second pseudoterminal, whose slave each open of
// /dev/console is another open of, handed on as it is, and what its
// master reads goes to descriptor 2.
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
#include <termios.h>
#include <sys/wait.h>
#include <unistd.h>

static volatile sig_atomic_t resized = 0;
static volatile sig_atomic_t ended = 0;
// SIGINT's, SIGQUIT's and SIGTSTP's, the keys the host's signals are.
static volatile sig_atomic_t keyed[3] = {};

static void resize(int) {
    resized = 1;
}

static void key(int sig) {
    keyed[sig == SIGINT ? 0 : sig == SIGQUIT ? 1 : 2] = 1;
}

// The characters of the keys the host's signals were, typed after what
// the console gave before them.
static void typeKeys(int master, char* pending, size_t& held) {
    termios t;
    if (tcgetattr(master, &t) != 0) {
        return;
    }
    const cc_t keys[] = {t.c_cc[VINTR], t.c_cc[VQUIT], t.c_cc[VSUSP]};
    for (int i = 0; i < 3; ++i) {
        if (keyed[i]) {
            keyed[i] = 0;
            if (keys[i] != _POSIX_VDISABLE && held < 4096) {
                pending[held++] = char(keys[i]);
            }
        }
    }
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

// Moves what a master has to the console's descriptor to: false once it
// has no more to give, its slave closed.
static bool fromMaster(int master, int to = STDOUT_FILENO) {
    char bytes[4096];
    const ssize_t n = read(master, bytes, sizeof bytes);
    if (n > 0) {
        return writeAll(to, bytes, size_t(n));
    }
    return n < 0 && (errno == EAGAIN || errno == EINTR);
}

// /dev/console's pseudoterminal: its master, the path of its slave, and
// the device's end at /dev/init/ctl, where its opens come; -1 where there
// is none.
static int systemMaster = -1;
static char systemSlave[64];
static Device systemConsole{-1, {}};

// Registers /dev/console, of a second pseudoterminal: its slave stays
// open, so that what is written to it waits for console to read.
static void registerConsole() {
    int slave;
    if (openpty(&systemMaster, &slave, systemSlave, nullptr, nullptr) != 0) {
        dprintf(STDERR_FILENO, "console: /dev/console: openpty: %s\n", strerror(errno));
        systemMaster = -1;
        return;
    }
    fcntl(systemMaster, F_SETFD, FD_CLOEXEC);
    fcntl(slave, F_SETFD, FD_CLOEXEC);
    fcntl(systemMaster, F_SETFL, O_NONBLOCK);
    const int registry = open("/dev/init/ctl", O_RDWR | O_CLOEXEC);
    const int e = registry < 0 ? errno : enroll(registry, "console", S_IFCHR | 0622, uint32_t(geteuid()), uint32_t(getegid()), systemConsole);
    if (registry >= 0) {
        close(registry);
    }
    if (e != 0) {
        dprintf(STDERR_FILENO, "console: /dev/console: %s\n", strerror(e));
        systemConsole.end = -1;
    }
}

// An open of /dev/console: another open of its slave, of the open's flags,
// which goes with an OK of no body, the open file as it is.
static void openConsole() {
    alignas(Header) unsigned char packet[1024];
    int fds[maxFds];
    size_t count = 0;
    const ssize_t got = receivePacket(systemConsole.end, packet, sizeof packet, fds, count, MSG_DONTWAIT);
    if (got < ssize_t(sizeof(Header)) || count == 0) {
        for (size_t k = 0; k < count; ++k) {
            close(fds[k]);
        }
        if (got == 0) {
            close(systemConsole.end);
            systemConsole.end = -1;
        }
        return;
    }
    Header h;
    memcpy(&h, packet, sizeof h);
    const int reply = fds[count - 1];
    const int file = h.op == opOpen ? open(systemSlave, int(h.arg & (O_ACCMODE | O_NONBLOCK | O_APPEND)) | O_NOCTTY | O_CLOEXEC) : -1;
    if (h.op != opOpen) {
        fail(reply, EINVAL);
    } else if (file < 0) {
        fail(reply, errno);
    } else {
        const Header ok{opOk, 0, 0, 0, 0, 0};
        const iovec parts[] = {{const_cast<Header*>(&ok), sizeof ok}};
        sendPacket(reply, parts, 1, &file, 1);
        close(file);
    }
    for (size_t k = 0; k < count; ++k) {
        close(fds[k]);
    }
}

// The console's input ended: a terminal in canonical mode gets VEOF twice
// after what it has, the end of a line it holds and then its end of file;
// one in raw mode has no end of file, and gets nothing.
static void inputEnded(int master, char* pending, size_t& held) {
    termios t;
    if (tcgetattr(master, &t) != 0 || (t.c_lflag & ICANON) == 0) {
        return;
    }
    for (int i = 0; i < 2 && held < 4096; ++i) {
        pending[held++] = char(t.c_cc[VEOF]);
    }
}

int consoleMain(int argc, char** argv) {
    if (argc < 3) {
        dprintf(STDERR_FILENO, "usage: console <path> <arg0> [arg...]\n");
        return 2;
    }
    winsize size{};
    const bool window = ioctl(STDIN_FILENO, TIOCGWINSZ, &size) == 0;
    int master;
    int slave;
    if (openpty(&master, &slave, nullptr, nullptr, window ? &size : nullptr) != 0) {
        fatal("console: openpty: %s", strerror(errno));
    }
    fcntl(master, F_SETFD, FD_CLOEXEC);
    registerConsole();
    const pid_t child = fork();
    if (child < 0) {
        fatal("console: fork: %s", strerror(errno));
    }
    if (child == 0) {
        if (setsid() < 0 || ioctl(slave, TIOCSCTTY, 0) != 0 || dup2(slave, STDIN_FILENO) < 0 || dup2(slave, STDOUT_FILENO) < 0 ||
            dup2(slave, STDERR_FILENO) < 0) {
            dprintf(STDERR_FILENO, "console: the terminal: %s\n", strerror(errno));
            _exit(127);
        }
        if (slave > STDERR_FILENO) {
            close(slave);
        }
        execv(argv[1], argv + 2);
        dprintf(STDERR_FILENO, "console: %s: %s\n", argv[1], strerror(errno));
        _exit(127);
    }
    close(slave);
    struct sigaction action{};
    action.sa_handler = resize;
    sigaction(SIGWINCH, &action, nullptr);
    action.sa_handler = end;
    sigaction(SIGCHLD, &action, nullptr);
    action.sa_handler = key;
    sigaction(SIGINT, &action, nullptr);
    sigaction(SIGQUIT, &action, nullptr);
    sigaction(SIGTSTP, &action, nullptr);
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
        typeKeys(master, pending, held);
        // Once the input ended, its descriptor is out of the poll, whose
        // POLLHUP for it would end every poll at once.
        pollfd fds[4] = {
            {input ? STDIN_FILENO : -1, short(held < sizeof pending ? POLLIN : 0), 0},
            {master, short(POLLIN | (held > 0 ? POLLOUT : 0)), 0},
            {systemMaster, POLLIN, 0},
            {systemConsole.end, POLLIN, 0},
        };
        if (poll(fds, 4, -1) < 0) {
            if (errno == EINTR) {
                continue;
            }
            break;
        }
        if (input && (fds[0].revents & (POLLIN | POLLHUP | POLLERR)) != 0) {
            const ssize_t n = read(STDIN_FILENO, pending + held, sizeof pending - held);
            if (n > 0) {
                held += size_t(n);
            } else if (n == 0 || errno != EINTR) {
                input = false;
                inputEnded(master, pending, held);
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
        if ((fds[2].revents & POLLIN) != 0) {
            fromMaster(systemMaster, STDERR_FILENO);
        }
        if ((fds[3].revents & (POLLIN | POLLHUP)) != 0) {
            openConsole();
        }
    }
    // The program ended: what it left on the terminals goes out first.
    while (fromMaster(master)) {
        pollfd more{master, POLLIN, 0};
        if (poll(&more, 1, 0) <= 0) {
            break;
        }
    }
    while (systemMaster >= 0 && fromMaster(systemMaster, STDERR_FILENO)) {
        pollfd more{systemMaster, POLLIN, 0};
        if (poll(&more, 1, 0) <= 0) {
            break;
        }
    }
    int status = 0;
    while (waitpid(child, &status, 0) < 0 && errno == EINTR) {
    }
    return WIFEXITED(status) ? WEXITSTATUS(status) : 128 + WTERMSIG(status);
}
