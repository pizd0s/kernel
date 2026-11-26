"""The system's wasm32 C toolchain: flags every object is built with and a parallel compiler driver.

Long double is 64-bit (cc1 -mlong-double-64): the ABI has no binary128 and so
no compiler-rt soft-float. -fno-builtin keeps the compiler from inventing
calls to libc functions the translation unit never declared, which would
reach the linker without their kernel import attribute.

An object has no name of the directory it was built in: a source's name in
it, an assert's __FILE__ and a debug line's file, is its path from the
tree's root or from the build's temporary directory (placeless), so that
the same sources make the same object wherever they are built.
"""
import concurrent.futures
import io
import os
import re
import subprocess
import sys
import tarfile
from pathlib import Path

CFLAGS = [
    "--target=wasm32-unknown-unknown",
    "-Xclang",
    "-mlong-double-64",
    "-mbulk-memory",
    "-nostdinc",
    "-fno-builtin",
    "-ffunction-sections",
    "-fdata-sections",
]


def jobs():
    return int(os.environ.get("BUILD_JOBS") or os.cpu_count() or 4)


def placeless(*directories):
    """The flags that leave the directories of a build out of the names of
    the sources in what is compiled: the tree's root, the temporary
    directory the build's scripts unpack and generate under, and those
    given, a script's own of a random name. A path under one of them is its
    path from there, and the directory itself ".". They go from the outer to
    the inner, so that the inner one's flag is taken of those that fit a
    path, whether a compiler takes the longest or the last."""
    roots = [Path(__file__).resolve().parent.parent, Path(os.environ.get("TMPDIR") or "/tmp"), *directories]

    return [flag for root in roots for flag in (f"-ffile-prefix-map={root}=.", f"-ffile-prefix-map={root}/=")]


def compile_one(cc, source, output, flags, cwd, places):
    Path(output).parent.mkdir(parents=True, exist_ok=True)
    result = subprocess.run(
        [cc, *CFLAGS, *placeless(*places), *flags, "-c", str(source), "-o", str(output)],
        cwd=cwd,
        capture_output=True,
        text=True,
    )

    return source, result


def compile_all(cc, units, cwd=None, places=()):
    """Compiles the units, each a source, its object and its flags. places
    are the directories of random names a script has sources in, which
    placeless leaves out of their names with the build's own."""
    failed = []

    with concurrent.futures.ThreadPoolExecutor(max_workers=jobs()) as pool:
        futures = [pool.submit(compile_one, cc, source, output, flags, cwd, places) for source, output, flags in units]

        for future in concurrent.futures.as_completed(futures):
            source, result = future.result()

            if result.returncode != 0:
                failed.append((str(source), result.stderr))

    for source, stderr in sorted(failed):
        sys.stderr.write(stderr)

    if failed:
        raise SystemExit(f"{len(failed)} of {len(units)} objects failed to compile")


def archive(ar, output, objects):
    Path(output).parent.mkdir(parents=True, exist_ok=True)
    Path(output).unlink(missing_ok=True)
    subprocess.run([ar, "rcs", str(output), *map(str, objects)], check=True)


def write_tar(output, base, root):
    """Write the tree under root to output, each entry named by its path from
    base, with no times or owners: the same tree is the same archive."""
    Path(output).parent.mkdir(parents=True, exist_ok=True)

    with tarfile.open(output, "w", format=tarfile.PAX_FORMAT) as archive:
        for path in sorted(Path(root).rglob("*")):
            info = archive.gettarinfo(path, str(path.relative_to(base)))
            info.mtime, info.uid, info.gid, info.uname, info.gname = 0, 0, 0, "", ""
            info.mode = 0o755 if path.is_dir() else 0o644

            if path.is_dir():
                archive.addfile(info)
            else:
                archive.addfile(info, io.BytesIO(path.read_bytes()))


def unpack(tarball, work):
    """A release tarball extracted under work: its tree, the one directory at
    its top."""
    Path(work).mkdir(parents=True, exist_ok=True)

    with tarfile.open(tarball) as archive:
        archive.extractall(work, filter="data")

    (tree,) = [path for path in Path(work).iterdir() if path.is_dir()]

    return tree


def library(cc, ar, tree, sources, flags, output, work):
    """The archive output of sources, paths of the tree, each compiled with
    flags in the tree, its object in work's obj."""
    units = [(tree / source, Path(work) / "obj" / (str(Path(source).with_suffix("")).replace("/", "_") + ".o"), flags)
             for source in sources]
    compile_all(cc, units, cwd=tree)
    archive(ar, output, [obj for _, obj, _ in units])


def headers(output, include, files):
    """output, the tar of include/, the headers of files: each a source path
    and its name under include."""
    for source, name in files:
        target = Path(include) / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes(Path(source).read_bytes())

    write_tar(output, Path(include).parent, include)


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
