#!/usr/bin/env python3
"""The terminfo database of the system (dev/terminfo.py) is what ncurses'
own compiler, tic, makes of ncurses' source of the terminals, byte for
byte: every name has its description, in the directory of its first
letter. And a source of our own with what tic's reading has that a plain
one has not is compiled as tic compiles it: numbers of other bases and a
number past 16 bits; ^ and \\ in strings, ^ after % a character of its
own; a string that ends with @, one that goes on in the next line, one
without its =; a capability commented out, one by another name and one
by its variable's; termcap's obsolete capabilities, read and not
written; %{65} written as %'A'; a description's own capability before
those of what it uses, an earlier use's before a later one's, what it
cancels cancelled in it and absent in what uses it; the VT100's map of
the alternate character set where there are the set's modes and no map;
AIX's box characters as a map.

    test_terminfo.py <ncurses.tar.gz> <terminfo.tar>
    test_terminfo.py --tic <tic> <ncurses.tar.gz>

TIC is what tic answers, the digests of what it compiles of ncurses'
source and of ours: `test_terminfo.py --tic` prints it, with the tic of
the tarball's ncurses.
"""
import hashlib
import struct
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "dev"))

import terminfo  # noqa: E402

SOURCE = r"""# a comment
base|the base of the others,
	am, OTbs, cols#80, lines#0x18, it#010,
	bel=^G, cr=\r, cup=\E[%i%p1%d;%p2%dH, sgr0=\E[m\017,
	smacs=^N, rmacs=^O,
	kf1=\EOP, kf2@, ind=\n,
other|another, with numbers past 16 bits,
	xenl, colors#0x10000, kf1=\E[11~, kf2=\E[12~, .kf3=\E[13~,
	u6=\E[%i%d;%dR, ich=\E[%p1%d@, rep=%p1%c\E[%p2%{1}%-%db,
	setaf=\E[3%p1%{65}%+%c%{8}%^m, kbs=^?, khome=\200\0x, kend=a\,b\:c\sd,
	smso=\E[7m
	, rmso=\E[27m,
uses|u1|a terminal that uses both,
	cols#132, kf3=\E[13~, am@,
	use=base, use=other,
later|a terminal that uses what cancels,
	use=uses, use=other,
boxed|AIX's box characters,
	box1=lqkxjmwuvtn, font0=\E(B, auto_right_margin,
	smul,
"""

TIC = {
    "database": "2899 descriptions, sha256 b2ef82c932b172c234fb9f63737fdb720dbfe93e759f124c34864b27cb0f1bb1",
    "source": "6 descriptions, sha256 4ef277e3eae9471e8b30a7dfa769accd3a14f2100cbecf66c3b0f13282a6fa1f",
}


def digest(files):
    """The digest of compiled descriptions by name."""
    h = hashlib.sha256()

    for name in sorted(files):
        h.update(name.encode() + b"\0" + struct.pack("<I", len(files[name])) + files[name])

    return f"{len(files)} descriptions, sha256 {h.hexdigest()}"


def read(data, caps):
    """The capabilities of a compiled description by name, as a reader of
    the format finds them: True, a number, a string's bytes, and
    terminfo.CANCELLED; the 32-bit format's under the name "wide"."""
    magic, names, booleans, numbers, strings, table = struct.unpack_from("<6H", data)
    assert magic in (terminfo.MAGIC, terminfo.MAGIC32), magic
    wide = magic == terminfo.MAGIC32
    at = 12 + names
    flags = data[at:at + booleans]
    at += booleans + (names + booleans) % 2
    values = struct.unpack_from(f"<{numbers}{'i' if wide else 'h'}", data, at)
    at += numbers * (4 if wide else 2)
    offsets = struct.unpack_from(f"<{strings}h", data, at)
    at += strings * 2
    assert len(data) == at + table and data[12 + names - 1] == 0, "the size of the description"
    found = {"wide": True} if wide else {}

    for name, (kind, index) in caps.places.items():
        if kind == terminfo.BOOLEAN and index < booleans and flags[index]:
            found[name] = True
        elif kind == terminfo.NUMBER and index < numbers and values[index] != terminfo.ABSENT:
            found[name] = values[index]
        elif kind == terminfo.STRING and index < strings and offsets[index] != terminfo.ABSENT:
            start = at + offsets[index]
            found[name] = terminfo.CANCELLED if offsets[index] == terminfo.CANCELLED else data[start:data.index(b"\0", start)]

    return found


def tic(program, tarball):
    """TIC, of the tic at program."""
    _, text = terminfo.ncurses(tarball)
    out = {}

    with tempfile.TemporaryDirectory(prefix="pizdos-terminfo-") as work:
        for name, source in ("database", text), ("source", SOURCE):
            (Path(work) / name).mkdir()
            (Path(work) / (name + ".src")).write_text(source, encoding="latin-1")
            subprocess.run([program, "-o", str(Path(work) / name), str(Path(work) / (name + ".src"))], check=True, capture_output=True)
            out[name] = digest({path.name: path.read_bytes() for path in (Path(work) / name).glob("*/*")})

    return out


def main():
    if sys.argv[1:2] == ["--tic"]:
        for name, value in tic(*sys.argv[2:]).items():
            print(f'    "{name}": "{value}",')

        return

    tarball, database = sys.argv[1:]
    caps, _ = terminfo.ncurses(tarball)

    with tarfile.open(database) as archive:
        files = {m.name: archive.extractfile(m).read() for m in archive.getmembers() if m.isfile()}

    assert all(path == f"terminfo/{path.rsplit('/', 1)[1][0]}/{path.rsplit('/', 1)[1]}" for path in files), "a terminfo directory's layout"
    assert digest({path.rsplit("/", 1)[1]: data for path, data in files.items()}) == TIC["database"], "the database is not tic's"
    xterm = read(files["terminfo/x/xterm-256color"], caps)
    assert xterm["colors"] == 256 and xterm["kcuu1"] == b"\x1bOA" and xterm["ich"] == b"\x1b[%p1%d@", xterm

    compiled = terminfo.descriptions(SOURCE, caps)
    assert digest(compiled) == TIC["source"], "the source of the test is not compiled as tic compiles it"
    base, other, uses, later, boxed = (read(compiled[name], caps) for name in ("base", "other", "uses", "later", "boxed"))
    assert compiled["u1"] == compiled["uses"]
    assert (base["am"], base["cols"], base["lines"], base["it"]) == (True, 80, 24, 8) and "OTbs" not in base, base
    assert (base["bel"], base["cr"], base["ind"], base["sgr0"]) == (b"\x07", b"\r", b"\n", b"\x1b[m\x0f"), base
    assert base["acsc"] == terminfo.VT_ACSC and base["kf2"] is terminfo.CANCELLED, base
    assert other["wide"] and other["colors"] == 65536 and "kf3" not in other and "acsc" not in other, other
    assert (other["ich"], other["rep"]) == (b"\x1b[%p1%d@", b"%p1%c\x1b[%p2%{1}%-%db"), other
    assert (other["setaf"], other["kbs"]) == (b"\x1b[3%p1%'A'%+%c%{8}%^m", b"\x7f"), other
    assert (other["khome"], other["kend"]) == (b"\x80\x80x", b"a,b:c d"), other
    assert (other["smso"], other["rmso"]) == (b"\x1b[7m", b"\x1b[27m"), other
    assert uses["cols"] == 132 and uses["kf1"] == base["kf1"] and uses["kf3"] == b"\x1b[13~" and uses["xenl"], uses
    assert "am" not in uses and "kf2" not in uses, uses
    assert later["kf2"] == other["kf2"] and "am" not in later and later["cols"] == 132, later
    assert (boxed["acsc"], boxed["s0ds"], boxed["am"], boxed["smul"]) == (b"llqqkkxxjjmmwwuuvvttnn", b"\x1b(B", True, b"") and "box1" not in boxed, boxed
    print(f"terminfo: {TIC['database'].split(',')[0]} and the test's own source are compiled as tic compiles them")


if __name__ == "__main__":
    main()
