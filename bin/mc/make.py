#!/usr/bin/env python3
"""Build Midnight Commander for the system, from the release tarballs of mc
and of what it stands on: GLib, its utility library, with PCRE2 under
GLib's regular expressions, and S-Lang, its screen.

    make.py <clang> <wasm-ld> <libc-include> <libc.a> <mc.tar.xz> <glib.tar.xz> <slang.tar.bz2> <pcre2.tar.bz2> <mc.wasm> <mc.tar>

What the four configures would make is made here. Each config.h is ours
(mc-config.h, glib-config.h, slang-config.h; PCRE2's is its generic one
and flags). GLib's glibconfig.h comes from its template with wasm32's
sizes and lib/c's constants, its two other built headers from its own
generator. Of S-Lang only the terminal and the screen are built, the part
mc calls, with S-Lang's own reader of the terminfo database. Of mc, the
file manager with the editor, the viewers, the subshell and the virtual
file systems of local archives; nothing of the network.

clang compiles all of it for wasm32 against lib/c's headers and wasm-ld
links it with libc.a. Threads are lib/c's one thread: GLib locks and
keeps thread-local values through it and starts no thread for what mc
asks of it.

GLib and mc are patched as they are unpacked, by the patches here, each
one's reason at its head: errors of their own, which their authors have
too. They call functions through pointers of other types, which C leaves
undefined and WebAssembly traps on (glib-function-types.patch,
mc-function-types.patch), and a tree of mc's loses its word of a removed
entry when another tree closes (mc-tree-hook.patch). A patch applies to
the lines it names, exactly, or the build stops.

mc.tar is what mc reads as it runs, as its make install lays it out: its
members etc/mc, share/mc and libexec/mc are the system's /etc/mc,
/usr/share/mc and /usr/libexec/mc. The templates among them get the
system's paths. There is no help file: mc makes it from its manual with a
perl script.
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

import wasm  # noqa: E402

CFLAGS = ["-O2", "-w", "-DHAVE_CONFIG_H"]

LINK = ["--stack-first", "-z", "stack-size=1049600", "--max-memory=4294967296", "--export=__heap_base",
        "--export=__stack_pointer", "--gc-sections"]

PCRE2 = """
    auto_possess chkdint compile compile_cgroup compile_class config context convert dfa_match error
    extuni find_bracket jit_compile maketables match match_data match_next newline ord2utf
    pattern_info script_run serialize string_utils study substitute substring tables ucd valid_utf
    xclass
""".split()
PCRE2_FLAGS = ["-DPCRE2_CODE_UNIT_WIDTH=8", "-DSUPPORT_UNICODE", "-DPCRE2_STATIC", "-DHAVE_MEMMOVE=1", "-DHAVE_STDINT_H=1",
               "-DHAVE_INTTYPES_H=1", "-DHAVE_LIMITS_H=1", "-DHAVE_STRING_H=1", "-DHAVE_STDLIB_H=1"]

# S-Lang's terminal, keyboard and screen, and what of the library they use.
SLANG = "slcommon sldisply slerr slgetkey sllower slmisc slsignal slsmg slstring sltermin slupper slutf8 slutty slwcwidth".split()

# glib/meson.build's sources of libglib for a Unix, and libcharset's.
GLIB = """
    garcbox garray gasyncqueue gatomic gbacktrace gbase64 gbitlock gbookmarkfile gbytes gcharset
    gchecksum gconvert gdataset gdate gdatetime gdatetime-private gdir genviron gerror gfileutils
    ggettext ghash ghmac ghook ghostutils giochannel giounix gkeyfile glib-init glib-private
    glib-unix glist gmain gmappedfile gmarkup gmem gmessages gnode goption gpathbuf gpattern gpoll
    gprimes gprint gprintf gqsort gquark gqueue grand grcbox grefcount grefstring gregex gscanner
    gsequence gshell gslice gslist gspawn gspawn-posix gstdio gstrfuncs gstring gstringchunk
    gstrvbuilder gtestutils gthread gthreadpool gtimer gtimezone gtrace gtranslit gtrashstack gtree
    guniprop gutf8 gunibreak gunicollate gunidecomp guri gutils guuid gvariant gvariant-core
    gvariant-parser gvariant-serialiser gvarianttypeinfo gvarianttype gversion gwakeup
    deprecated/gallocator deprecated/gcache deprecated/gcompletion deprecated/grel
    deprecated/gthread-deprecated libcharset/localcharset
""".split()
GLIB_FLAGS = ["-std=gnu11", "-DGLIB_COMPILATION", '-DG_LOG_DOMAIN="GLib"', "-DPCRE2_STATIC", '-DGLIB_CHARSETALIAS_DIR="/usr/lib"']

# glibconfig.h.in's values for wasm32: ILP32, little-endian; size_t and
# intptr_t are long.
GLIBCONFIG = {
    "gint16": "short", "gint16_modifier": '"h"', "gint16_format": '"hi"', "guint16_format": '"hu"',
    "gint32": "int", "gint32_modifier": '""', "gint32_format": '"i"', "guint32_format": '"u"',
    "glib_extension": "G_GNUC_EXTENSION ", "gint64": "long long",
    "gint64_constant": "(G_GNUC_EXTENSION (val##LL))", "guint64_constant": "(G_GNUC_EXTENSION (val##ULL))",
    "gint64_modifier": '"ll"', "gint64_format": '"lli"', "guint64_format": '"llu"',
    "glib_void_p": "4", "glib_long": "4", "glib_size_t": "4", "glib_ssize_t": "4",
    "glib_size_type_define": "long", "gsize_modifier": '"l"', "gssize_modifier": '"l"',
    "gsize_format": '"lu"', "gssize_format": '"li"', "glib_msize_type": "LONG",
    "g_pollfd_format": '"%d"', "glib_gpi_cast": "(glong)", "glib_gpui_cast": "(gulong)",
    "glib_intptr_type_define": "long", "gintptr_modifier": '"l"', "gintptr_format": '"li"', "guintptr_format": '"lu"',
    "glib_os": "#define G_OS_UNIX", "glib_vacopy": "", "g_threads_impl_def": "POSIX",
    "g_bs_native": "LE", "g_bs_alien": "BE", "glongbits": "32", "gintbits": "32", "gsizebits": "32",
    "g_byte_order": "G_LITTLE_ENDIAN", "g_module_suffix": "so", "g_pid_type": "int", "g_pid_format": '"i"',
    "g_dir_separator": "/", "g_searchpath_separator": ":",
}
GLIBCONFIG_DEFINED = {"GLIB_HAVE_ALLOCA_H": "", "GLIB_STATIC_COMPILATION": " 1", "GLIB_USING_SYSTEM_PRINTF": "", "G_ATOMIC_LOCK_FREE": ""}
# the constants of lib/c's headers glibconfig.h repeats
GLIBCONFIG_SYSTEM = {"g_pollin": "POLLIN", "g_pollout": "POLLOUT", "g_pollpri": "POLLPRI", "g_pollhup": "POLLHUP",
                     "g_pollerr": "POLLERR", "g_pollnval": "POLLNVAL", "g_af_unix": "AF_UNIX", "g_af_inet": "AF_INET",
                     "g_af_inet6": "AF_INET6", "g_msg_oob": "MSG_OOB", "g_msg_peek": "MSG_PEEK", "g_msg_dontroute": "MSG_DONTROUTE"}

# The patches of a tree, by its name before the version.
PATCHES = {"glib": ["glib-function-types.patch"], "mc": ["mc-function-types.patch", "mc-tree-hook.patch"]}

# What of mc's lib and src is not built: the other screen library, X11,
# the network and its file systems, ext2's attributes and undelete, the
# speller, the log of a maintainer's build, programs of their own.
MC_LEFT = ["lib/logging.c", "lib/tty/color-ncurses.c", "lib/tty/tty-ncurses.c", "lib/tty/x11conn.c", "lib/vfs/netutil.c",
           "src/editor/spell.c", "src/filemanager/chattr.c", "src/consaver/", "src/man2hlp/", "src/vfs/ftpfs/", "src/vfs/sftpfs/",
           "src/vfs/shell/", "src/vfs/undelfs/"]
MC_FLAGS = ["-Dunix", '-DSYSCONFDIR="/etc/mc/"', '-DLIBEXECDIR="/usr/libexec/mc/"', '-DDATADIR="/usr/share/mc/"',
            '-DLOCALEDIR="/usr/share/locale"', '-DEXTHELPERSDIR="/usr/libexec/mc/ext.d/"']

# The values of mc's templates: the system's paths, and the programs its
# scripts run, by the names the system would have them under.
VALUES = {
    "PACKAGE": "mc", "bindir": "/usr/bin", "datarootdir": "/usr/share", "pkglibexecdir": "/usr/libexec/mc",
    "EXTHELPERSDIR": "/usr/libexec/mc/ext.d", "EXTFSHELPERSDIR": "/usr/libexec/mc/extfs.d",
    "AWK": "awk", "PERL": "/usr/bin/env perl", "PYTHON": "/usr/bin/python3", "RUBY": "/usr/bin/env ruby",
    "ZIP": "/usr/bin/zip", "UNZIP": "/usr/bin/unzip", "HAVE_ZIPINFO": "0", "MANDOC": "-man", "MAN_FLAGS": "",
    "ENCODING_ISO8859": "ISO-8859", "ENCODING_CP1251": "CP1251", "ENCODING_CP866": "CP866",
}


def constants(cc, include, names):
    """The values lib/c's headers give the names, as the preprocessor prints them."""
    text = "#include <poll.h>\n#include <sys/socket.h>\n" + "".join(f"@the_{name} {name}\n" for name in names)
    out = subprocess.run([cc, "--target=wasm32-unknown-unknown", "-nostdinc", "-isystem", include, "-E", "-P", "-x", "c", "-"],
                         input=text, capture_output=True, text=True, check=True).stdout
    found = dict(re.findall(r"^@the_(\w+) (.+)$", out, re.M))

    return {name: str(int(found[name], 0)) for name in names}


def glibconfig(cc, include, template, output):
    values = dict(GLIBCONFIG)
    system = constants(cc, include, sorted(set(GLIBCONFIG_SYSTEM.values())))
    values.update({key: system[name] for key, name in GLIBCONFIG_SYSTEM.items()})
    major, minor, micro = re.search(r"glib-(\d+)\.(\d+)\.(\d+)", str(template)).groups()
    values.update(GLIB_MAJOR_VERSION=major, GLIB_MINOR_VERSION=minor, GLIB_MICRO_VERSION=micro)
    text = template.read_text()
    text = re.sub(r"^#mesondefine (\w+)$",
                  lambda m: f"#define {m[1]}{GLIBCONFIG_DEFINED[m[1]]}" if m[1] in GLIBCONFIG_DEFINED else f"/* #undef {m[1]} */",
                  text, flags=re.M)
    output.write_text(re.sub(r"@(\w+)@", lambda m: values[m[1]], text))


def patched(tree, patch):
    """Applies a unified diff to a tree: every hunk's old lines are the
    file's at the place the hunk names, or it is an error. What comes
    before the first file's header is the patch's explanation."""
    name = None
    files = {}

    for line in patch.read_text().split("\n"):
        if line.startswith("--- a/"):
            name = None
        elif line.startswith("+++ b/"):
            name = line[len("+++ b/"):]
            files[name] = []
        elif name is not None and line.startswith("@@"):
            start = int(re.match(r"@@ -(\d+)", line)[1])
            files[name].append((start, [], []))
        elif name is not None and files[name] and line[:1] in (" ", "-", "+"):
            _, old, new = files[name][-1]

            if line[0] != "+":
                old.append(line[1:])

            if line[0] != "-":
                new.append(line[1:])

    for name, hunks in files.items():
        path = tree / name
        lines = path.read_text().split("\n")

        # from the last hunk to the first, so that the lines before keep their numbers
        for start, old, new in reversed(hunks):
            if lines[start - 1:start - 1 + len(old)] != old:
                raise SystemExit(f"{patch.name}: {name}: the lines at {start} are not the patch's")

            lines[start - 1:start - 1 + len(old)] = new

        path.write_text("\n".join(lines))


def units(sources, objects, flags):
    return [(source, objects / f"{i}-{source.stem}.o", flags) for i, source in enumerate(sources)]


def pcre2(tree, work, system):
    gen = work / "gen/pcre2"
    gen.mkdir(parents=True)
    shutil.copy(tree / "src/pcre2.h.generic", gen / "pcre2.h")
    shutil.copy(tree / "src/config.h.generic", gen / "config.h")
    shutil.copy(tree / "src/pcre2_chartables.c.dist", gen / "pcre2_chartables.c")
    flags = [*CFLAGS, *PCRE2_FLAGS, "-I", str(gen), "-I", str(tree / "src"), *system]
    sources = [gen / "pcre2_chartables.c", *(tree / "src" / f"pcre2_{name}.c" for name in PCRE2)]

    return units(sources, work / "o/pcre2", flags), gen


def slang(tree, work, system):
    gen = work / "gen/slang"
    gen.mkdir(parents=True)
    shutil.copy(HERE / "slang-config.h", gen / "config.h")
    shutil.copy(tree / "src/terminfo/default.inc", gen / "terminfo.inc")
    flags = [*CFLAGS, "-DSLANG", "-I", str(gen), "-I", str(tree / "src"), *system]

    return units([tree / "src" / f"{name}.c" for name in SLANG], work / "o/slang", flags)


def glib(cc, include, tree, work, system, pcre2_headers):
    gen = work / "gen/glib"
    (gen / "glib").mkdir(parents=True)
    shutil.copy(HERE / "glib-config.h", gen / "config.h")
    glibconfig(cc, include, tree / "glib/glibconfig.h.in", gen / "glibconfig.h")
    version = re.search(r"glib-([\d.]+)$", tree.name)[1]
    generator = [sys.executable, str(tree / "tools/gen-visibility-macros.py"), version]
    subprocess.run([*generator, "versions-macros", str(tree / "glib/gversionmacros.h.in"), str(gen / "glib/gversionmacros.h")], check=True)
    subprocess.run([*generator, "visibility-macros", "GLIB", str(gen / "glib/glib-visibility.h")], check=True)
    includes = ["-I", str(gen), "-I", str(gen / "glib"), "-I", str(tree), "-I", str(tree / "glib"), "-I", str(pcre2_headers)]
    flags = [*CFLAGS, *GLIB_FLAGS, *includes, *system]
    built = units([tree / "glib" / f"{name}.c" for name in GLIB], work / "o/glib", flags)

    # The path of an open file is asked of the system by its name, and GLib
    # knows one that cannot tell it, the Hurd: g_unix_fd_query_path is not
    # supported there, as it is not here.
    return [(source, obj, flags + ["-D__GNU__"] if source.name == "glib-unix.c" else flags) for source, obj, flags in built], includes[:8]


def mc(tree, work, system, glib_includes, slang_tree):
    gen = work / "gen/mc"
    gen.mkdir(parents=True)
    shutil.copy(HERE / "mc-config.h", gen / "config.h")
    # C23's checked arithmetic, which lib/c has no header of: mc's own, made
    # where its configure makes it, with GLib's boolean.
    checked = (tree / "lib/stdckdint.in.h").read_text().splitlines(keepends=True)
    (tree / "lib/stdckdint.h").write_text("".join(line.replace("bool", "gboolean", 1) for line in checked))
    sources = sorted(source for directory in ("lib", "src") for source in (tree / directory).rglob("*.c")
                     if not any(str(source.relative_to(tree)).startswith(left) for left in MC_LEFT))
    flags = [*CFLAGS, *MC_FLAGS, "-I", str(gen), "-I", str(tree), *glib_includes, "-I", str(slang_tree / "src"), *system]

    return units(sources, work / "o/mc", flags)


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


# misc's files of /etc/mc; its others are /usr/share/mc's.
ETC = ["edit.indent.rc", "filehighlight.ini", "mc.default.keymap", "mc.emacs.keymap", "mc.ext.ini", "mc.menu", "mc.vim.keymap",
       "mcedit.menu"]


def data(tree, output):
    """mc.tar: what make install puts under /etc/mc, /usr/share/mc and
    /usr/libexec/mc."""
    misc = tree / "misc"
    script, plain = (lambda name: 0o755), (lambda name: 0o644)
    trees = {
        "etc/mc": {**installed(misc, lambda name: 0o755 if name == "edit.indent.rc" else 0o644, only=ETC),
                   **installed(tree / "src/vfs/sfs", plain, only=["sfs.ini"])},
        "share/mc": installed(misc, plain, only=["mc.lib", "mc.charsets"]),
        "share/mc/skins": installed(misc / "skins", plain),
        "share/mc/syntax": installed(misc / "syntax", plain),
        "share/mc/hints": installed(tree / "doc/hints", plain),
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
    if len(sys.argv) != 11:
        raise SystemExit(__doc__)

    cc, ld, include, libc = sys.argv[1:5]
    tarballs = sys.argv[5:9]
    output, share = Path(sys.argv[9]), sys.argv[10]
    include = str(Path(include).resolve())
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "mc"
    shutil.rmtree(work, ignore_errors=True)
    (work / "src").mkdir(parents=True)

    for tarball in tarballs:
        with tarfile.open(tarball) as archive:
            archive.extractall(work / "src", filter="data")

    mc_tree, glib_tree, slang_tree, pcre2_tree = (next((work / "src").glob(f"{name}-*")) for name in ("mc", "glib", "slang", "pcre2"))

    for tree in (glib_tree, mc_tree):
        for patch in PATCHES[tree.name.split("-")[0]]:
            patched(tree, HERE / patch)

    system = ["-isystem", include]
    regex, pcre2_headers = pcre2(pcre2_tree, work, system)
    utility, glib_includes = glib(cc, include, glib_tree, work, system, pcre2_headers)
    built = [*regex, *slang(slang_tree, work, system), *utility, *mc(mc_tree, work, system, glib_includes, slang_tree)]
    wasm.compile_all(cc, built)
    output.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run([ld, *LINK, "-o", str(output), *(str(obj) for _, obj, _ in built), libc], check=True)
    data(mc_tree, share)
    shutil.rmtree(work)


if __name__ == "__main__":
    main()
