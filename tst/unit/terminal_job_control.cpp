//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "no controlling terminal: /dev/tty ENXIO\ncontrolling terminal: /dev/tty opened, wrote 1\nread: stopped by SIGTTIN\nread, SIGTTIN ignored: EIO\nread, SIGTTIN blocked: EIO\nwrite: 1\nwrite, TOSTOP: stopped by SIGTTOU\nwrite, TOSTOP, SIGTTOU ignored: 1\ntcsetattr: stopped by SIGTTOU\ntcsetpgrp, SIGTTOU ignored: 0\norphaned read: EIO\norphaned write, TOSTOP: EIO\norphaned, stopped: SIGHUP 1 SIGCONT 1\nleader gone, foreground: SIGHUP 1 SIGCONT 0, /dev/tty ENXIO\nnotty, foreground: SIGHUP 1 SIGCONT 1, /dev/tty ENXIO\nnotty: 0, /dev/tty ENXIO\nhangup: read EIO, SIGHUP 1 SIGCONT 1, /dev/tty ENXIO\n"

// Job control on a session's controlling terminal, a pseudoterminal, as
// Linux's: what a group out of the foreground gets of a read, a write and
// a change of the terminal; what a group gets that no one in its session
// could continue, an orphaned one; what the foreground group hears as the
// session's leader goes or gives the terminal up; and what the leader
// hears as the terminal hangs up, its master gone. Each case is a session
// of its own, forkpty's; its processes tell what came of them on a pipe,
// in order.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <pty.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/wait.h>
#include <termios.h>
#include <unistd.h>

// Where a case's processes tell what came of them.
static int report = -1;

static void tell(const char* line) {
    const size_t n = strlen(line);
    assert(write(report, line, n) == ssize_t(n));
}

static const char* errorName(int e) {
    switch (e) {
        case EIO:
            return "EIO";
        case ENXIO:
            return "ENXIO";
        case EINTR:
            return "EINTR";
        case EPERM:
            return "EPERM";
        default:
            return "another error";
    }
}

static const char* signalName(int sig) {
    switch (sig) {
        case SIGTTIN:
            return "SIGTTIN";
        case SIGTTOU:
            return "SIGTTOU";
        case SIGTSTP:
            return "SIGTSTP";
        case SIGSTOP:
            return "SIGSTOP";
        default:
            return "another signal";
    }
}

// What a call of the case named what gave: its count, or its error.
static const char* what;

static void said(ssize_t n) {
    char line[128];
    if (n >= 0) {
        snprintf(line, sizeof line, "%s: %zd\n", what, n);
    } else {
        snprintf(line, sizeof line, "%s: %s\n", what, errorName(errno));
    }
    tell(line);
}

// What /dev/tty's open finds: the controlling terminal, or the error.
static const char* openTerminal() {
    const int fd = open("/dev/tty", O_RDWR | O_NOCTTY);
    if (fd < 0) {
        return errorName(errno);
    }
    close(fd);
    return "opened";
}

static void readTerminal() {
    char c;
    said(read(STDIN_FILENO, &c, 1));
}

static void readIgnoring() {
    signal(SIGTTIN, SIG_IGN);
    readTerminal();
}

static void readBlocking() {
    sigset_t set;
    sigemptyset(&set);
    sigaddset(&set, SIGTTIN);
    assert(sigprocmask(SIG_BLOCK, &set, nullptr) == 0);
    readTerminal();
}

static void writeTerminal() {
    said(write(STDOUT_FILENO, "w", 1));
}

static void writeIgnoring() {
    signal(SIGTTOU, SIG_IGN);
    writeTerminal();
}

static void setAttributes() {
    termios settings;
    assert(tcgetattr(STDIN_FILENO, &settings) == 0);
    said(tcsetattr(STDIN_FILENO, TCSANOW, &settings));
}

static void takeForeground() {
    signal(SIGTTOU, SIG_IGN);
    said(tcsetpgrp(STDIN_FILENO, getpgrp()));
}

static void setTostop(bool on) {
    termios settings;
    assert(tcgetattr(STDIN_FILENO, &settings) == 0);
    if (on) {
        settings.c_lflag |= TOSTOP;
    } else {
        settings.c_lflag &= ~TOSTOP;
    }
    assert(tcsetattr(STDIN_FILENO, TCSANOW, &settings) == 0);
}

// Runs call in a child of a group of its own, out of the foreground: the
// signal that stopped the child is told, or the child tells what its call
// gave.
static void outOfForeground(const char* label, void (*call)()) {
    what = label;
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        assert(setpgid(0, 0) == 0);
        call();
        _exit(0);
    }
    setpgid(child, child);
    int status;
    assert(waitpid(child, &status, WUNTRACED) == child);
    if (WIFSTOPPED(status)) {
        char line[128];
        snprintf(line, sizeof line, "%s: stopped by %s\n", label, signalName(WSTOPSIG(status)));
        tell(line);
        assert(kill(child, SIGKILL) == 0 && waitpid(child, &status, 0) == child);
    }
}

static void backgroundCalls() {
    const int fd = open("/dev/tty", O_RDWR | O_NOCTTY);
    assert(fd >= 0);
    char line[128];
    snprintf(line, sizeof line, "controlling terminal: /dev/tty opened, wrote %zd\n", write(fd, "t", 1));
    tell(line);
    close(fd);
    outOfForeground("read", readTerminal);
    outOfForeground("read, SIGTTIN ignored", readIgnoring);
    outOfForeground("read, SIGTTIN blocked", readBlocking);
    outOfForeground("write", writeTerminal);
    setTostop(true);
    outOfForeground("write, TOSTOP", writeTerminal);
    outOfForeground("write, TOSTOP, SIGTTOU ignored", writeIgnoring);
    setTostop(false);
    outOfForeground("tcsetattr", setAttributes);
    outOfForeground("tcsetpgrp, SIGTTOU ignored", takeForeground);
}

// A child's child, in a group of its own, whose parent went: its group
// is orphaned once it is the first process's. The leader waits for it
// to end: it needs the terminal to be the session's.
static void orphanedCalls() {
    setTostop(true);
    int done[2];
    assert(pipe(done) == 0);
    const pid_t middle = fork();
    assert(middle >= 0);
    if (middle == 0) {
        const pid_t parent = getpid();
        const pid_t child = fork();
        assert(child >= 0);
        if (child == 0) {
            assert(setpgid(0, 0) == 0);
            while (getppid() == parent) {
                usleep(1000);
            }
            what = "orphaned read";
            readTerminal();
            what = "orphaned write, TOSTOP";
            writeTerminal();
            _exit(0);
        }
        _exit(0);
    }
    close(done[1]);
    int status;
    assert(waitpid(middle, &status, 0) == middle);
    char c;
    assert(read(done[0], &c, 1) == 0);
}

static volatile sig_atomic_t hups;
static volatile sig_atomic_t conts;

static void heard(int sig) {
    if (sig == SIGHUP) {
        ++hups;
    } else {
        ++conts;
    }
}

static void listenForHangup() {
    struct sigaction action{};
    action.sa_handler = heard;
    sigemptyset(&action.sa_mask);
    assert(sigaction(SIGHUP, &action, nullptr) == 0 && sigaction(SIGCONT, &action, nullptr) == 0);
}

// A stopped child's child, in a group of its own, whose parent goes: the
// group, orphaned with a stopped process, hears SIGHUP and SIGCONT.
static void orphanedStopped() {
    int done[2];
    assert(pipe(done) == 0);
    const pid_t middle = fork();
    assert(middle >= 0);
    if (middle == 0) {
        const pid_t child = fork();
        assert(child >= 0);
        if (child == 0) {
            assert(setpgid(0, 0) == 0);
            listenForHangup();
            raise(SIGSTOP);
            for (int i = 0; i < 500 && (hups == 0 || conts == 0); ++i) {
                usleep(10000);
            }
            char line[128];
            snprintf(line, sizeof line, "orphaned, stopped: SIGHUP %d SIGCONT %d\n", int(hups), int(conts));
            tell(line);
            _exit(0);
        }
        setpgid(child, child);
        int status;
        assert(waitpid(child, &status, WUNTRACED) == child && WIFSTOPPED(status));
        _exit(0);
    }
    close(done[1]);
    int status;
    assert(waitpid(middle, &status, 0) == middle);
    char c;
    assert(read(done[0], &c, 1) == 0);
}

// A child in a group of its own, the foreground one, which tells what it
// heard of the leader's giving the terminal up and what /dev/tty opens
// then; ready is written once it listens. A SIGCONT it hears comes right
// after the SIGHUP: it waits for one it should hear, else a while.
static pid_t foregroundChild(const char* label, int ready, bool continued) {
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        assert(setpgid(0, 0) == 0);
        listenForHangup();
        sigset_t hup;
        sigset_t none;
        sigemptyset(&hup);
        sigemptyset(&none);
        sigaddset(&hup, SIGHUP);
        assert(sigprocmask(SIG_BLOCK, &hup, nullptr) == 0);
        assert(write(ready, "r", 1) == 1);
        while (hups == 0) {
            sigsuspend(&none);
        }
        for (int i = 0; i < (continued ? 500 : 10) && conts == 0; ++i) {
            usleep(10000);
        }
        char line[128];
        snprintf(line, sizeof line, "%s: SIGHUP %d SIGCONT %d, /dev/tty %s\n", label, int(hups), int(conts), openTerminal());
        tell(line);
        _exit(0);
    }
    setpgid(child, child);
    assert(tcsetpgrp(STDIN_FILENO, child) == 0);
    return child;
}

// The leader goes with its child's group in the foreground.
static void leaderGoes() {
    int ready[2];
    assert(pipe(ready) == 0);
    foregroundChild("leader gone, foreground", ready[1], false);
    char c;
    assert(read(ready[0], &c, 1) == 1);
}

// The leader gives the terminal up, TIOCNOTTY, with its child's group in
// the foreground.
static void leaderLetsGo() {
    int ready[2];
    assert(pipe(ready) == 0);
    const pid_t child = foregroundChild("notty, foreground", ready[1], true);
    char c;
    assert(read(ready[0], &c, 1) == 1);
    const int r = ioctl(STDIN_FILENO, TIOCNOTTY);
    const char* opened = openTerminal();
    int status;
    assert(waitpid(child, &status, 0) == child);
    char line[128];
    snprintf(line, sizeof line, "notty: %d, /dev/tty %s\n", r, opened);
    tell(line);
}

static void noTerminal() {
    char line[128];
    snprintf(line, sizeof line, "no controlling terminal: /dev/tty %s\n", openTerminal());
    tell(line);
}

// Runs body in a process of its own, the leader of a session, with a new
// terminal or with none, and prints what the case's processes tell.
static void runCase(void (*body)(), bool terminal) {
    int told[2];
    assert(pipe(told) == 0);
    fflush(stdout);
    int master = -1;
    const pid_t leader = terminal ? forkpty(&master, nullptr, nullptr, nullptr) : fork();
    assert(leader >= 0);
    if (leader == 0) {
        close(told[0]);
        report = told[1];
        if (!terminal) {
            assert(setsid() == getpid());
        }
        body();
        _exit(0);
    }
    close(told[1]);
    char text[2048];
    size_t n = 0;
    ssize_t got;
    while ((got = read(told[0], text + n, sizeof text - 1 - n)) > 0) {
        n += size_t(got);
    }
    text[n] = 0;
    fputs(text, stdout);
    close(told[0]);
    int status;
    assert(waitpid(leader, &status, 0) == leader && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    if (master >= 0) {
        close(master);
    }
}

// The master goes: the slave hangs up, the read the session's leader
// waits on is EIO, the leader hears SIGHUP and SIGCONT, which it blocks
// while it reads, so that they do not end the read first, and the
// terminal controls the session no more. The leader's read waits before
// the master goes as the test waits for it a while: nothing tells it.
static void hangUp() {
    int told[2];
    assert(pipe(told) == 0);
    fflush(stdout);
    int master;
    const pid_t leader = forkpty(&master, nullptr, nullptr, nullptr);
    assert(leader >= 0);
    if (leader == 0) {
        close(told[0]);
        report = told[1];
        listenForHangup();
        sigset_t both;
        sigemptyset(&both);
        sigaddset(&both, SIGHUP);
        sigaddset(&both, SIGCONT);
        assert(sigprocmask(SIG_BLOCK, &both, nullptr) == 0);
        assert(write(STDOUT_FILENO, "r", 1) == 1);
        char c;
        const ssize_t n = read(STDIN_FILENO, &c, 1);
        char got[32];
        snprintf(got, sizeof got, "%s", n < 0 ? errorName(errno) : n == 0 ? "0" : "a byte");
        assert(sigprocmask(SIG_UNBLOCK, &both, nullptr) == 0);
        for (int i = 0; i < 500 && (hups == 0 || conts == 0); ++i) {
            usleep(10000);
        }
        char line[128];
        snprintf(line, sizeof line, "hangup: read %s, SIGHUP %d SIGCONT %d, /dev/tty %s\n", got, int(hups), int(conts), openTerminal());
        tell(line);
        _exit(0);
    }
    close(told[1]);
    char c;
    assert(read(master, &c, 1) == 1 && c == 'r');
    // The read the leader makes next waits by then: a read that comes
    // after the hangup reads nothing, as Linux's does.
    usleep(300000);
    close(master);
    char text[256];
    size_t n = 0;
    ssize_t got;
    while ((got = read(told[0], text + n, sizeof text - 1 - n)) > 0) {
        n += size_t(got);
    }
    text[n] = 0;
    fputs(text, stdout);
    close(told[0]);
    int status;
    assert(waitpid(leader, &status, 0) == leader && WIFEXITED(status) && WEXITSTATUS(status) == 0);
}

int main() {
    alarm(60);
    runCase(noTerminal, false);
    runCase(backgroundCalls, true);
    runCase(orphanedCalls, true);
    runCase(orphanedStopped, true);
    runCase(leaderGoes, true);
    runCase(leaderLetsGo, true);
    hangUp();
    return 0;
}
