// pty: the system's pseudoterminals, a service (lib/protocol/service.h)
// that registers /dev/pty at /dev/init. Each open of /dev/pty is a new
// terminal: the open is its master, and its slave is /dev/pts/N, a device
// the service registers as the terminal comes and lets go when the master
// goes. The open files of both ends are a terminal's (openTerminal) and
// tell their readiness. The service keeps all of a terminal: its settings
// and the master's, its window's size, the session it controls and its
// foreground group, and its discipline, Linux's N_TTY: what the master
// writes goes through the slave's input processing, its line editing,
// echo and special characters, to what the slave reads; what the slave
// writes, and the echo, go through its output processing to what the
// master reads. A read that finds nothing waits, unless it must not
// (ioNonblock), until there is something or a CANCEL; so does a write
// that finds no room, and a drain. When the master goes, the terminal
// hangs up: the slave reads nothing more and its writes are EIO, and the
// session it controls hears SIGHUP; with no slave open, the master's
// reads are EIO once it read what there was.
//
//     pty [-r fd]

#include <bin/tools/tools.h>
#include <lib/protocol/service.h>

#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/ioctl.h>
#include <sys/param.h>
#include <termios.h>
#include <time.h>
#include <unistd.h>

namespace {
    // What the master reads: the slave's output and the echo.
    struct Output {
        unsigned char bytes[16384];
        size_t head;
        size_t count;

        size_t room() const;
        void push(unsigned char c);
        unsigned char take();
        void clear();
    };

    // What the slave reads, each byte with its mark: a line's end, a byte
    // a read takes and stops after, or an end of file, which ends a line
    // and is no byte of it.
    struct Input {
        unsigned char bytes[4096];
        unsigned char marks[4096];
        size_t head;
        size_t count;

        size_t room() const;
        void push(unsigned char c, unsigned char mark);
        unsigned char byteAt(size_t i) const;
        unsigned char markAt(size_t i) const;
        void drop();
        void clear();
    };

    struct Terminal;

    // An open file of a terminal, its master's or its slave's: its
    // endpoint, which hears its readiness, and what it heard last: the
    // events, the foreground group and the flags.
    struct Open {
        Terminal* terminal;
        bool master;
        bool reads;
        bool writes;
        int fd;
        uint32_t told;
        uint32_t toldForeground;
        uint32_t toldFlags;
    };

    enum class Wait {
        Read,
        Write,
        Drain,
    };

    // A request that waits: its endpoint, kept until it is answered, which
    // hears CANCEL; the open file it came on; a read's or a write's memfd,
    // length, place in the memfd and what of it is done, and a read's
    // start; a drain's settings, which a TCSETATTR sets once the output
    // went, and when, TCSADRAIN or TCSAFLUSH.
    struct Waiting {
        Endpoint* at;
        Open* open;
        Wait kind;
        int window;
        uint32_t length;
        uint32_t woff;
        uint32_t done;
        int64_t start;
        bool settles;
        uint32_t when;
        termios settings;
    };

    // A pseudoterminal: its number, its slave's device and the endpoint
    // of its opens; its settings and its master's, its window, the session
    // it controls and its foreground group; the line being edited, from
    // the column it started at; what the slave reads, its lines, and when
    // its last byte came; what the master reads, and the column the output
    // is at; whether the next byte is literal, the output stopped, the
    // master gone; its open files of each end.
    struct Terminal {
        int number;
        Device slave;
        Endpoint* channel;
        termios settings;
        termios masterSettings;
        winsize size;
        pid_t session;
        pid_t foreground;
        unsigned char line[4096];
        size_t lineLength;
        size_t lineColumn;
        Input input;
        size_t lines;
        int64_t lastInput;
        Output output;
        size_t column;
        bool literal;
        bool stopped;
        bool hungUp;
        int masters;
        int slaves;
    };
}

// The marks of Input.
static constexpr unsigned char plain = 0;
static constexpr unsigned char lineEnd = 1;
static constexpr unsigned char endOfFile = 2;

// A line is at most this, as Linux's: what the slave reads and the line
// being edited share its room.
static constexpr size_t inputLimit = 4095;

static Terminal* terminals[64];
static Open opens[256];
static Waiting waits[64];
static int epoll = -1;
static int registry = -1;
static Device device;
static int ptyTag;

size_t Output::room() const {
    return sizeof bytes - count;
}

void Output::push(unsigned char c) {
    bytes[(head + count) % sizeof bytes] = c;
    ++count;
}

unsigned char Output::take() {
    const unsigned char c = bytes[head];
    head = (head + 1) % sizeof bytes;
    --count;
    return c;
}

void Output::clear() {
    head = 0;
    count = 0;
}

size_t Input::room() const {
    return sizeof bytes - count;
}

void Input::push(unsigned char c, unsigned char mark) {
    const size_t at = (head + count) % sizeof bytes;
    bytes[at] = c;
    marks[at] = mark;
    ++count;
}

unsigned char Input::byteAt(size_t i) const {
    return bytes[(head + i) % sizeof bytes];
}

unsigned char Input::markAt(size_t i) const {
    return marks[(head + i) % sizeof bytes];
}

void Input::drop() {
    head = (head + 1) % sizeof bytes;
    --count;
}

void Input::clear() {
    head = 0;
    count = 0;
}

static int64_t now() {
    timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return int64_t(ts.tv_sec) * 1000 + ts.tv_nsec / 1000000;
}

static void signalGroup(pid_t group, int sig) {
    if (group > 0) {
        kill(-group, sig);
    }
}

// The settings of a new terminal, Linux's: its slave's, and its master's,
// which process nothing.
static void startSettings(Terminal& t) {
    memset(&t.settings, 0, sizeof t.settings);
    t.settings.c_iflag = ICRNL | IXON;
    t.settings.c_oflag = OPOST | ONLCR;
    t.settings.c_cflag = CS8 | CREAD;
    t.settings.c_lflag = ISIG | ICANON | ECHO | ECHOE | ECHOK | ECHOCTL | ECHOKE | IEXTEN;
    cc_t* cc = t.settings.c_cc;
    cc[VINTR] = 003;
    cc[VQUIT] = 034;
    cc[VERASE] = 0177;
    cc[VKILL] = 025;
    cc[VEOF] = 004;
    cc[VTIME] = 0;
    cc[VMIN] = 1;
    cc[VSTART] = 021;
    cc[VSTOP] = 023;
    cc[VSUSP] = 032;
    cc[VREPRINT] = 022;
    cc[VDISCARD] = 017;
    cc[VWERASE] = 027;
    cc[VLNEXT] = 026;
    cfsetispeed(&t.settings, B38400);
    cfsetospeed(&t.settings, B38400);
    memset(&t.masterSettings, 0, sizeof t.masterSettings);
    t.masterSettings.c_cflag = CS8 | CREAD;
    cfsetispeed(&t.masterSettings, B38400);
    cfsetospeed(&t.masterSettings, B38400);
}

// Whether a byte is the special character of an index, one not disabled.
static bool special(const Terminal& t, unsigned char c, int index) {
    return t.settings.c_cc[index] != _POSIX_VDISABLE && c == t.settings.c_cc[index];
}

static bool continuation(const Terminal& t, unsigned char c) {
    return (t.settings.c_iflag & IUTF8) != 0 && (c & 0xc0) == 0x80;
}

// Puts a byte through the output processing to what the master reads,
// the column following it: false when there is no room for it.
static bool out(Terminal& t, unsigned char c) {
    const tcflag_t oflag = t.settings.c_oflag;
    if ((oflag & OPOST) == 0) {
        if (t.output.room() == 0) {
            return false;
        }
        t.output.push(c);
        return true;
    }
    switch (c) {
        case '\n':
            if ((oflag & ONLRET) != 0) {
                t.column = 0;
            }
            if ((oflag & ONLCR) != 0) {
                if (t.output.room() < 2) {
                    return false;
                }
                t.output.push('\r');
                t.output.push('\n');
                t.column = 0;
                return true;
            }
            break;
        case '\r':
            if ((oflag & ONOCR) != 0 && t.column == 0) {
                return true;
            }
            if ((oflag & OCRNL) != 0) {
                c = '\n';
                if ((oflag & ONLRET) != 0) {
                    t.column = 0;
                }
                break;
            }
            t.column = 0;
            break;
        case '\t': {
            const size_t spaces = 8 - (t.column & 7);
            if ((oflag & TABDLY) == XTABS) {
                if (t.output.room() < spaces) {
                    return false;
                }
                for (size_t i = 0; i < spaces; ++i) {
                    t.output.push(' ');
                }
                t.column += spaces;
                return true;
            }
            t.column += spaces;
            break;
        }
        case '\b':
            if (t.column > 0) {
                --t.column;
            }
            break;
        default:
            if (!iscntrl(c)) {
                if ((oflag & OLCUC) != 0) {
                    c = uint8_t(toupper(c));
                }
                if (!continuation(t, c)) {
                    ++t.column;
                }
            }
            break;
    }
    if (t.output.room() == 0) {
        return false;
    }
    t.output.push(c);
    return true;
}

// Echoes a byte of the input: a control character as ^X with ECHOCTL,
// but a tab and a newline.
static void echo(Terminal& t, unsigned char c) {
    if ((t.settings.c_lflag & ECHOCTL) != 0 && iscntrl(c) && c != '\t' && c != '\n') {
        out(t, '^');
        out(t, c ^ 0100);
        return;
    }
    out(t, c);
}

// The column after the line's bytes up to length, from where it started:
// what erasing a tab goes back to.
static size_t columnAfter(const Terminal& t, size_t length) {
    size_t column = t.lineColumn;
    for (size_t i = 0; i < length; ++i) {
        const unsigned char c = t.line[i];
        if (c == '\t') {
            column += 8 - (column & 7);
        } else if (iscntrl(c)) {
            if ((t.settings.c_lflag & ECHOCTL) != 0) {
                column += 2;
            }
        } else if (!continuation(t, c)) {
            ++column;
        }
    }
    return column;
}

// Erases what VERASE, VWERASE or VKILL takes of the line being edited,
// and its echo.
static void erase(Terminal& t, unsigned char c) {
    const tcflag_t lflag = t.settings.c_lflag;
    const bool all = special(t, c, VKILL);
    const bool word = !all && !special(t, c, VERASE) && (lflag & IEXTEN) != 0;
    if (t.lineLength == 0) {
        return;
    }
    if ((lflag & ECHO) == 0 && all) {
        t.lineLength = 0;
        return;
    }
    if (all && ((lflag & ECHOK) == 0 || (lflag & ECHOKE) == 0 || (lflag & ECHOE) == 0)) {
        t.lineLength = 0;
        echo(t, c);
        if ((lflag & ECHOK) != 0) {
            out(t, '\n');
        }
        t.lineColumn = t.column;
        return;
    }
    size_t alnums = 0;
    while (t.lineLength > 0) {
        size_t start = t.lineLength - 1;
        while (start > 0 && continuation(t, t.line[start])) {
            --start;
        }
        const unsigned char last = t.line[start];
        if (word) {
            if (isalnum(last) || last == '_') {
                ++alnums;
            } else if (alnums > 0) {
                break;
            }
        }
        t.lineLength = start;
        if ((lflag & ECHO) != 0) {
            if ((lflag & ECHOE) == 0 && !word) {
                echo(t, c);
            } else if (last == '\t') {
                const size_t to = columnAfter(t, t.lineLength);
                while (t.column > to) {
                    out(t, '\b');
                }
            } else if (iscntrl(last)) {
                if ((lflag & ECHOCTL) != 0) {
                    for (int k = 0; k < 2; ++k) {
                        out(t, '\b');
                        out(t, ' ');
                        out(t, '\b');
                    }
                }
            } else {
                out(t, '\b');
                out(t, ' ');
                out(t, '\b');
            }
        }
        if (!all && !word) {
            break;
        }
    }
}

// A line ends: what was edited goes to what the slave reads, its last
// byte, a newline or VEOL's, marked; VEOF ends it with an end of file.
// The line and what is to read leave room for its end.
static void endLine(Terminal& t, unsigned char c, bool eof) {
    for (size_t i = 0; i < t.lineLength; ++i) {
        t.input.push(t.line[i], plain);
    }
    t.input.push(c, eof ? endOfFile : lineEnd);
    t.lineLength = 0;
    t.lineColumn = t.column;
    ++t.lines;
    t.lastInput = now();
}

// Drops what the slave has to read, and what is being edited.
static void flushInput(Terminal& t) {
    t.input.clear();
    t.lines = 0;
    t.lineLength = 0;
    t.literal = false;
}

// A special character of ISIG: its signal to the foreground group, the
// input and output flushed but with NOFLSH, and its echo.
static void raise(Terminal& t, unsigned char c, int sig) {
    if ((t.settings.c_lflag & NOFLSH) == 0) {
        flushInput(t);
        t.output.clear();
    }
    if ((t.settings.c_lflag & ECHO) != 0) {
        echo(t, c);
    }
    t.lineColumn = t.column;
    signalGroup(t.foreground, sig);
}

// Whether the line being edited has no room for a byte more: a byte then
// is lost, with a bell for IMAXBEL, as Linux loses it.
static bool full(Terminal& t) {
    if (t.lineLength + t.input.count < inputLimit) {
        return false;
    }
    if ((t.settings.c_iflag & IMAXBEL) != 0 && (t.settings.c_lflag & ECHO) != 0) {
        out(t, '\a');
    }
    return true;
}

// A byte the master wrote, through the slave's input processing: false
// when it found no room, and nothing of it was taken. In the canonical
// mode a byte the line has no room for is taken and lost.
static bool in(Terminal& t, unsigned char c) {
    const tcflag_t iflag = t.settings.c_iflag;
    const tcflag_t lflag = t.settings.c_lflag;
    const bool canonical = (lflag & ICANON) != 0;
    if ((iflag & ISTRIP) != 0) {
        c &= 0x7f;
    }
    if (t.literal) {
        t.literal = false;
        if (full(t)) {
            return true;
        }
        if (t.lineLength == 0) {
            t.lineColumn = t.column;
        }
        t.line[t.lineLength++] = c;
        if ((lflag & ECHO) != 0) {
            echo(t, c);
        }
        return true;
    }
    if (c == '\r') {
        if ((iflag & IGNCR) != 0) {
            return true;
        }
        if ((iflag & ICRNL) != 0) {
            c = '\n';
        }
    } else if (c == '\n' && (iflag & INLCR) != 0) {
        c = '\r';
    }
    if ((iflag & IXON) != 0) {
        if (special(t, c, VSTOP)) {
            t.stopped = true;
            return true;
        }
        if (special(t, c, VSTART)) {
            t.stopped = false;
            return true;
        }
        if ((iflag & IXANY) != 0) {
            t.stopped = false;
        }
    }
    if ((lflag & ISIG) != 0) {
        if (special(t, c, VINTR)) {
            raise(t, c, SIGINT);
            return true;
        }
        if (special(t, c, VQUIT)) {
            raise(t, c, SIGQUIT);
            return true;
        }
        if (special(t, c, VSUSP)) {
            raise(t, c, SIGTSTP);
            return true;
        }
    }
    if (!canonical) {
        if (t.input.room() == 0) {
            return false;
        }
        t.input.push(c, plain);
        t.lastInput = now();
        if ((lflag & ECHO) != 0) {
            echo(t, c);
        }
        return true;
    }
    if (special(t, c, VERASE) || special(t, c, VKILL) || ((lflag & IEXTEN) != 0 && special(t, c, VWERASE))) {
        erase(t, c);
        return true;
    }
    if ((lflag & IEXTEN) != 0 && special(t, c, VLNEXT)) {
        t.literal = true;
        if ((lflag & ECHO) != 0 && (lflag & ECHOCTL) != 0) {
            out(t, '^');
            out(t, '\b');
        }
        return true;
    }
    if ((lflag & IEXTEN) != 0 && special(t, c, VREPRINT)) {
        if ((lflag & ECHO) != 0) {
            echo(t, c);
            out(t, '\n');
            t.lineColumn = t.column;
            for (size_t i = 0; i < t.lineLength; ++i) {
                echo(t, t.line[i]);
            }
        }
        return true;
    }
    if (c == '\n' || special(t, c, VEOL) || ((lflag & IEXTEN) != 0 && special(t, c, VEOL2))) {
        if ((lflag & ECHO) != 0 || (c == '\n' && (lflag & ECHONL) != 0)) {
            if (c == '\n') {
                out(t, '\n');
            } else {
                echo(t, c);
            }
        }
        endLine(t, c, false);
        return true;
    }
    if (special(t, c, VEOF)) {
        endLine(t, c, true);
        return true;
    }
    if (full(t)) {
        return true;
    }
    if (t.lineLength == 0) {
        t.lineColumn = t.column;
    }
    t.line[t.lineLength++] = c;
    if ((lflag & ECHO) != 0) {
        echo(t, c);
    }
    return true;
}

static bool canonical(const Terminal& t) {
    return (t.settings.c_lflag & ICANON) != 0;
}

// The bytes a read of the slave would take now: a line's, or as many as
// are there; for a canonical read with nothing, none.
static size_t inq(const Terminal& t) {
    if (!canonical(t)) {
        size_t n = 0;
        for (size_t i = 0; i < t.input.count; ++i) {
            n += t.input.markAt(i) != endOfFile;
        }
        return n;
    }
    size_t n = 0;
    for (size_t i = 0, lines = 0; i < t.input.count && lines < t.lines; ++i) {
        const unsigned char mark = t.input.markAt(i);
        n += mark != endOfFile;
        lines += mark != plain;
    }
    return n;
}

// Whether a read of the slave finds what it waits for now: a line, or
// VMIN bytes; with VTIME a byte.
static bool readable(const Terminal& t) {
    if (t.hungUp) {
        return true;
    }
    if (canonical(t)) {
        return t.lines > 0;
    }
    const cc_t min = t.settings.c_cc[VMIN];
    const cc_t time = t.settings.c_cc[VTIME];
    if (min == 0 && time == 0) {
        return true;
    }
    return t.input.count >= (time != 0 ? 1 : min);
}

static uint32_t eventsOf(const Open& o) {
    const Terminal& t = *o.terminal;
    if (o.master) {
        uint32_t events = 0;
        if (t.output.count > 0 || t.slaves == 0) {
            events |= POLLIN;
        }
        if (canonical(t) ? t.lineLength + t.input.count < inputLimit : t.input.room() > 0) {
            events |= POLLOUT;
        }
        if (t.slaves == 0) {
            events |= POLLHUP;
        }
        return events;
    }
    if (t.hungUp) {
        return POLLIN | POLLOUT | POLLERR | POLLHUP;
    }
    uint32_t events = readable(t) ? POLLIN : 0;
    if (!t.stopped && t.output.room() > 0) {
        events |= POLLOUT;
    }
    return events;
}

// Tells every open file of the terminal what changed of its readiness,
// its foreground group and its TOSTOP.
static void tellAll(const Terminal& t) {
    const uint32_t foreground = uint32_t(t.foreground);
    const uint32_t flags = (t.settings.c_lflag & TOSTOP) != 0 ? readyTostop : 0;
    for (Open& o : opens) {
        if (o.terminal == &t) {
            const uint32_t events = eventsOf(o);
            if (events != o.told || foreground != o.toldForeground || flags != o.toldFlags) {
                tell(o.fd, events, foreground, flags);
                o.told = events;
                o.toldForeground = foreground;
                o.toldFlags = flags;
            }
        }
    }
}

static void answerCount(int fd, uint32_t count) {
    answer(fd, opOk, 0, &count, sizeof count);
}

static void answerWrote(int fd, uint32_t count) {
    unsigned char wrote[12] = {};
    memcpy(wrote, &count, 4);
    answer(fd, opOk, 0, wrote, sizeof wrote);
}

// Takes what a read of the slave gets into the reader's memory: in the
// canonical mode up to the end of a line, an end of file none of it; else
// what is there, the ends of file dropped.
static uint32_t takeInput(Terminal& t, int window, uint32_t woff, uint32_t length) {
    unsigned char bytes[4096];
    size_t n = 0;
    while (t.input.count > 0 && n < length) {
        const unsigned char c = t.input.byteAt(0);
        const unsigned char mark = t.input.markAt(0);
        if (mark == endOfFile) {
            t.input.drop();
            if (canonical(t)) {
                --t.lines;
                break;
            }
            continue;
        }
        bytes[n++] = c;
        t.input.drop();
        if (mark == lineEnd && canonical(t)) {
            --t.lines;
            break;
        }
    }
    if (!canonical(t)) {
        t.lines = 0;
    }
    pwrite(window, bytes, n, woff);
    return uint32_t(n);
}

// Whether a read of the slave that waits since start may end now, by
// VTIME's time if it has one; due is when it will.
static bool readDone(const Terminal& t, const Waiting& w, int64_t at, int64_t& due) {
    due = -1;
    if (t.hungUp || readable(t)) {
        if (canonical(t) || t.hungUp) {
            return true;
        }
        const cc_t min = t.settings.c_cc[VMIN];
        if (min == 0 || t.input.count >= MIN(size_t(min), size_t(w.length))) {
            return true;
        }
    }
    const cc_t time = t.settings.c_cc[VTIME];
    if (canonical(t) || time == 0) {
        return false;
    }
    if (t.settings.c_cc[VMIN] == 0) {
        due = w.start + int64_t(time) * 100;
    } else if (t.input.count > 0) {
        due = t.lastInput + int64_t(time) * 100;
    } else {
        return false;
    }
    return at >= due;
}

// Moves what a write of the slave holds to what the master reads, as far
// as there is room: whether all went.
static bool moveOutput(Terminal& t, Waiting& w) {
    unsigned char bytes[1024];
    while (w.done < w.length && !t.stopped) {
        const size_t n = MIN(sizeof bytes, size_t(w.length - w.done));
        const ssize_t got = pread(w.window, bytes, n, w.woff + w.done);
        if (got <= 0) {
            return true;
        }
        size_t put = 0;
        while (put < size_t(got) && out(t, bytes[put])) {
            ++put;
        }
        w.done += uint32_t(put);
        if (put < size_t(got)) {
            return false;
        }
    }
    return w.done == w.length;
}

// Moves what a write of the master holds through the input processing,
// as far as there is room: whether all went.
static bool moveInput(Terminal& t, Waiting& w) {
    unsigned char bytes[1024];
    while (w.done < w.length) {
        const size_t n = MIN(sizeof bytes, size_t(w.length - w.done));
        const ssize_t got = pread(w.window, bytes, n, w.woff + w.done);
        if (got <= 0) {
            return true;
        }
        size_t put = 0;
        while (put < size_t(got) && in(t, bytes[put])) {
            ++put;
        }
        w.done += uint32_t(put);
        if (put < size_t(got)) {
            return false;
        }
    }
    return true;
}

// Takes what the master reads into its memory.
static uint32_t takeOutput(Terminal& t, int window, uint32_t woff, uint32_t length) {
    unsigned char bytes[4096];
    size_t n = 0;
    while (t.output.count > 0 && n < MIN(size_t(length), sizeof bytes)) {
        bytes[n++] = t.output.take();
    }
    pwrite(window, bytes, n, woff);
    return uint32_t(n);
}

static void apply(Terminal& t, const termios& settings);

static void release(Waiting& w) {
    if (w.window >= 0) {
        close(w.window);
    }
    unwatch(epoll, w.at);
    w = {};
    w.window = -1;
}

// Answers what of the waiting requests of a terminal can be, in order.
static void progress(Terminal& t) {
    const int64_t at = now();
    for (Waiting& w : waits) {
        if (!w.at || w.open->terminal != &t) {
            continue;
        }
        const int fd = w.at->fd;
        switch (w.kind) {
            case Wait::Read: {
                int64_t due;
                if (w.open->master) {
                    if (t.output.count > 0) {
                        answerCount(fd, takeOutput(t, w.window, w.woff, w.length));
                        release(w);
                    } else if (t.slaves == 0) {
                        fail(fd, EIO);
                        release(w);
                    }
                } else if (readDone(t, w, at, due)) {
                    answerCount(fd, t.hungUp ? 0 : takeInput(t, w.window, w.woff, w.length));
                    release(w);
                }
                break;
            }
            case Wait::Write:
                if (w.open->master ? moveInput(t, w) : (t.hungUp || moveOutput(t, w))) {
                    if (!w.open->master && t.hungUp && w.done == 0) {
                        fail(fd, EIO);
                    } else {
                        answerWrote(fd, w.done);
                    }
                    release(w);
                }
                break;
            case Wait::Drain:
                if (t.output.count == 0 || t.hungUp) {
                    if (w.settles) {
                        if (w.when == TCSAFLUSH) {
                            flushInput(t);
                        }
                        apply(t, w.settings);
                    }
                    answer(fd, opOk, 0);
                    release(w);
                }
                break;
        }
    }
    tellAll(t);
}

// Keeps a request to answer it later: its endpoint hears CANCEL.
static Waiting* hold(Open& o, Request& r, Wait kind) {
    for (Waiting& w : waits) {
        if (!w.at) {
            w = {};
            w.open = &o;
            w.kind = kind;
            w.window = r.window();
            w.length = r.h.arg;
            w.start = now();
            w.at = watch(epoll, {r.reply(), false, false, false, &w});
            if (w.window >= 0) {
                r.fds[0] = -1;
            }
            r.fds[r.count - 1] = -1;
            return &w;
        }
    }
    fail(r.reply(), EAGAIN);
    return nullptr;
}

static bool isWaiting(const void* of) {
    return of >= static_cast<const void*>(waits) && of < static_cast<const void*>(waits + sizeof waits / sizeof waits[0]);
}

static bool isOpen(const void* of) {
    return of >= static_cast<const void*>(opens) && of < static_cast<const void*>(opens + sizeof opens / sizeof opens[0]);
}

// A CANCEL of a waiting request: it ends with what it did, or EINTR.
static void cancel(Waiting& w) {
    const int fd = w.at->fd;
    Terminal& t = *w.open->terminal;
    if (w.kind == Wait::Write && w.done > 0) {
        answerWrote(fd, w.done);
    } else {
        fail(fd, EINTR);
    }
    release(w);
    tellAll(t);
}

static void readFile(Open& o, Request& r) {
    Terminal& t = *o.terminal;
    Io io;
    if (!o.reads) {
        return fail(r.reply(), EBADF);
    }
    if (r.window() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    if (Waiting* w = hold(o, r, Wait::Read)) {
        w->woff = io.woff;
        progress(t);
        if (w->at && (io.flags & ioNonblock) != 0) {
            fail(w->at->fd, EAGAIN);
            release(*w);
        }
    }
}

static void writeFile(Open& o, Request& r) {
    Terminal& t = *o.terminal;
    Io io;
    if (!o.writes) {
        return fail(r.reply(), EBADF);
    }
    if (r.window() < 0 || !r.io(io)) {
        return fail(r.reply(), EINVAL);
    }
    if (!o.master && t.hungUp) {
        return fail(r.reply(), EIO);
    }
    if (Waiting* w = hold(o, r, Wait::Write)) {
        w->woff = io.woff;
        progress(t);
        if (w->at && (io.flags & ioNonblock) != 0) {
            if (w->done > 0) {
                answerWrote(w->at->fd, w->done);
            } else {
                fail(w->at->fd, EAGAIN);
            }
            release(*w);
            tellAll(t);
        }
    }
}

// New settings of the slave: what the slave has to read stays readable
// across a change of ICANON, as one line or as bytes.
static void apply(Terminal& t, const termios& settings) {
    const bool was = canonical(t);
    t.settings = settings;
    if (was && !canonical(t)) {
        for (size_t i = 0; i < t.lineLength && t.input.room() > 0; ++i) {
            t.input.push(t.line[i], plain);
        }
        t.lineLength = 0;
        t.lines = 0;
    } else if (!was && canonical(t) && t.input.count > 0) {
        t.lines = 1;
        const size_t last = (t.input.head + t.input.count - 1) % sizeof t.input.bytes;
        t.input.marks[last] = lineEnd;
    }
}

static void setAttributes(Open& o, Request& r) {
    Terminal& t = *o.terminal;
    termios settings;
    if (r.size != sizeof settings || r.h.arg > TCSAFLUSH) {
        return fail(r.reply(), EINVAL);
    }
    memcpy(&settings, r.body, sizeof settings);
    if (o.master) {
        t.masterSettings = settings;
        return answer(r.reply(), opOk, 0);
    }
    if (r.h.arg == TCSANOW) {
        apply(t, settings);
        progress(t);
        return answer(r.reply(), opOk, 0);
    }
    if (Waiting* w = hold(o, r, Wait::Drain)) {
        w->settles = true;
        w->when = r.h.arg;
        w->settings = settings;
        progress(t);
    }
}

static void flush(Open& o, Request& r) {
    Terminal& t = *o.terminal;
    const uint32_t queue = r.h.arg;
    if (queue > TCIOFLUSH) {
        return fail(r.reply(), EINVAL);
    }
    // The input of the master's is what it reads, the slave's output; its
    // output, what it wrote, the slave's input.
    const bool input = queue == TCIFLUSH || queue == TCIOFLUSH;
    const bool output = queue == TCOFLUSH || queue == TCIOFLUSH;
    if (o.master ? output : input) {
        flushInput(t);
    }
    if (o.master ? input : output) {
        t.output.clear();
    }
    answer(r.reply(), opOk, 0);
    progress(t);
}

static void flow(Open& o, Request& r) {
    Terminal& t = *o.terminal;
    switch (r.h.arg) {
        case TCOOFF:
            t.stopped = true;
            break;
        case TCOON:
            t.stopped = false;
            break;
        case TCIOFF:
        case TCION: {
            const cc_t c = t.settings.c_cc[r.h.arg == TCIOFF ? VSTOP : VSTART];
            if (c != _POSIX_VDISABLE && t.output.room() > 0) {
                t.output.push(c);
            }
            break;
        }
        default:
            return fail(r.reply(), EINVAL);
    }
    answer(r.reply(), opOk, 0);
    progress(t);
}

static void setSize(Open& o, Request& r) {
    Terminal& t = *o.terminal;
    winsize size;
    if (r.size != sizeof size) {
        return fail(r.reply(), EINVAL);
    }
    memcpy(&size, r.body, sizeof size);
    const bool changed = memcmp(&size, &t.size, sizeof size) != 0;
    t.size = size;
    answer(r.reply(), opOk, 0);
    if (changed) {
        signalGroup(t.foreground, SIGWINCH);
    }
}

static void name(const Open& o, const Request& r) {
    char path[32];
    if (r.h.arg != 0 && !o.master) {
        return fail(r.reply(), ENOTTY);
    }
    if (o.master && r.h.arg == 0) {
        snprintf(path, sizeof path, "/dev/pty");
    } else {
        snprintf(path, sizeof path, "/dev/pts/%d", o.terminal->number);
    }
    answer(r.reply(), opOk, 0, path, strlen(path));
}

static void controls(Open& o, const Request& r) {
    Terminal& t = *o.terminal;
    uint32_t group;
    if (o.master) {
        return fail(r.reply(), ENOTTY);
    }
    if (r.size != sizeof group) {
        return fail(r.reply(), EINVAL);
    }
    memcpy(&group, r.body, sizeof group);
    if (r.h.arg != 0 && t.session != 0 && t.session != pid_t(r.h.arg)) {
        return fail(r.reply(), EPERM);
    }
    t.session = pid_t(r.h.arg);
    t.foreground = r.h.arg != 0 ? pid_t(group) : 0;
    tellAll(t);
    answer(r.reply(), opOk, 0);
}

static void fileCall(Open& o, Request& r) {
    Terminal& t = *o.terminal;
    switch (r.h.op) {
        case opRead:
            return readFile(o, r);
        case opWrite:
            return writeFile(o, r);
        case opFstat:
            return answer(r.reply(), opOk, 0, o.master ? &device.st : &t.slave.st, sizeof device.st);
        case opFsync:
            return answer(r.reply(), opOk, 0);
        case opFchmod:
        case opFchown:
        case opFutimes:
            return fail(r.reply(), EPERM);
        case opTcgetattr:
            return answer(r.reply(), opOk, 0, o.master ? &t.masterSettings : &t.settings, sizeof t.settings);
        case opTcsetattr:
            return setAttributes(o, r);
        case opTcflush:
            return flush(o, r);
        case opTcflow:
            return flow(o, r);
        case opTcdrain:
            if (o.master) {
                return answer(r.reply(), opOk, 0);
            }
            if (hold(o, r, Wait::Drain)) {
                progress(t);
            }
            return;
        case opGetWinsize:
            return answer(r.reply(), opOk, 0, &t.size, sizeof t.size);
        case opSetWinsize:
            return setSize(o, r);
        case opInq:
            return answerCount(r.reply(), uint32_t(o.master ? t.output.count : inq(t)));
        case opOutq:
            return answerCount(r.reply(), uint32_t(o.master ? t.input.count + t.lineLength : t.output.count));
        case opName:
            return name(o, r);
        case opGetPgrp:
            return answerCount(r.reply(), uint32_t(t.foreground));
        case opSetPgrp:
            t.foreground = pid_t(r.h.arg);
            tellAll(t);
            return answer(r.reply(), opOk, 0);
        case opSetCtty:
            return controls(o, r);
        default:
            return fail(r.reply(), EINVAL);
    }
}

// An open of a terminal's end: the open file is its endpoint, which
// hears the terminal's readiness from its OK on.
static void opened(Terminal& t, bool master, Request& r) {
    const uint32_t access = r.h.arg & O_ACCMODE;
    for (Open& o : opens) {
        if (!o.terminal) {
            o = {&t, master, access != O_WRONLY, access != O_RDONLY, r.reply(), 0xffffffff, 0, 0};
            watch(epoll, {r.reply(), false, o.reads, o.writes, &o});
            r.fds[r.count - 1] = -1;
            ++(master ? t.masters : t.slaves);
            answer(o.fd, opOk, openTells | openTerminal, master ? &device.st : &t.slave.st, sizeof device.st);
            progress(t);
            return;
        }
    }
    fail(r.reply(), ENFILE);
}

// An open of /dev/pty: a new terminal, whose slave is registered.
static void openMaster(Request& r) {
    for (int n = 0; n < int(sizeof terminals / sizeof terminals[0]); ++n) {
        if (terminals[n]) {
            continue;
        }
        auto* t = static_cast<Terminal*>(calloc(1, sizeof(Terminal)));
        if (!t) {
            return fail(r.reply(), ENOMEM);
        }
        t->number = n;
        startSettings(*t);
        char path[16];
        snprintf(path, sizeof path, "pts/%d", n);
        if (const int e = enroll(registry, path, S_IFCHR | 0666, t->slave)) {
            free(t);
            return fail(r.reply(), e);
        }
        terminals[n] = t;
        t->channel = watch(epoll, {t->slave.end, true, false, false, t});
        return opened(*t, true, r);
    }
    fail(r.reply(), ENOSPC);
}

// The master went: the slave hangs up, and the leader of the session it
// controls hears SIGHUP and SIGCONT, as Linux's does; its name goes.
static void hangUp(Terminal& t) {
    t.hungUp = true;
    if (t.session > 0) {
        kill(t.session, SIGHUP);
        kill(t.session, SIGCONT);
    }
    t.session = 0;
    t.foreground = 0;
    unwatch(epoll, t.channel);
    t.channel = nullptr;
}

static bool handle(Endpoint& e, Request& r) {
    if (e.channel) {
        if (r.h.op != opOpen) {
            fail(r.reply(), EINVAL);
        } else if (e.of == &ptyTag) {
            openMaster(r);
        } else {
            Terminal& t = *static_cast<Terminal*>(e.of);
            if (t.hungUp) {
                fail(r.reply(), EIO);
            } else {
                opened(t, false, r);
            }
        }
        return false;
    }
    if (isWaiting(e.of)) {
        if (r.h.op == opCancel) {
            cancel(*static_cast<Waiting*>(e.of));
        }
        return false;
    }
    fileCall(*static_cast<Open*>(e.of), r);
    return false;
}

// The terminal is gone once neither end is open.
static void dropIfDone(Terminal& t) {
    if (t.masters > 0 || t.slaves > 0) {
        return;
    }
    for (Waiting& w : waits) {
        if (w.at && w.open->terminal == &t) {
            release(w);
        }
    }
    terminals[t.number] = nullptr;
    free(&t);
}

static void gone(Endpoint& e) {
    if (isWaiting(e.of)) {
        // The asker went; serve closes the endpoint itself.
        auto& w = *static_cast<Waiting*>(e.of);
        if (w.window >= 0) {
            close(w.window);
        }
        w = {};
        w.window = -1;
        return;
    }
    if (!isOpen(e.of)) {
        return;
    }
    // An open file closed: what still waits of it ends, EIO to its asker.
    Open& o = *static_cast<Open*>(e.of);
    Terminal& t = *o.terminal;
    for (Waiting& w : waits) {
        if (w.at && w.open == &o) {
            release(w);
        }
    }
    const bool master = o.master;
    o = {};
    if (master) {
        if (--t.masters == 0) {
            hangUp(t);
        }
    } else {
        --t.slaves;
    }
    progress(t);
    dropIfDone(t);
}

static int expire() {
    const int64_t at = now();
    int64_t next = -1;
    for (Terminal* t : terminals) {
        if (!t) {
            continue;
        }
        bool due = false;
        for (const Waiting& w : waits) {
            int64_t when;
            if (w.at && w.open->terminal == t && w.kind == Wait::Read && !w.open->master) {
                if (readDone(*t, w, at, when)) {
                    due = true;
                } else if (when >= 0 && (next < 0 || when < next)) {
                    next = when;
                }
            }
        }
        if (due) {
            progress(*t);
        }
    }
    return next < 0 ? -1 : int(MAX(next - at, int64_t(0)));
}

int ptyMain(int argc, char** argv) {
    int first;
    const int ready = readyDescriptor(argc, argv, first);
    if (first != argc) {
        dprintf(STDERR_FILENO, "usage: pty [-r fd]\n");
        return 2;
    }
    registry = open("/dev/init", O_RDWR | O_CLOEXEC);
    epoll = epoll_create1(EPOLL_CLOEXEC);
    if (registry < 0 || epoll < 0) {
        fatal("pty: errno %d", errno);
    }
    for (Waiting& w : waits) {
        w.window = -1;
    }
    signal(SIGPIPE, SIG_IGN);
    device = enroll("pty", "pty", S_IFCHR | 0666);
    watch(epoll, {device.end, true, false, false, &ptyTag});
    if (ready >= 0) {
        close(ready);
    }
    serve(epoll, handle, gone, nullptr, expire);
    return 0;
}
