#!/usr/bin/env python3
"""Midnight Commander in the system (bin/mc), on a terminal of the host's:
it runs on the system's pseudoterminal joined to the console, finds the
terminal the host names (TERM) in the terminfo database, draws its panels
and takes the terminal's keys and mouse, and keeps what it keeps in the
shell's HOME. The test is a terminal: it keeps the screen mc draws, as a
VT100 with xterm's additions would show it, and types.

On an xterm: the panels list two directories; F1 shows the help of mc's
manual and its index; the arrows move the bar; F3 views a file and F4
edits it, F2 saving what was typed; F5 copies to the other panel and F7
makes a directory; Enter on a tar archive lists it as a directory; F9
pulls a menu down; a click of the mouse moves the bar; a command typed in
mc's line runs in the subshell; cd to ftp:// lists the directory of an FTP
server, busybox's ftpd on the loopback, and F3 views a file of it; ^O
shows the subshell itself, mc's own child on a pseudoterminal it made; a
window of another size is redrawn; F10 leaves, the screen the shell's
again. mcedit and mcview are mc by other names. On a VT100, by its other
name, which the database has as a link, there are no colours and the keys
are the few it has: mc draws and moves all the same. And under the
kernel's checks of memory (vm run --asan) a tree in a panel forgets the
directory under its bar after another tree was open and closed: the
panel's tree is still told of the entry removed, and draws no freed one
(bin/mc/mc-tree-hook.patch).

    test_mc.py <vm> <rootfs> <tests.img>
"""
import fcntl
import os
import pty
import re
import select
import signal
import struct
import sys
import termios
import time

LINES = {"q": "─", "x": "│", "l": "┌", "k": "┐", "m": "└", "j": "┘", "t": "├", "u": "┤", "w": "┬", "v": "┴", "n": "┼", "a": "▒"}


def prompt(text):
    """The shell waits for a command: its prompt ends the screen."""
    return text.rstrip().endswith("#")


class Screen:
    """What a terminal shows of the bytes written to it: the characters, not
    their colours; the cursor, the scrolling region, the two character
    sets, the alternate screen, and the modes a program set, the
    application's cursor keys and the mouse's reports among them."""

    def __init__(self, rows, cols):
        self.resize(rows, cols)
        self.sets = ["B", "B"]
        self.shift = 0
        self.saved = (0, 0)
        self.pending = b""
        self.modes = set()
        self.main = None

    def resize(self, rows, cols):
        self.rows, self.cols = rows, cols
        self.cells = [[" "] * cols for _ in range(rows)]
        self.r = self.c = 0
        self.top, self.bottom = 0, rows - 1

    def blank(self):
        return [" "] * self.cols

    def alternate(self, on):
        """The alternate screen, empty as it is taken; the main one is as
        it was, cursor and all, once it is left."""
        if on and self.main is None:
            self.main = (self.cells, self.r, self.c)
            self.cells = [self.blank() for _ in range(self.rows)]
        elif not on and self.main is not None:
            cells, self.r, self.c = self.main
            self.main = None

            if len(cells) == self.rows and len(cells[0]) == self.cols:
                self.cells = cells
            else:
                self.cells = [self.blank() for _ in range(self.rows)]
                self.r, self.c = min(self.r, self.rows - 1), min(self.c, self.cols - 1)

    def put(self, ch):
        if self.sets[self.shift] == "0":
            ch = LINES.get(ch, ch)

        if self.c >= self.cols:
            self.c = 0
            self.newline()

        self.cells[self.r][self.c] = ch
        self.c += 1

    def newline(self):
        if self.r == self.bottom:
            del self.cells[self.top]
            self.cells.insert(self.bottom, self.blank())
        elif self.r < self.rows - 1:
            self.r += 1

    def csi(self, params, final):
        private = params[:1] in ("?", ">", "=", "<")
        numbers = [int(n) if n.isdigit() else 0 for n in re.split("[;:]", params.lstrip("?>=<"))]

        def n(i, default=1):
            return numbers[i] if len(numbers) > i and numbers[i] else default

        if private:
            if final in "hl":
                for mode in numbers:
                    (self.modes.add if final == "h" else self.modes.discard)(mode)

                    if mode == 1049:
                        self.alternate(final == "h")
        elif final in "Hf":
            self.r, self.c = min(n(0), self.rows) - 1, min(n(1), self.cols) - 1
        elif final == "A":
            self.r = max(self.r - n(0), 0)
        elif final == "B":
            self.r = min(self.r + n(0), self.rows - 1)
        elif final == "C":
            self.c = min(self.c + n(0), self.cols - 1)
        elif final == "D":
            self.c = max(self.c - n(0), 0)
        elif final == "G":
            self.c = min(n(0), self.cols) - 1
        elif final == "d":
            self.r = min(n(0), self.rows) - 1
        elif final == "J":
            mode = numbers[0]

            if mode == 0:
                self.cells[self.r][self.c:] = [" "] * (self.cols - self.c)
                self.cells[self.r + 1:] = [self.blank() for _ in self.cells[self.r + 1:]]
            elif mode == 1:
                self.cells[:self.r] = [self.blank() for _ in self.cells[:self.r]]
                self.cells[self.r][:self.c + 1] = [" "] * (self.c + 1)
            else:
                self.cells = [self.blank() for _ in self.cells]
        elif final == "K":
            mode = numbers[0]

            if mode == 0:
                self.cells[self.r][self.c:] = [" "] * (self.cols - self.c)
            elif mode == 1:
                self.cells[self.r][:self.c + 1] = [" "] * (self.c + 1)
            else:
                self.cells[self.r] = self.blank()
        elif final == "r":
            self.top, self.bottom = n(0) - 1, n(1, self.rows) - 1
            self.r = self.c = 0
        elif final == "L":
            for _ in range(n(0)):
                del self.cells[self.bottom]
                self.cells.insert(self.r, self.blank())
        elif final == "M":
            for _ in range(n(0)):
                del self.cells[self.r]
                self.cells.insert(self.bottom, self.blank())
        elif final == "P":
            row = self.cells[self.r]
            del row[self.c:self.c + n(0)]
            row.extend([" "] * (self.cols - len(row)))
        elif final == "@":
            row = self.cells[self.r]
            row[self.c:self.c] = [" "] * n(0)
            del row[self.cols:]
        elif final == "X":
            end = min(self.c + n(0), self.cols)
            self.cells[self.r][self.c:end] = [" "] * (end - self.c)

    def escape(self, rest):
        """Takes the escape sequence rest begins after its ESC; returns how
        many bytes it is, 0 for one not whole yet."""
        kind = chr(rest[0])

        if kind == "[":
            match = re.match(rb"\[([0-9;:?>=<]*)[ -/]*([@-~])", rest)

            if not match:
                return 0 if len(rest) < 64 else 1

            self.csi(match[1].decode(), match[2].decode())

            return match.end()

        if kind in "()*+":
            if len(rest) < 2:
                return 0

            if kind in "()":
                self.sets[0 if kind == "(" else 1] = chr(rest[1])

            return 2

        if kind == "]":
            end = re.search(rb"\x07|\x1b\\", rest)

            return end.end() if end else 0

        if kind == "7":
            self.saved = (self.r, self.c)
        elif kind == "8":
            self.r, self.c = self.saved
        elif kind == "M":
            if self.r == self.top:
                del self.cells[self.bottom]
                self.cells.insert(self.top, self.blank())
            else:
                self.r = max(self.r - 1, 0)
        elif kind == "D":
            self.newline()
        elif kind == "E":
            self.c = 0
            self.newline()

        return 1

    def feed(self, data):
        data = self.pending + data
        self.pending = b""
        i = 0

        while i < len(data):
            b = data[i]

            if b == 0x1B:
                taken = self.escape(data[i + 1:]) if i + 1 < len(data) else 0

                if not taken:
                    self.pending = data[i:]

                    return

                i += 1 + taken
            elif b == 0x0D:
                self.c = 0
                i += 1
            elif b == 0x0A:
                self.newline()
                i += 1
            elif b == 0x08:
                self.c = max(self.c - 1, 0)
                i += 1
            elif b == 0x09:
                self.c = min((self.c // 8 + 1) * 8, self.cols - 1)
                i += 1
            elif b in (0x0E, 0x0F):
                self.shift = 1 if b == 0x0E else 0
                i += 1
            elif b < 0x20 or b == 0x7F:
                i += 1
            elif b < 0x80:
                self.put(chr(b))
                i += 1
            else:
                length = 2 if b >> 5 == 6 else 3 if b >> 4 == 14 else 4 if b >> 3 == 30 else 1

                if i + length > len(data):
                    self.pending = data[i:]

                    return

                self.put(data[i:i + length].decode("utf-8", "replace"))
                i += length

    def text(self):
        return "\n".join("".join(row).rstrip() for row in self.cells)


class Terminal:
    """A terminal of rows by cols, of the type kind as the host names it,
    with the system on it, its shell at the prompt."""

    # xterm's keys: the arrows are the application's while mc asks for them.
    KEYS = {"f1": "\x1bOP", "f2": "\x1bOQ", "f3": "\x1bOR", "f4": "\x1bOS", "f5": "\x1b[15~", "f7": "\x1b[18~", "f9": "\x1b[20~", "f10": "\x1b[21~"}
    ARROWS = {"up": "A", "down": "B", "right": "C", "left": "D"}

    def __init__(self, vm, rootfs, image, rows=24, cols=80, flags=(), kind="xterm-256color"):
        self.screen = Screen(rows, cols)
        env = dict(os.environ, VM_MEMORY_TAIL="strict", VM_TAIL_PROFILE="", TERM=kind)
        self.pid, self.fd = pty.fork()

        if self.pid == 0:
            fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
            os.execve(vm, [vm, "run", *flags, "--disk", f"image={image}", "--", rootfs], env)

        self.until(prompt, "the shell's prompt", 120)

    def pump(self, seconds):
        ready, _, _ = select.select([self.fd], [], [], seconds)

        if ready:
            try:
                self.screen.feed(os.read(self.fd, 65536))
            except OSError:
                pass

    def until(self, wanted, what, seconds=30):
        """Waits until the screen has wanted, a text or a predicate of the
        screen's text."""
        found = wanted if callable(wanted) else (lambda text: wanted in text)
        end = time.time() + seconds

        while not found(self.screen.text()):
            if time.time() > end:
                raise AssertionError(f"{what}: the screen is\n{self.screen.text()}")

            self.pump(0.05)

        # what follows at once is of the same drawing
        self.pump(0.2)

    def send(self, text):
        os.write(self.fd, text.encode())

    def key(self, name):
        if name in self.ARROWS:
            self.send(("\x1bO" if 1 in self.screen.modes else "\x1b[") + self.ARROWS[name])
        else:
            self.send(self.KEYS[name])

    def click(self, row, col):
        """A press and a release of the left button, as xterm reports them
        in the SGR encoding mc asks for."""
        self.send(f"\x1b[<0;{col};{row}M\x1b[<0;{col};{row}m")

    def resize(self, rows, cols):
        fcntl.ioctl(self.fd, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
        self.screen.resize(rows, cols)
        os.kill(self.pid, signal.SIGWINCH)

    def row(self, r):
        return "".join(self.screen.cells[r]).rstrip()

    def close(self):
        """Ends the system by the shell's exit and returns vm's status."""
        self.send("exit 7\r")
        end = time.time() + 30

        while time.time() < end:
            pid, status = os.waitpid(self.pid, os.WNOHANG)

            if pid:
                os.close(self.fd)

                return os.waitstatus_to_exitcode(status)

            self.pump(0.05)

        os.kill(self.pid, signal.SIGKILL)
        os.waitpid(self.pid, 0)

        raise AssertionError(f"the system did not end: the screen is\n{self.screen.text()}")


SETUP = ("cd /run && mkdir -p work/sub other ftp && cd work && printf 'hello mc\\nsecond line\\n' > a.txt && "
         "printf 'inside\\n' > sub/in.txt && tar cf t.tar a.txt sub && printf 'over ftp\\n' > ../ftp/remote.txt && "
         "(cd ../ftp && tcpsvd 127.0.0.1 2121 ftpd -w -a root > /dev/null 2>&1 &) && cd /run && clear\r")


def status(term, name, what, side=0):
    """The bar is on name in the left panel, or the right one: the panel's
    line under its files, the fifth from the screen's end, names the file
    under the bar, a directory with its slash."""
    def there(text):
        screen = term.screen
        half = screen.cols // 2
        line = "".join(screen.cells[screen.rows - 5][side * half + 1:(side + 1) * half - 1])

        return line.strip() == name

    term.until(there, what)


def xterm(vm, rootfs, image):
    term = Terminal(vm, rootfs, image)
    term.send(SETUP)
    term.until(lambda text: text.strip() == "~ #", "the files of the test")
    term.send("mc /run/work /run/other\r")
    term.until("10Quit", "mc's keys line")
    text = term.screen.text()
    assert term.row(0).split() == ["Left", "File", "Command", "Options", "Right"], text
    assert "<─ ~/work ─" in term.row(1) and "<─ ~/other ─" in term.row(1), text
    assert [line[1:17].strip() for line in text.splitlines()[3:8]] == ["/..", "/sub", "a.txt", "t.tar", ""], text
    assert term.row(23).split() == ["1Help", "2Menu", "3View", "4Edit", "5Copy", "6RenMov", "7Mkdir", "8Delete", "9PullDn10Quit"], text
    assert {1049, 1002, 1006} <= term.screen.modes, f"the alternate screen and the mouse: {sorted(term.screen.modes)}"

    # F1 shows the help, of mc's manual: its main node, then its index; F10
    # leaves it
    term.key("f1")
    term.until("This is the main help screen for GNU Midnight Commander.", "the help's main node")
    term.key("f2")
    term.until(lambda text: "Topics:" in text and "Mouse Support" in text, "the help's index")
    term.key("f10")
    term.until(lambda text: term.row(23).split()[:2] == ["1Help", "2Menu"] and "Topics:" not in text, "the panels after the help")

    # the arrows move the bar
    term.key("down")
    status(term, "/sub", "the bar on sub")
    term.key("down")
    status(term, "a.txt", "the bar on a.txt")

    # F3 views the file, F10 leaves the viewer
    term.key("f3")
    term.until("3Quit", "the viewer")
    text = term.screen.text()
    assert term.row(0).startswith("/run/work/a.txt") and term.row(1) == "hello mc" and term.row(2) == "second line", text
    term.key("f10")
    term.until("10Quit", "the panels after the viewer")

    # F4 edits it: what is typed goes before the first line, F2 saves
    term.key("f4")
    term.until("2Save", "the editor")
    term.send("XYZ ")
    term.until(lambda text: term.row(1) == "XYZ hello mc", "the typed text")
    term.key("f2")
    term.until("Confirm save file", "the question of the save")
    term.send("\r")
    term.until(lambda text: "Confirm save file" not in text, "the file saved")
    term.key("f10")
    term.until(lambda text: "10Quit" in text and " 25│" in term.row(5), "the file's new size")

    # F5 copies it to the other panel
    term.key("f5")
    term.until("Copy file", "the copy's dialog")
    term.send("\r")
    term.until(lambda text: "Copy file" not in text and "a.txt" in term.row(4)[40:], "the copy in the other panel")

    # F7 makes a directory
    term.key("f7")
    term.until("Create a new Directory", "the dialog of a new directory")
    term.send("made\r")
    term.until(lambda text: term.row(4)[1:17].strip() == "/made", "the new directory")

    # Enter on the archive lists it
    for _ in range(3):
        term.key("down")

    status(term, "t.tar", "the bar on the archive")
    term.send("\r")
    term.until("t.tar/utar://", "the archive's path")
    text = term.screen.text()
    assert [line[1:17].strip() for line in text.splitlines()[3:7]] == ["/..", "/sub", "a.txt", ""], text
    term.send("\r")
    term.until(lambda text: "t.tar/utar://" not in text, "the directory of the archive again")

    # F9 pulls the menu down
    term.key("f9")
    term.send("\r")
    term.until("Listing format...", "the menu of the left panel")
    term.send("\x1b\x1b")
    term.until(lambda text: "Listing format..." not in text, "the menu gone")

    # a click in the other panel moves the bar there
    term.click(5, 45)
    status(term, "a.txt", "the bar under the mouse", side=1)
    term.until("~/other #", "the other panel's directory in mc's line")
    term.send("\t")
    term.until("~/work #", "the first panel's directory in mc's line")

    # a command in mc's line runs in the subshell, in the panel's directory
    term.send("echo from-mc > typed; echo done\r")
    term.until(lambda text: any(line[1:17].strip() == "typed" for line in text.splitlines()[3:10]), "the command's file")

    # cd to ftp:// lists a directory of busybox's ftpd on the loopback, of
    # mc's file system of FTP's, and F3 views a file of it: the server's
    # root is the system's, as a chroot's process has no sockets
    term.send("cd ftp://127.0.0.1:2121/run/ftp\r")
    term.until(lambda text: any(line[1:17].strip() == "remote.txt" for line in text.splitlines()[3:10]), "the FTP server's directory")
    term.key("down")
    status(term, "remote.txt", "the bar on the file of the FTP server")
    term.key("f3")
    term.until(lambda text: "3Quit" in text and term.row(1) == "over ftp", "the file of the FTP server in the viewer")
    term.key("f10")
    term.until("10Quit", "the panels after the viewer of the FTP server's file")
    term.send("cd /run/work\r")
    term.until(lambda text: "~/work #" in text and "<─ ~/work ─" in term.row(1), "the first panel's directory again")

    # ^O shows the subshell: a shell of mc's on a terminal of its own
    term.send("\x0f")
    term.until(lambda text: "10Quit" not in text, "the subshell's screen")
    term.send("cat typed; tty; echo over\r")
    term.until(lambda text: re.search(r"^over$", text, re.M), "the subshell's command")
    text = term.screen.text()
    assert "from-mc\n" in text and re.search(r"^/dev/pts/[1-9]\d*$", text, re.M), text
    term.send("\x0f")
    term.until("10Quit", "the panels after the subshell")

    # a window of another size
    term.resize(30, 100)
    term.until(lambda text: term.row(29).startswith(" 1Help") and "10Quit" in term.row(29), "the keys on the last line")
    text = term.screen.text()
    assert len(term.row(1)) == 100 and term.row(1).endswith("┐"), text

    # F10 leaves: the screen is the shell's again
    term.key("f10")
    term.until(lambda text: "10Quit" not in text and prompt(text), "the shell after mc")
    assert 1049 not in term.screen.modes and 1002 not in term.screen.modes, sorted(term.screen.modes)

    # the files are as mc left them
    term.send("clear; cd /run; cat work/a.txt other/a.txt work/typed; ls -d work/made; echo status $?\r")
    term.until("status 0", "the files after mc")
    text = term.screen.text()
    assert text.startswith("XYZ hello mc\nsecond line\nXYZ hello mc\nsecond line\nfrom-mc\nwork/made\nstatus 0\n"), text

    # mcview and mcedit are mc by other names
    term.send("clear; mcview work/a.txt\r")
    term.until("3Quit", "mcview")
    assert term.row(1) == "XYZ hello mc", term.screen.text()
    term.key("f10")
    term.until(lambda text: "3Quit" not in text and prompt(text), "the shell after mcview")
    term.send("clear; mcedit work/a.txt\r")
    term.until("2Save", "mcedit")
    assert term.row(1) == "XYZ hello mc", term.screen.text()
    term.key("f10")
    term.until(lambda text: "2Save" not in text and prompt(text), "the shell after mcedit")

    if (code := term.close()) != 7:
        raise AssertionError(f"vm's status is {code}, not the shell's 7")


def vt100(vm, rootfs, image):
    """A VT100, by its other name, a link in the database: mc draws
    without colours and takes the keys of one, a function key as Esc and
    its digit."""
    term = Terminal(vm, rootfs, image, kind="vt100-am")
    term.send(SETUP)
    term.until(lambda text: text.strip() == "~ #", "the files of the test")
    term.send("echo $TERM; mc /run/work /run/other\r")
    term.until("10Quit", "mc's keys line")
    text = term.screen.text()
    assert term.row(0).split() == ["Left", "File", "Command", "Options", "Right"], text
    assert [line[1:17].strip() for line in text.splitlines()[3:8]] == ["/..", "/sub", "a.txt", "t.tar", ""], text
    assert 1 in term.screen.modes, f"the application's cursor keys: {sorted(term.screen.modes)}"
    term.key("down")
    term.key("down")
    status(term, "a.txt", "the bar on a.txt")
    term.send("\x1b3")
    term.until("3Quit", "the viewer")
    assert term.row(1) == "hello mc", term.screen.text()
    term.send("\x1b0")
    term.until("10Quit", "the panels after the viewer")
    term.send("\x1b0")
    term.until(lambda text: "10Quit" not in text and prompt(text), "the shell after mc")
    term.send("clear; echo $TERM\r")
    term.until("vt100-am\n", "the terminal's type")

    if (code := term.close()) != 7:
        raise AssertionError(f"vm's status is {code}, not the shell's 7")


def trees(vm, rootfs, image):
    """A panel's tree and a dialog's: the dialog's closing leaves the
    panel's tree told of what the tree store removes. The kernel checks
    every access of memory here, so a freed entry read is mc's end."""
    term = Terminal(vm, rootfs, image, flags=["--asan"])
    term.send(SETUP)
    term.until(lambda text: text.strip() == "~ #", "the files of the test")
    term.send("mc /run/work /run/other\r")
    term.until("10Quit", "mc's keys line")

    # the left panel is a tree
    term.key("f9")
    term.send("\r")
    term.until("Listing format...", "the menu of the left panel")
    term.send("t")
    term.until(lambda text: "Directory tree" in term.row(1) and "3Forget" in term.row(23), "the tree in the panel")

    # the Directory tree dialog, another tree, opens and closes
    term.key("f9")
    term.send("c")
    term.until("Directory tree", "the Command menu")
    term.send("d")
    term.until(lambda text: text.count("Directory tree") == 2, "the dialog of the tree")
    term.send("\x1b\x1b")
    term.until(lambda text: text.count("Directory tree") == 1, "the dialog closed")

    # the root's directories, the bar on the first, and it is forgotten
    term.key("f2")
    term.until("─ dev", "the root's directories in the tree")
    term.key("down")
    status(term, "/dev", "the bar on /dev")
    term.key("f3")
    term.until(lambda text: "─ dev" not in text, "the directory forgotten")
    status(term, "/pzd", "the bar on the next directory")

    # the tree is drawn again: after a menu, and as the bar moves
    term.key("f9")
    term.send("\x1b\x1b")
    term.key("down")
    status(term, "/run", "the bar on /run")
    term.key("up")
    status(term, "/pzd", "the bar on /pzd again")
    text = term.screen.text()
    assert "asan" not in text and "3Forget" in term.row(23), text
    term.key("f10")
    term.until(lambda text: "3Forget" not in text and prompt(text), "the shell after mc")

    if (code := term.close()) != 7:
        raise AssertionError(f"vm's status is {code}, not the shell's 7")


def main():
    vm, rootfs, image = sys.argv[1:]
    xterm(vm, rootfs, image)
    vt100(vm, rootfs, image)
    trees(vm, rootfs, image)
    print("mc: panels, keys, mouse, help, viewer, editor, copy, archive, menu, subshell, FTP and resize on an xterm; a VT100's keys on one; "
          "a tree's forgotten directory under the checks of memory")


if __name__ == "__main__":
    main()
