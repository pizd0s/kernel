"""The system's wasm32 C toolchain: flags every object is built with and a parallel compiler driver.

Long double is 64-bit (cc1 -mlong-double-64): the ABI has no binary128 and so
no compiler-rt soft-float. -fno-builtin keeps the compiler from inventing
calls to libc functions the translation unit never declared, which would
reach the linker without their kernel import attribute.
"""
import concurrent.futures
import io
import os
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


def compile_one(cc, source, output, flags, cwd):
    Path(output).parent.mkdir(parents=True, exist_ok=True)
    result = subprocess.run(
        [cc, *CFLAGS, *flags, "-c", str(source), "-o", str(output)],
        cwd=cwd,
        capture_output=True,
        text=True,
    )

    return source, result


def compile_all(cc, units, cwd=None):
    failed = []

    with concurrent.futures.ThreadPoolExecutor(max_workers=jobs()) as pool:
        futures = [pool.submit(compile_one, cc, source, output, flags, cwd) for source, output, flags in units]

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
