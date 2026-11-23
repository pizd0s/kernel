#!/usr/bin/env python3
"""Build lib/glib: GLib's libglib of its release tarball, for lib/c, with
PCRE2's (lib/pcre2) under its regular expressions.

    make.py <cc> <ar> <tarball> <libc-include> <pcre2 include.tar> <libglib.a> <include.tar>

What GLib's meson would make is made here. config.h is ours
(glib-config.h); glibconfig.h comes from its template with wasm32's sizes
and lib/c's constants, its two other built headers from its own
generator. The library is glib/meson.build's sources of libglib for a
Unix and libcharset's, the tree patched as it is unpacked
(glib-function-types.patch, its reason at its head; a patch applies to
the lines it names, exactly, or the build stops). Threads are lib/c's one
thread: GLib locks and keeps thread-local values through it and starts
no thread for what a program asks of it.

include.tar is include/, as meson installs the headers: glib.h and
glib-unix.h, glibconfig.h, and glib/ of meson's glib_sub_headers, the
built two and deprecated/; not glib-object.h, of GObject, which is not
built.
"""
import os
import re
import shutil
import subprocess
import sys
import tarfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parent.parent / "dev"))

import wasm  # noqa: E402

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
CFLAGS = ["-O2", "-w", "-DHAVE_CONFIG_H"]

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


def installed(tree, name):
    """The headers of one of glib/meson.build's lists, paths of glib/."""
    found = re.search(rf"^\s*{name} = files\((.*?)\)", (tree / "glib" / "meson.build").read_text(), re.M | re.S)

    if not found:
        raise SystemExit(f"glib/meson.build has no {name}")

    return re.findall(r"'([^']+\.h)'", found[1])


def main():
    cc, ar, tarball, libc_include, pcre2_include, library, include_tar = sys.argv[1:8]
    include = str(Path(libc_include).resolve())
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "libglib"
    shutil.rmtree(work, ignore_errors=True)
    tree = wasm.unpack(tarball, work / "src")
    wasm.patched(tree, HERE / "glib-function-types.patch")

    with tarfile.open(pcre2_include) as archive:
        archive.extractall(work / "pcre2", filter="data")

    gen = work / "gen"
    (gen / "glib").mkdir(parents=True)
    shutil.copy(HERE / "glib-config.h", gen / "config.h")
    glibconfig(cc, include, tree / "glib/glibconfig.h.in", gen / "glibconfig.h")
    version = re.search(r"glib-([\d.]+)$", tree.name)[1]
    generator = [sys.executable, str(tree / "tools/gen-visibility-macros.py"), version]
    subprocess.run([*generator, "versions-macros", str(tree / "glib/gversionmacros.h.in"), str(gen / "glib/gversionmacros.h")], check=True)
    subprocess.run([*generator, "visibility-macros", "GLIB", str(gen / "glib/glib-visibility.h")], check=True)
    flags = [*CFLAGS, *GLIB_FLAGS, "-I", str(gen), "-I", str(gen / "glib"), "-I", str(tree), "-I", str(tree / "glib"),
             "-I", str(work / "pcre2" / "include"), "-isystem", include]
    # The path of an open file is asked of the system by its name, and GLib
    # knows one that cannot tell it, the Hurd: g_unix_fd_query_path is not
    # supported there, as it is not here.
    units = [(tree / "glib" / f"{name}.c", work / "obj" / f"{name.replace('/', '_')}.o",
              flags + ["-D__GNU__"] if name == "glib-unix" else flags) for name in GLIB]
    wasm.compile_all(cc, units)
    wasm.archive(ar, library, [obj for _, obj, _ in units])
    top = [name for name in installed(tree, "glib_headers") if name != "glib-object.h"] + installed(tree, "glib_unix_headers")
    wasm.headers(include_tar, work / "out" / "include", [
        *((tree / "glib" / name, name) for name in top),
        (gen / "glibconfig.h", "glibconfig.h"),
        *((tree / "glib" / name, f"glib/{name}") for name in installed(tree, "glib_sub_headers")),
        *((tree / "glib" / name, f"glib/{name}") for name in installed(tree, "glib_deprecated_headers")),
        (gen / "glib" / "gversionmacros.h", "glib/gversionmacros.h"),
        (gen / "glib" / "glib-visibility.h", "glib/glib-visibility.h"),
    ])


if __name__ == "__main__":
    main()
