#!/usr/bin/env python3
"""Build Midnight Commander for the system, from its release tarball, on
the libraries of lib/: GLib (lib/glib), its utility library, with PCRE2
(lib/pcre2) under GLib's regular expressions, and S-Lang (lib/slang), its
screen.

    make.py <clang> <wasm-ld> <libc-include> <libc.a> <mc.tar.xz> <glib include.tar> <libglib.a> <libpcre2.a>
            <slang include.tar> <libslang.a> <mc.wasm> <mc.tar> <prefix>

What mc's configure would make is made here: config.h is ours
(mc-config.h). Of mc, the file manager with the editor, the viewers, the
subshell and the virtual file systems of local archives and FTP's. clang
compiles it for wasm32 against lib/c's headers and the libraries' and
wasm-ld links it with the libraries and libc.a.

mc is patched as it is unpacked, by the patches here, each one's reason
at its head: errors of its own, which its authors have too. It calls
functions through pointers of other types, which C leaves undefined and
WebAssembly traps on (mc-function-types.patch), and a tree of mc's loses
its word of a removed entry when another tree closes (mc-tree-hook.patch).
A patch applies to the lines it names, exactly, or the build stops.

mc.tar is what mc reads as it runs, as its make install lays it out: its
members etc/mc, share/mc and libexec/mc are those of the prefix, mc's
directory of the store (dev/pkg.py), where mc looks for them. The
templates among them get those paths. Its help, share/mc/help, is mc.hlp
of the manual and mc.hlp.<language> of each translation of it, which mc
takes by the locale's language: man2hlp.py makes them as mc's own
man2hlp does, a Perl script, of the manuals mc's build makes.
"""
import io
import os
import re
import shutil
import subprocess
import sys
import tarfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
ROOT = HERE.parent.parent
sys.path.insert(0, str(ROOT / "dev"))
sys.path.insert(0, str(HERE))

import man2hlp  # noqa: E402
import wasm  # noqa: E402

CFLAGS = ["-O2", "-w", "-DHAVE_CONFIG_H"]

LINK = ["--stack-first", "--max-memory=4294967296", "--export=__heap_base",
        "--export=__stack_pointer", "--gc-sections"]

PATCHES = ["mc-function-types.patch", "mc-tree-hook.patch"]

# What of mc's lib and src is not built: the other screen library, X11,
# the file systems of the network over programs the system has not, ssh
# (shell) and libssh2 (sftpfs), ext2's attributes and undelete, the
# speller, the log of a maintainer's build, programs of their own.
MC_LEFT = ["lib/logging.c", "lib/tty/color-ncurses.c", "lib/tty/tty-ncurses.c", "lib/tty/x11conn.c", "src/editor/spell.c",
           "src/filemanager/chattr.c", "src/consaver/", "src/man2hlp/", "src/vfs/sftpfs/", "src/vfs/shell/", "src/vfs/undelfs/"]


def mc_flags(prefix):
    return ["-Dunix", f'-DSYSCONFDIR="{prefix}/etc/mc/"', f'-DLIBEXECDIR="{prefix}/libexec/mc/"', f'-DDATADIR="{prefix}/share/mc/"',
            f'-DLOCALEDIR="{prefix}/share/locale"', f'-DEXTHELPERSDIR="{prefix}/libexec/mc/ext.d/"']


# The values of mc's templates: its paths under the prefix, and the
# programs its scripts run, by the names the system has them under; main
# fills in the paths.
VALUES = {
    "PACKAGE": "mc", "AWK": "awk", "PERL": "/usr/bin/env perl", "PYTHON": "/usr/bin/python3", "RUBY": "/usr/bin/env ruby",
    "ZIP": "/usr/bin/zip", "UNZIP": "/usr/bin/unzip", "HAVE_ZIPINFO": "0", "MANDOC": "-man", "MAN_FLAGS": "",
    "ENCODING_ISO8859": "ISO-8859", "ENCODING_CP1251": "CP1251", "ENCODING_CP866": "CP866",
}


def mc(tree, work, system, libraries, prefix):
    gen = work / "gen/mc"
    gen.mkdir(parents=True)
    shutil.copy(HERE / "mc-config.h", gen / "config.h")
    # C23's checked arithmetic, which lib/c has no header of: mc's own, made
    # where its configure makes it, with GLib's boolean.
    checked = (tree / "lib/stdckdint.in.h").read_text().splitlines(keepends=True)
    (tree / "lib/stdckdint.h").write_text("".join(line.replace("bool", "gboolean", 1) for line in checked))
    sources = sorted(source for directory in ("lib", "src") for source in (tree / directory).rglob("*.c")
                     if not any(str(source.relative_to(tree)).startswith(left) for left in MC_LEFT))
    flags = [*CFLAGS, *mc_flags(prefix), "-I", str(gen), "-I", str(tree), "-I", str(libraries), *system]

    return [(source, work / "o/mc" / f"{i}-{source.stem}.o", flags) for i, source in enumerate(sources)]


# The languages of the manual's translations, configure's ALL_DOC_LINGUAS.
DOC_LINGUAS = ["es", "hu", "it", "pl", "ru", "sr"]


def manual(tree, language, prefix):
    """mc.1 of a language as mc's build makes it of mc.1.in: its paths the
    prefix's. The date and version are .TH's, which the help leaves out."""
    text = (tree / "doc/man" / language / "mc.1.in").read_text(encoding="latin-1")
    version = re.search(r"mc-([\d.]+)$", tree.name)[1]
    paths = {"%DATE_OF_MAN_PAGE%": "", "%MAN_VERSION%": version, "%sysconfdir%": f"{prefix}/etc",
             "%libexecdir%": f"{prefix}/libexec", "%pkglibexecdir%": f"{prefix}/libexec/mc", "%pkgdatadir%": f"{prefix}/share/mc"}

    for name, value in paths.items():
        text = text.replace(name, value)

    return text


def helps(tree, prefix):
    """share/mc/help's files: each manual's help, its template the
    language's xnc.hlp."""
    found = {}

    for language in ["", *DOC_LINGUAS]:
        template = (tree / "doc/hlp" / language / "xnc.hlp").read_text(encoding="latin-1")
        text = man2hlp.convert(manual(tree, language, prefix), template)
        found["mc.hlp" + (f".{language}" if language else "")] = (text.encode("latin-1"), 0o644)

    return found


def filled(path):
    """A template with the system's values; a name that is not one of them
    is the text's own."""
    return re.sub(r"@(\w+)@", lambda m: VALUES.get(m[1], m[0]), path.read_text()).encode()


def installed(source, mode, only=None, skip=()):
    """The files of a directory as they are installed, {name: (bytes,
    mode)}: a template filled, under its name without .in; mc's build files
    left out, and of the rest those only names, or all but those skip
    names."""
    found = {}

    for path in sorted(source.iterdir()):
        name = path.name[:-3] if path.name.endswith(".in") else path.name

        if path.is_file() and not path.name.startswith("Makefile") and name not in skip and (only is None or name in only):
            found[name] = (filled(path) if path.name.endswith(".in") else path.read_bytes(), mode(name))

    return found


# misc's files of etc/mc; its others are share/mc's.
ETC = ["edit.indent.rc", "filehighlight.ini", "mc.default.keymap", "mc.emacs.keymap", "mc.ext.ini", "mc.menu", "mc.vim.keymap",
       "mcedit.menu"]


def data(tree, output, prefix):
    """mc.tar: what make install puts under the prefix's etc/mc, share/mc
    and libexec/mc."""
    misc = tree / "misc"
    script, plain = (lambda name: 0o755), (lambda name: 0o644)
    trees = {
        "etc/mc": {**installed(misc, lambda name: 0o755 if name == "edit.indent.rc" else 0o644, only=ETC),
                   **installed(tree / "src/vfs/sfs", plain, only=["sfs.ini"])},
        "share/mc": installed(misc, plain, only=["mc.lib", "mc.charsets"]),
        "share/mc/skins": installed(misc / "skins", plain),
        "share/mc/syntax": installed(misc / "syntax", plain),
        "share/mc/hints": installed(tree / "doc/hints", plain),
        "share/mc/help": helps(tree, prefix),
        "share/mc/examples/macros.d": installed(misc / "macros.d", plain),
        "libexec/mc": installed(tree / "contrib", script, skip=["README.xterm"]),
        "libexec/mc/ext.d": installed(misc / "ext.d", script),
        "libexec/mc/extfs.d": installed(tree / "src/vfs/extfs/helpers", lambda name: 0o644 if name.startswith("README") else 0o755),
    }
    links = {"etc/mc/mc.keymap": "mc.default.keymap"}
    Path(output).parent.mkdir(parents=True, exist_ok=True)

    def entry(name, kind, mode):
        info = tarfile.TarInfo(name)
        info.type, info.mode, info.mtime, info.uid, info.gid, info.uname, info.gname = kind, mode, 0, 0, 0, "", ""

        return info

    with tarfile.open(output, "w", format=tarfile.PAX_FORMAT) as archive:
        made = set()

        for where in sorted(trees):
            parts = where.split("/")

            for depth in range(1, len(parts) + 1):
                parent = "/".join(parts[:depth])

                if parent not in made:
                    made.add(parent)
                    archive.addfile(entry(parent, tarfile.DIRTYPE, 0o755))

            for name, (content, mode) in sorted(trees[where].items()):
                info = entry(f"{where}/{name}", tarfile.REGTYPE, mode)
                info.size = len(content)
                archive.addfile(info, io.BytesIO(content))

            for name, target in sorted(links.items()):
                if name.rsplit("/", 1)[0] == where:
                    info = entry(name, tarfile.SYMTYPE, 0o777)
                    info.linkname = target
                    archive.addfile(info)


def main():
    if len(sys.argv) != 14:
        raise SystemExit(__doc__)

    cc, ld, include, libc, tarball, glib_include, libglib, libpcre2, slang_include, libslang = sys.argv[1:11]
    output, share, prefix = Path(sys.argv[11]), sys.argv[12], sys.argv[13]
    VALUES.update({"bindir": f"{prefix}/bin", "datarootdir": f"{prefix}/share", "pkglibexecdir": f"{prefix}/libexec/mc",
                   "EXTHELPERSDIR": f"{prefix}/libexec/mc/ext.d", "EXTFSHELPERSDIR": f"{prefix}/libexec/mc/extfs.d"})
    include = str(Path(include).resolve())
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "mc"
    shutil.rmtree(work, ignore_errors=True)
    tree = wasm.unpack(tarball, work / "src")

    for patch in PATCHES:
        wasm.patched(tree, HERE / patch)

    for include_tar in (glib_include, slang_include):
        with tarfile.open(include_tar) as archive:
            archive.extractall(work / "libraries", filter="data")

    built = mc(tree, work, ["-isystem", include], work / "libraries" / "include", prefix)
    wasm.compile_all(cc, built)
    output.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run([ld, *LINK, "-o", str(output), *(str(obj) for _, obj, _ in built), libglib, libpcre2, libslang, libc], check=True)
    data(tree, share, prefix)
    shutil.rmtree(work)


if __name__ == "__main__":
    main()
