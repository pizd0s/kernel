//@ host: false
//@ compile-flags: -D_GNU_SOURCE
//@ max-memory: 4194304
//@ stdout: "no terminal: NULL ENXIO\nname: /dev/pts/ 1 1\npasswords: 's3cr3t' '' 127 x '' settings kept 1\ntranscript 'first: \\r\\nempty: \\r\\nlong: \\r\\neof: \\r\\n'\ninterrupted: NULL EINTR alarm 1 settings kept 1\ntranscript 'interrupt: \\r\\n'\nno memory: NULL ENOMEM\ntranscript ''\n"

// getpass, the kernel's, on a session's controlling terminal, a
// pseudoterminal: it reads a line of /dev/tty with no echo, writes the
// prompt and a newline there, and leaves the terminal's settings as they
// were; a password is at most 127 bytes, as musl's; an end of file is an
// empty one. A signal interrupts it, EINTR; a session with no terminal is
// ENXIO, a process with no room for the password ENOMEM. The test answers
// the prompts at the master and prints what the terminal showed; the
// slave's name is its path in /dev, which opens it again.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <pty.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <termios.h>
#include <unistd.h>

// Where a case's process tells what came of it.
static int report = -1;

static void tell(const char* line) {
    const size_t n = strlen(line);
    assert(write(report, line, n) == ssize_t(n));
}

static const char* errorName(int e) {
    switch (e) {
        case ENXIO:
            return "ENXIO";
        case EINTR:
            return "EINTR";
        case ENOMEM:
            return "ENOMEM";
        case EIO:
            return "EIO";
        default:
            return "another error";
    }
}

static void quote(char* out, const char* bytes, size_t n) {
    size_t at = 0;
    for (size_t i = 0; i < n; ++i) {
        const unsigned char c = (unsigned char)bytes[i];
        if (c == '\n') {
            at += sprintf(out + at, "\\n");
        } else if (c == '\r') {
            at += sprintf(out + at, "\\r");
        } else if (c < 0x20 || c >= 0x7f) {
            at += sprintf(out + at, "\\x%02x", c);
        } else {
            out[at++] = char(c);
        }
    }
    out[at] = 0;
}

static bool sameSettings(const termios& a, const termios& b) {
    return a.c_iflag == b.c_iflag && a.c_oflag == b.c_oflag && a.c_lflag == b.c_lflag;
}

static void noTerminal() {
    errno = 0;
    const char* password = getpass("unreachable: ");
    char line[128];
    snprintf(line, sizeof line, "no terminal: %s %s\n", password ? "a password" : "NULL", errorName(errno));
    tell(line);
}

static void passwords() {
    const char* name = ttyname(STDIN_FILENO);
    char copy[64];
    const int named = name && strncmp(name, "/dev/pts/", 9) == 0;
    int same = name && ttyname_r(STDIN_FILENO, copy, sizeof copy) == 0 && strcmp(copy, name) == 0;
    termios before;
    termios after;
    assert(tcgetattr(STDIN_FILENO, &before) == 0);
    const int reopened = same ? open(copy, O_RDWR | O_NOCTTY) : -1;
    same = reopened >= 0 && tcgetattr(reopened, &after) == 0 && sameSettings(before, after);
    if (reopened >= 0) {
        close(reopened);
    }
    char line[512];
    snprintf(line, sizeof line, "name: /dev/pts/ %d %d\n", named, same);
    tell(line);

    char first[160];
    char empty[160];
    char eof[160];
    const char* password = getpass("first: ");
    snprintf(first, sizeof first, "%s", password ? password : "(null)");
    password = getpass("empty: ");
    snprintf(empty, sizeof empty, "%s", password ? password : "(null)");
    password = getpass("long: ");
    size_t xs = 0;
    while (password && password[xs] == 'x') {
        ++xs;
    }
    const bool allX = password && password[xs] == 0;
    password = getpass("eof: ");
    snprintf(eof, sizeof eof, "%s", password ? password : "(null)");
    assert(tcgetattr(STDIN_FILENO, &after) == 0);
    snprintf(line, sizeof line, "passwords: '%s' '%s' %zu %s '%s' settings kept %d\n", first, empty, xs, allX ? "x" : "?", eof, int(sameSettings(before, after)));
    tell(line);
}

static volatile sig_atomic_t alarmed;

static void onAlarm(int) {
    alarmed = 1;
}

static void interrupted() {
    struct sigaction action{};
    action.sa_handler = onAlarm;
    sigemptyset(&action.sa_mask);
    assert(sigaction(SIGALRM, &action, nullptr) == 0);
    termios before;
    termios after;
    assert(tcgetattr(STDIN_FILENO, &before) == 0);
    errno = 0;
    const char* password = getpass("interrupt: ");
    const int e = errno;
    assert(tcgetattr(STDIN_FILENO, &after) == 0);
    char line[128];
    snprintf(line, sizeof line, "interrupted: %s %s alarm %d settings kept %d\n", password ? "a password" : "NULL", errorName(e), int(alarmed), int(sameSettings(before, after)));
    tell(line);
}

// What malloc gives, kept so that no allocation goes unused.
static void* volatile kept;

static void noMemory() {
    while ((kept = malloc(65536)) != nullptr) {
    }
    while ((kept = malloc(129)) != nullptr) {
    }
    errno = 0;
    const char* password = getpass("oom: ");
    tell(!password && errno == ENOMEM ? "no memory: NULL ENOMEM\n" : "no memory: ?\n");
}

// A prompt the test waits for at the master, and what it types then, or
// with none SIGALRM to the leader: getpass, one call, is reading then, or
// about to with the signal there.
struct Answer {
    const char* prompt;
    const char* text;
};

static bool endsWith(const char* text, size_t n, const char* suffix) {
    const size_t length = strlen(suffix);
    return n >= length && memcmp(text + n - length, suffix, length) == 0;
}

// Runs body in a process of its own, the leader of a session, with a new
// terminal, whose prompts the answers answer, or with none; prints what
// the process tells, then what the terminal showed.
static void runCase(void (*body)(), const Answer* answers, size_t count, bool terminal) {
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
    char text[1024];
    size_t n = 0;
    char shown[1024];
    size_t at = 0;
    size_t answered = 0;
    bool done = false;
    while (!done) {
        pollfd fds[2] = {{told[0], POLLIN, 0}, {master, POLLIN, 0}};
        assert(poll(fds, master >= 0 ? 2 : 1, 10000) > 0);
        if (fds[0].revents != 0) {
            const ssize_t got = read(told[0], text + n, sizeof text - 1 - n);
            done = got <= 0;
            n += done ? 0 : size_t(got);
        }
        if (master >= 0 && (fds[1].revents & POLLIN) != 0) {
            const ssize_t got = read(master, shown + at, sizeof shown - at);
            at += got > 0 ? size_t(got) : 0;
            if (answered < count && endsWith(shown, at, answers[answered].prompt)) {
                const char* typed = answers[answered++].text;
                if (typed) {
                    assert(write(master, typed, strlen(typed)) == ssize_t(strlen(typed)));
                } else {
                    assert(kill(leader, SIGALRM) == 0);
                }
            }
        }
    }
    text[n] = 0;
    fputs(text, stdout);
    close(told[0]);
    int status;
    assert(waitpid(leader, &status, 0) == leader && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    if (master >= 0) {
        // What the terminal showed after: the slave is closed, the master
        // reads it to the end.
        pollfd ready{master, POLLIN, 0};
        while (at < sizeof shown && poll(&ready, 1, 300) == 1 && (ready.revents & POLLIN) != 0) {
            const ssize_t got = read(master, shown + at, sizeof shown - at);
            if (got <= 0) {
                break;
            }
            at += size_t(got);
        }
        char quoted[4096];
        quote(quoted, shown, at);
        printf("transcript '%s'\n", quoted);
        close(master);
    }
}

int main() {
    alarm(60);
    char longLine[152];
    memset(longLine, 'x', 150);
    longLine[150] = '\n';
    longLine[151] = 0;
    const Answer answers[] = {{"first: ", "s3cr3t\n"}, {"empty: ", "\n"}, {"long: ", longLine}, {"eof: ", "\x04"}};
    const Answer interrupt[] = {{"interrupt: ", nullptr}};
    runCase(noTerminal, nullptr, 0, false);
    runCase(passwords, answers, 4, true);
    runCase(interrupted, interrupt, 1, true);
    runCase(noMemory, nullptr, 0, true);
    return 0;
}
