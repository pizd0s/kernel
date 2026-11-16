//@ compile-flags: -D_GNU_SOURCE
//@ stdout: "slave /dev/pts/ 1 1\nline 'abc\\n' echo 'abc\\r\\n'\nerase 'ac\\n' echo 'ab\\b \\bc\\r\\n'\nkill 'd\\n' echo 'abc\\b \\b\\b \\b\\b \\bd\\r\\n'\nword 'foo baz\\n' echo 'foo bar\\b \\b\\b \\b\\b \\bbaz\\r\\n'\neof 0 'xy'\ncontrol '\\x01\\n' echo '^A\\r\\n'\nliteral '\\x7f\\n' echo '^\\b^?\\r\\n'\ninq 4\nraw 'x\\ry' echo ''\nraw out 'a\\nb'\ntimeout 0\nwindow 24 80\nmaster settings 1 1\nowner 1 private 1\ninterrupted 1 foreground 1 echo '^C'\nhangup read 0 write EIO\n"

// The pseudoterminals: openpty's master and slave, the slave's line
// discipline as Linux's, its settings, which the master's calls get and
// set too, its window, its owner, its signals and its hangup. Each result
// is shown as its bytes.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <pty.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <termios.h>
#include <unistd.h>

static void quote(char* out, const char* bytes, ssize_t n) {
    size_t at = 0;
    for (ssize_t i = 0; i < n; ++i) {
        const unsigned char c = (unsigned char)bytes[i];
        if (c == '\n') {
            at += sprintf(out + at, "\\n");
        } else if (c == '\r') {
            at += sprintf(out + at, "\\r");
        } else if (c == '\b') {
            at += sprintf(out + at, "\\b");
        } else if (c < 0x20 || c >= 0x7f) {
            at += sprintf(out + at, "\\x%02x", c);
        } else {
            out[at++] = char(c);
        }
    }
    out[at] = 0;
}

// What there is to read, until nothing more comes for a while.
static ssize_t drain(int fd, char* out, size_t room) {
    size_t n = 0;
    pollfd p{fd, POLLIN, 0};
    while (n < room && poll(&p, 1, 300) == 1 && (p.revents & POLLIN) != 0) {
        const ssize_t got = read(fd, out + n, room - n);
        if (got <= 0) {
            break;
        }
        n += size_t(got);
    }
    return ssize_t(n);
}

// Types a line at the master: what the slave reads of it, and the echo.
static void typed(int master, int slave, const char* label, const char* text) {
    char got[256];
    char echoed[256];
    char a[1024];
    char b[1024];
    assert(write(master, text, strlen(text)) == ssize_t(strlen(text)));
    const ssize_t n = read(slave, got, sizeof got);
    quote(a, got, n);
    quote(b, echoed, drain(master, echoed, sizeof echoed));
    printf("%s '%s' echo '%s'\n", label, a, b);
}

static volatile sig_atomic_t interrupted;

static void interrupt(int) {
    interrupted = 1;
}

int main() {
    int master;
    int slave;
    char name[64];
    assert(openpty(&master, &slave, name, nullptr, nullptr) == 0);
    printf("slave %.9s %d %d\n", name, isatty(master), isatty(slave));
    char got[256];
    char a[1024];
    char b[1024];
    typed(master, slave, "line", "abc\n");
    typed(master, slave, "erase", "ab\177c\n");
    typed(master, slave, "kill", "abc\025d\n");
    typed(master, slave, "word", "foo bar\027baz\n");
    assert(write(master, "\x04", 1) == 1);
    const ssize_t eof = read(slave, got, sizeof got);
    assert(write(master, "xy\x04", 3) == 3);
    const ssize_t xy = read(slave, got, sizeof got);
    quote(a, got, xy);
    printf("eof %zd '%s'\n", eof, a);
    drain(master, got, sizeof got);
    typed(master, slave, "control", "\x01\n");
    typed(master, slave, "literal", "\x16\x7f\n");

    assert(write(master, "abc\n", 4) == 4);
    pollfd ready{slave, POLLIN, 0};
    assert(poll(&ready, 1, 1000) == 1);
    int inq = -1;
    assert(ioctl(slave, FIONREAD, &inq) == 0);
    printf("inq %d\n", inq);
    read(slave, got, sizeof got);
    drain(master, got, sizeof got);

    termios saved;
    termios raw;
    assert(tcgetattr(slave, &saved) == 0);
    raw = saved;
    cfmakeraw(&raw);
    assert(tcsetattr(slave, TCSANOW, &raw) == 0);
    assert(write(master, "x\ry", 3) == 3);
    quote(a, got, drain(slave, got, sizeof got));
    quote(b, got + 128, drain(master, got + 128, 128));
    printf("raw '%s' echo '%s'\n", a, b);
    assert(write(slave, "a\nb", 3) == 3);
    quote(a, got, drain(master, got, sizeof got));
    printf("raw out '%s'\n", a);
    raw.c_cc[VMIN] = 0;
    raw.c_cc[VTIME] = 1;
    assert(tcsetattr(slave, TCSANOW, &raw) == 0);
    printf("timeout %zd\n", read(slave, got, sizeof got));
    assert(tcsetattr(slave, TCSANOW, &saved) == 0);

    winsize size{24, 80, 0, 0};
    winsize back{};
    assert(ioctl(master, TIOCSWINSZ, &size) == 0 && ioctl(slave, TIOCGWINSZ, &back) == 0);
    printf("window %d %d\n", back.ws_row, back.ws_col);

    // The master's settings are the slave's, as Linux's are.
    termios fromMaster;
    termios fromSlave;
    assert(tcgetattr(master, &fromMaster) == 0 && tcgetattr(slave, &fromSlave) == 0);
    const int same = fromMaster.c_iflag == fromSlave.c_iflag && fromMaster.c_lflag == fromSlave.c_lflag;
    cfmakeraw(&fromMaster);
    assert(tcsetattr(master, TCSANOW, &fromMaster) == 0 && tcgetattr(slave, &fromSlave) == 0);
    printf("master settings %d %d\n", same, (fromSlave.c_lflag & ICANON) == 0);
    assert(tcsetattr(master, TCSANOW, &saved) == 0);

    // The slave is its opener's, as devpts makes it: others but its group
    // may not even write to it.
    struct stat device;
    assert(fstat(slave, &device) == 0);
    const int owner = device.st_uid == getuid();
    const int closed = (device.st_mode & S_IRWXU) == (S_IRUSR | S_IWUSR) && (device.st_mode & (S_IRWXO | S_IRGRP | S_IXGRP)) == 0;
    printf("owner %d private %d\n", owner, closed);

    // A session the slave controls: ^C at the master interrupts its
    // foreground group.
    fflush(stdout);
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        struct sigaction action{};
        action.sa_handler = interrupt;
        assert(sigaction(SIGINT, &action, nullptr) == 0);
        assert(setsid() == getpid());
        assert(ioctl(slave, TIOCSCTTY, 0) == 0);
        const int foreground = tcgetpgrp(slave) == getpgrp();
        assert(write(master, "\x03", 1) == 1);
        for (int i = 0; i < 300 && !interrupted; ++i) {
            usleep(10000);
        }
        quote(a, got, drain(master, got, sizeof got));
        printf("interrupted %d foreground %d echo '%s'\n", int(interrupted), foreground, a);
        fflush(stdout);
        _exit(0);
    }
    int status;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);

    // The master goes: the slave hangs up.
    signal(SIGHUP, SIG_IGN);
    assert(close(master) == 0);
    const ssize_t after = read(slave, got, sizeof got);
    const ssize_t wrote = write(slave, "x", 1);
    printf("hangup read %zd write %s\n", after, wrote < 0 && errno == EIO ? "EIO" : "?");
    return 0;
}
