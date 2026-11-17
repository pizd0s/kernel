#!/usr/bin/env python3
"""The terminfo database of the system: descriptions of terminals compiled
from ncurses' source of them, as its tic compiles them.

    terminfo.py <ncurses.tar.gz> <terminfo.tar> <name>...

The tarball's misc/terminfo.src describes the terminals and its
include/Caps names the capabilities of the compiled format, a boolean's,
a number's and a string's place there the order Caps lists them in. Each
name is compiled with the descriptions it uses, a capability of its own
before those of what it uses and of an earlier use before a later one's, a
cancelled one gone; the archive has it as terminfo/<first letter>/<name>,
under each of its names but the last, its description, a terminfo
directory's layout.

The compiled format is the one every reader takes: a header of six 16-bit
numbers, the names, a byte for each boolean, the numbers and the offsets
of the strings, 16 bits each, and the strings; with a number past 32767,
the format of 32-bit numbers ncurses 6.1 began. Capabilities outside
Caps, a terminal's own extensions, are left out.
"""
import re
import struct
import sys
import tarfile
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

import wasm  # noqa: E402

MAGIC, MAGIC32 = 0o432, 0o1036
CANCELLED = None
ESCAPES = {"E": 27, "e": 27, "n": 10, "l": 10, "r": 13, "t": 9, "b": 8, "f": 12, "s": 32, "a": 7}


def capabilities(text):
    """The places of Caps' capabilities: {name: (kind, index)}."""
    places, counts = {}, {"bool": 0, "num": 0, "str": 0}

    for line in text.splitlines():
        fields = line.split()

        if line.startswith("#") or len(fields) < 3 or fields[2] not in counts:
            continue

        places[fields[1]] = (fields[2], counts[fields[2]])
        counts[fields[2]] += 1

    return places


def string(text):
    """The bytes of a string capability's source."""
    out = bytearray()
    i = 0

    while i < len(text):
        c = text[i]
        i += 1

        if c == "^" and i < len(text):
            out.append(127 if text[i] == "?" else ord(text[i]) & 0x1F)
            i += 1
        elif c == "\\" and i < len(text):
            c = text[i]
            i += 1

            if c in "01234567":
                digits = c

                while i < len(text) and len(digits) < 3 and text[i] in "01234567":
                    digits += text[i]
                    i += 1

                # a NUL would end the string: 0200 stands for it
                out.append(int(digits, 8) & 0xFF or 0x80)
            else:
                out.append(ESCAPES.get(c, ord(c)))
        else:
            out += c.encode()

    return bytes(out)


def fields(body):
    """The fields of an entry: comma-separated, a comma after a backslash or a
    caret a string's own."""
    out, current, i = [], "", 0

    while i < len(body):
        c = body[i]

        if c in "\\^" and i + 1 < len(body):
            current += c + body[i + 1]
            i += 2
        elif c == ",":
            out.append(current.strip())
            current = ""
            i += 1
        else:
            current += c
            i += 1

    return [field for field in out if field]


def entries(text):
    """The entries of terminfo.src: {name: (names, capabilities, uses)}. An
    entry's lines after the first begin with white space, which is not of
    it: a long string goes on from one line to the next."""
    blocks, current = [], None

    for line in text.splitlines():
        if not line or line.startswith("#"):
            continue

        if line[0] in " \t":
            if current is not None:
                current.append(line.strip())
        else:
            current = [line.strip()]
            blocks.append(current)

    found = {}

    for block in blocks:
        parts = fields("".join(block))
        names = parts[0].split("|")
        caps, uses = {}, []

        for field in parts[1:]:
            if field.startswith("use="):
                uses.append(field[4:])
            elif field.endswith("@"):
                caps[field[:-1]] = CANCELLED
            elif (match := re.match(r"([A-Za-z0-9_.+-]+)#(.+)$", field)) and "=" not in match[1]:
                caps[match[1]] = int(match[2], 0) if not re.fullmatch(r"0[0-7]+", match[2]) else int(match[2], 8)
            elif "=" in field:
                name, value = field.split("=", 1)
                caps[name] = string(value)
            else:
                caps[field] = True

        entry = (names, caps, uses)

        for name in names[:-1] if len(names) > 1 else names:
            found.setdefault(name, entry)

    return found


def resolved(name, source, seen=()):
    """An entry's capabilities with those of what it uses."""
    if name in seen:
        raise SystemExit(f"terminfo: {name} uses itself")

    if name not in source:
        raise SystemExit(f"terminfo: no description of {name}")

    _, own, uses = source[name]
    caps = {}

    for use in reversed(uses):
        caps.update(resolved(use, source, (*seen, name)))

    caps.update(own)

    return caps


def compiled(names, caps, places):
    """The compiled description of a terminal."""
    booleans, numbers, strings = {}, {}, {}

    for name, value in caps.items():
        if value is CANCELLED or name not in places:
            continue

        kind, index = places[name]

        if kind == "bool" and value is True:
            booleans[index] = 1
        elif kind == "num" and isinstance(value, int) and value is not True:
            numbers[index] = value
        elif kind == "str" and isinstance(value, bytes):
            strings[index] = value

    wide = any(value > 32767 for value in numbers.values())
    number = "<i" if wide else "<h"
    title = "|".join(names).encode() + b"\0"
    flags = bytes(booleans.get(i, 0) for i in range(max(booleans, default=-1) + 1))
    values = b"".join(struct.pack(number, numbers.get(i, -1)) for i in range(max(numbers, default=-1) + 1))
    table, offsets = b"", b""

    for i in range(max(strings, default=-1) + 1):
        if i in strings:
            offsets += struct.pack("<h", len(table))
            table += strings[i] + b"\0"
        else:
            offsets += struct.pack("<h", -1)

    header = struct.pack("<6H", MAGIC32 if wide else MAGIC, len(title), len(flags), len(values) // struct.calcsize(number),
                         len(offsets) // 2, len(table))
    pad = b"\0" * ((len(title) + len(flags)) % 2)

    return header + title + flags + pad + values + offsets + table


def member(archive, name):
    return archive.extractfile(next(m for m in archive.getmembers() if m.name.endswith(name))).read().decode()


def main():
    if len(sys.argv) < 4:
        raise SystemExit(__doc__)

    tarball, output, wanted = sys.argv[1], sys.argv[2], sys.argv[3:]

    with tarfile.open(tarball) as archive:
        places = capabilities(member(archive, "/include/Caps"))
        source = entries(member(archive, "/misc/terminfo.src"))

    with tempfile.TemporaryDirectory(dir=Path(output).parent, prefix="terminfo-") as work:
        root = Path(work) / "terminfo"

        for name in wanted:
            names = source[name][0] if name in source else [name]
            data = compiled(names, resolved(name, source), places)

            for alias in names[:-1] if len(names) > 1 else names:
                path = root / alias[0] / alias
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(data)

        wasm.write_tar(output, Path(work), root)


if __name__ == "__main__":
    main()
