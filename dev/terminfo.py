#!/usr/bin/env python3
"""The terminfo database of the system: the descriptions of terminals
ncurses has, compiled from its source of them byte for byte as its tic
compiles them.

    terminfo.py <ncurses.tar.gz> <terminfo.tar>

The tarball's misc/terminfo.src describes the terminals; its include/Caps
names the capabilities of the compiled format, a boolean's, a number's
and a string's place there the order Caps lists them in, those after its
mark of the end, termcap's obsolete ones, read and never written; its
include/Caps-ncurses has other names of some. Every description is
compiled with those it uses, and the archive has it as
terminfo/<first letter>/<name> under its first name; each other name of
it but the last, its title, is a symbolic link to that file, as a tic
built to link by name makes them (--enable-symlinks): a terminfo
directory's layout.

The source is read as tic reads it (ncurses/tinfo/comp_scan.c,
parse_entry.c): a line that begins with # is a comment and the white space
a line begins with is not there; a description begins in the first column
with its names, and its capabilities follow, each to a comma: a name alone
a boolean, name#number, name=string, name@ the capability cancelled, a
name after a period commented out, use=name another description's
capabilities. What a description uses is merged as tic merges
(comp_parse.c, alloc_entry.c): its own capability before those of what it
uses and of an earlier use before a later one's; what it cancels stays
cancelled in it, and is absent in a description that uses it, whatever a
later use of that one has. A description with the alternate character
set's modes and no map of it gets the VT100's, and AIX's box characters
become a map.

The compiled format is the one every reader takes (write_entry.c): a
header of six 16-bit numbers, the names, a byte for each boolean, the
numbers and the offsets of the strings, 16 bits each, and the strings;
with a number past 32767, the format of 32-bit numbers ncurses 6.1 began.
An absent number or string is -1 and a cancelled one -2. Capabilities
outside Caps, a terminal's own extensions, are left out, as tic leaves
them without -x.
"""
import io
import re
import struct
import sys
import tarfile
from pathlib import Path

MAGIC, MAGIC32 = 0o432, 0o1036
ABSENT, CANCELLED = -1, -2
LARGEST = 0x7FFFFFFF
NAMES, BOOLEAN, NUMBER, STRING, CANCEL, UNDEFINED, END = "names", "bool", "num", "str", "cancel", "undefined", "end"
ALNUM = set("0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
ESCAPES = {"E": 27, "n": 10, "r": 13, "b": 8, "f": 12, "t": 9, "a": 7, "e": 27, "l": 10, "s": 32}
# The VT100's map of the alternate character set.
VT_ACSC = b"``aaffggiijjkkllmmnnooppqqrrssttuuvvwwxxyyzz{{||}}~~"
# AIX's box characters, in box1's order, as the alternate set's.
BOX = "lqkxjmwuvtn"


class Capabilities:
    """The capabilities of the compiled format: the place of each, a kind
    and an index, by its name, by the name of its variable and by its
    other names, and how many of each kind are written."""

    def __init__(self, caps, others):
        self.places, self.variables, self.written = {}, {}, {}
        counts = {BOOLEAN: 0, NUMBER: 0, STRING: 0}

        for line in caps.splitlines():
            fields = line.split()

            if line.startswith("#"):
                if fields[1:2] == ["%%-STOP-HERE-%%"]:
                    self.written = dict(counts)

                continue

            if len(fields) < 3 or fields[2] not in counts:
                continue

            if fields[1] in self.places:
                raise SystemExit(f"terminfo: {fields[1]} is two capabilities")

            self.places[fields[1]] = self.variables[fields[0]] = (fields[2], counts[fields[2]])
            counts[fields[2]] += 1

        if not self.written:
            raise SystemExit("terminfo: Caps has no mark of the capabilities written")

        self.others = {}

        for line in others.splitlines():
            fields = line.split()

            if fields[:1] == ["infoalias"]:
                self.others[fields[1]] = fields[2]

    def place(self, name):
        """The place of the capability a source names, or None: no
        capability of the format, or one a source is to leave out."""
        if name in self.places:
            return self.places[name]

        if name in self.others:
            return self.places.get(self.others[name])

        return self.variables.get(name)


class Source:
    """The characters of a source and its tokens."""

    def __init__(self, text):
        self.lines = []

        for line in text.split("\n"):
            if not line.startswith("#"):
                line = line.removesuffix("\r")
                self.lines.append((line.lstrip(" \t") + "\n", line[:1] in (" ", "\t")))

        self.line, self.at = 0, 0
        self.first = False

    def next(self):
        """The next character, None at the end; first says whether it is
        in the first column of its line."""
        if self.line == len(self.lines):
            return None

        text, indented = self.lines[self.line]
        c = text[self.at]
        self.first = self.at == 0 and not indented
        self.at += 1

        if self.at == len(text):
            self.line, self.at = self.line + 1, 0

        return c

    def back(self):
        if self.at == 0:
            self.line -= 1
            self.at = len(self.lines[self.line][0])

        self.at -= 1

    def rest(self):
        """What is left of the line of the character before, without the
        white space it ends with."""
        return self.lines[self.line][0][self.at:].rstrip() if self.at else ""

    def escaped(self, c):
        """c, or what follows a backslash and the white space after it."""
        if c == "\\":
            c = self.next()

            while c is not None and c in "\n \t":
                c = self.next()

        return c

    def skip(self):
        """Skips to after the next comma."""
        while self.next() not in (",", None):
            pass

    def names(self, c, caps):
        """The names of a description, which begin with c: the line to its
        end, or to a comma a capability follows."""
        out = c
        bar = comma = False

        while (c := self.next()) != "\n":
            if c is None:
                raise SystemExit(f"terminfo: {out}: the source ends in its names")

            rest = self.rest()

            if c == "|":
                bar = True
            elif c == ":" and rest[-1:] != ",":
                raise SystemExit(f"terminfo: {out}: a termcap description, not a terminfo one")
            elif c == ",":
                comma = True

                if not bar:
                    break

                # a comma may be the title's own: it ends the names when a
                # capability follows it, unless the line ends as termcap's
                after = rest.lstrip()

                if rest[-2:-1] != ":" and rest[-1:] not in ("\\", ":") and after[:1].islower() and after[:1] in ALNUM:
                    word = len(after) - len(after.lstrip("".join(ALNUM)))

                    if after[word:word + 1] in ("#", "=", "@") or after[word:word + 1] == "," and after[:word] in caps.places:
                        break
            else:
                c = self.escaped(c)

            out += c

        if not comma:
            raise SystemExit(f"terminfo: {out}: no terminfo description")

        return out.rstrip(" \t,")

    def string(self):
        """The bytes of a string capability, to its comma."""
        out = bytearray()
        last = ""

        while (c := self.next()) != ",":
            if c is None:
                break

            if c == "^" and last != "%":
                c = self.next()

                if c is None:
                    raise SystemExit("terminfo: the source ends in a string")

                out.append(127 if c == "?" else ord(c) & 0x1F or 0x80)
                c = ""
            elif c == "\\":
                c = self.next()

                if c is None:
                    raise SystemExit("terminfo: the source ends in a string")

                if c in "01234567":
                    number = int(c)

                    for _ in range(2):
                        c = self.next()

                        if c is None:
                            raise SystemExit("terminfo: the source ends in a string")

                        if c not in "0123456789":
                            self.back()

                            break

                        number = number * 8 + int(c)

                    # a NUL would end the string: 0200 stands for it
                    out.append(number & 0xFF or 0x80)
                elif c == "\n":
                    continue
                else:
                    out.append(ESCAPES.get(c, ord(c)))
            elif c == "\n":
                # the string goes on in the next line, which begins with
                # white space
                continue
            else:
                out.append(ord(c))

            if self.first:
                raise SystemExit(f"terminfo: the string {bytes(out)!r} has no comma after it")

            last = c

        return bytes(out)

    def token(self, caps):
        """The next token: its kind, its name and its value."""
        commented = False

        while True:
            c = self.next()

            while c is not None and c in "\n \t":
                c = self.next()

            c = self.escaped(c)

            if c == ".":
                commented = True
                c = self.next()

                while c is not None and c in ". \t":
                    c = self.next()

            if c is None:
                return END, None, None

            if c in ALNUM or c in "@%&*!#":
                break

            self.skip()

        if self.first:
            found = NAMES, self.names(c, caps), None
        else:
            name = c

            while (c := self.next()) is not None and (c in ALNUM or c == "_"):
                name += c

            if c is None:
                return END, None, None

            if c == ",":
                found = BOOLEAN, name, None
            elif c == "@":
                self.next()
                found = CANCEL, name, None
            elif c == "#":
                digits = ""

                while (c := self.next()) is not None and c in ALNUM:
                    digits += c

                found = NUMBER, name, min(number(digits), LARGEST)
            elif c == "=":
                found = STRING, name, self.string()
            else:
                found = UNDEFINED, name, None

        return self.token(caps) if commented else found


def number(text):
    """The number text begins with, as C's strtol reads one of base 0."""
    match = re.match(r"\s*([+-]?)(0[xX][0-9a-fA-F]+|0[0-7]*|[1-9][0-9]*)", text)

    if match is None:
        return 0

    return int(match[1] + match[2], 16 if match[2][:2] in ("0x", "0X") else 8 if match[2][0] == "0" else 10)


def valid(name):
    """Whether name may be a description's."""
    return all(" " < c <= "~" and c not in "/\\|=,:" for c in name) and "#" not in name[1:] and "@" not in name[1:]


class Entry:
    """A description: its names, its capabilities by place, a cancelled
    one CANCELLED, and the names of those it uses."""

    def __init__(self, names):
        self.names = names
        self.caps, self.uses = {}, []
        self.merged = None

    def aliases(self):
        """The names it is found by: all but the last, its title."""
        names = self.names.split("|")

        return names[:-1] if len(names) > 1 else names


def entries(text, caps):
    """The descriptions of a source, by each of their names."""
    source = Source(text)
    found = {}
    kind, name, value = source.token(caps)

    while kind != END:
        if kind != NAMES:
            raise SystemExit("terminfo: the source does not begin with a description's names")

        entry = Entry(name)
        kind, name, value = source.token(caps)

        while kind not in (END, NAMES):
            if name in ("use", "tc"):
                if kind == STRING and value and valid(value.decode("latin-1")):
                    entry.uses.append(value.decode("latin-1"))
            elif (place := caps.place(name)) is not None:
                if kind == CANCEL:
                    entry.caps[place] = CANCELLED
                elif kind == place[0]:
                    entry.caps[place] = True if kind == BOOLEAN else value
                elif kind == BOOLEAN and place[0] == STRING:
                    # a string without its = is an empty one
                    entry.caps[place] = b""

            kind, name, value = source.token(caps)

        boxed(entry, caps)

        for alias in entry.aliases():
            if alias in found:
                raise SystemExit(f"terminfo: {alias} is two descriptions")

            if "/" in alias:
                raise SystemExit(f"terminfo: {alias} is no name of a file")

            found[alias] = entry

    return found


def boxed(entry, caps):
    """AIX's box characters of a description become its map of the
    alternate character set, after what it has of one."""
    box, acsc = caps.places["box1"], caps.places["acsc"]
    chars = entry.caps.get(box)

    if not isinstance(chars, bytes):
        return

    had = entry.caps.get(acsc)
    made = (had if isinstance(had, bytes) else b"") + b"".join(bytes([ord(code), c]) for code, c in zip(BOX, chars))

    if made:
        entry.caps[acsc] = made
        del entry.caps[box]


def resolved(entry, source, seen=()):
    """A description's capabilities with those of what it uses."""
    if entry.merged is not None:
        return entry.merged

    if entry in seen:
        raise SystemExit(f"terminfo: {entry.aliases()[0]} uses itself")

    merged = dict(entry.caps)

    for use in reversed(entry.uses):
        if use not in source:
            raise SystemExit(f"terminfo: {entry.aliases()[0]} uses {use}, of which there is no description")

        for place, value in resolved(source[use], source, (*seen, entry)).items():
            if merged.get(place) is CANCELLED:
                continue

            if value is CANCELLED:
                merged.pop(place, None)
            else:
                merged[place] = value

    for place, value in entry.caps.items():
        merged[place] = value

    entry.merged = merged

    return merged


def shortened(text):
    """A string with each %{number} of a printable character the shorter
    %'character', as tic writes it."""
    if b"{" not in text:
        return text

    out = bytearray()
    i = 0

    while i < len(text):
        c = text[i]
        i += 1
        out.append(c)

        if c == ord("\\"):
            if i == len(text):
                break

            out.append(text[i])
            i += 1
        elif c == ord("%") and text[i:i + 1] == b"{":
            match = re.match(rb"\s*[+-]?(0[xX][0-9a-fA-F]+|0[0-7]*|[1-9][0-9]*)", text[i + 1:])
            value = number(match[0].decode()) if match else 0

            if match and text[i + 1 + match.end():i + 2 + match.end()] == b"}" and 32 <= value < 127 and value != ord("\\"):
                out += b"'" + bytes([value]) + b"'"
                i += 2 + match.end()

    return bytes(out) if len(out) < len(text) else text


def compiled(entry, source, caps):
    """The compiled description of a terminal."""
    values = dict(resolved(entry, source))
    acsc, smacs, rmacs = (caps.places[name] for name in ("acsc", "smacs", "rmacs"))

    if acsc not in values and isinstance(values.get(smacs), bytes) and isinstance(values.get(rmacs), bytes):
        values[acsc] = VT_ACSC

    kinds = {BOOLEAN: {}, NUMBER: {}, STRING: {}}

    for (kind, index), value in values.items():
        if index < caps.written[kind] and (kind != BOOLEAN or value is True):
            kinds[kind][index] = shortened(value) if kind == STRING and value is not CANCELLED else value

    booleans, numbers, strings = kinds[BOOLEAN], kinds[NUMBER], kinds[STRING]
    wide = any(value > 32767 for value in numbers.values())
    title = entry.names.encode("latin-1") + b"\0"
    flags = bytes(index in booleans for index in range(max(booleans, default=-1) + 1))
    packed = [struct.pack("<i" if wide else "<h", numbers.get(index, ABSENT)) for index in range(max(numbers, default=-1) + 1)]
    table, offsets = b"", b""

    for index in range(max(strings, default=-1) + 1):
        value = strings.get(index, ABSENT)

        if isinstance(value, bytes):
            offsets += struct.pack("<h", len(table))
            table += value + b"\0"
        else:
            offsets += struct.pack("<h", value)

    header = struct.pack("<6H", MAGIC32 if wide else MAGIC, len(title), len(flags), len(packed), len(offsets) // 2, len(table))
    pad = b"\0" * ((len(title) + len(flags)) % 2)

    return header + title + flags + pad + b"".join(packed) + offsets + table


def member(archive, name):
    return archive.extractfile(next(m for m in archive.getmembers() if m.name.endswith(name))).read().decode("latin-1")


def ncurses(tarball):
    """ncurses' capabilities and its source of the terminals."""
    with tarfile.open(tarball) as archive:
        return Capabilities(member(archive, "/include/Caps"), member(archive, "/include/Caps-ncurses")), member(archive, "/misc/terminfo.src")


def descriptions(text, caps):
    """The compiled descriptions of a source, by each name."""
    source = entries(text, caps)

    return {name: compiled(entry, source, caps) for name, entry in source.items()}


def database(text, caps):
    """The files of a source's database by path: a description's bytes
    under its first name, and under each other name of it the way to that
    file, a link's target."""
    source = entries(text, caps)
    files = {}

    for name, entry in source.items():
        first = entry.aliases()[0]

        if name == first:
            files[f"terminfo/{name[0]}/{name}"] = compiled(entry, source, caps)
        else:
            files[f"terminfo/{name[0]}/{name}"] = first if first[0] == name[0] else f"../{first[0]}/{first}"

    return files


def archived(output, files):
    """Writes files to an archive, a file's bytes and a link's target by
    path, with no times or owners: the same files are the same archive."""
    def entry(name, kind, mode):
        info = tarfile.TarInfo(name)
        info.type, info.mode, info.mtime, info.uid, info.gid, info.uname, info.gname = kind, mode, 0, 0, 0, "", ""

        return info

    Path(output).parent.mkdir(parents=True, exist_ok=True)

    with tarfile.open(output, "w", format=tarfile.PAX_FORMAT) as archive:
        made = set()

        for path in sorted(files):
            parts = path.split("/")

            for depth in range(1, len(parts)):
                parent = "/".join(parts[:depth])

                if parent not in made:
                    made.add(parent)
                    archive.addfile(entry(parent, tarfile.DIRTYPE, 0o755))

            if isinstance(files[path], str):
                info = entry(path, tarfile.SYMTYPE, 0o777)
                info.linkname = files[path]
                archive.addfile(info)
            else:
                info = entry(path, tarfile.REGTYPE, 0o644)
                info.size = len(files[path])
                archive.addfile(info, io.BytesIO(files[path]))


def main():
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)

    tarball, output = sys.argv[1:]
    caps, text = ncurses(tarball)
    archived(output, database(text, caps))


if __name__ == "__main__":
    main()
