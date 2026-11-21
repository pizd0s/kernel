#!/usr/bin/env python3
"""Packages as ix makes them: a package is a directory of the store,
/pzd/store/<uid>-<name>, whose uid is its recipe's (build's $(UID)), and
its file here is a tar.zst of that directory, rooted at /.

    pkg.py pack <package.tar.zst> <directory> [--file <path> <host file>]... [--link <path> <target>]...
                [--links <directory> <target> <list>]... [--tar <path> <tarball> <member>]... [--tree <path> <host directory>]...
    pkg.py realm <realm.tar.zst> <directory> <package.tar.zst>...

pack lays out a package under its directory: a --file the host file's
bytes and mode at path, a --link a symbolic link, --links a link in
directory to target for each name a list file has, one a line, by its last
component (busybox's links), a --tar the files and links of a tarball under
its member, and a --tree those of a host directory. A realm is a package of
links, as ix's bld/realm/symlink makes one: each file and link of its
packages but those at the top of a package has its link in the realm at
the same path, to where it is in its package; of two packages with a path,
the later one's. The archives have no times and no owners: the same
inputs are the same bytes.
"""
import io
import os
import stat
import sys
import tarfile
from pathlib import Path

STORE = "/pzd/store"


def root(package):
    """The directory of the store a package's file holds, its first entry."""
    with tarfile.open(package, "r:zst") as archive:
        first = archive.next()

    if first is None or not first.isdir() or not ("/" + first.name).startswith(STORE + "/"):
        raise SystemExit(f"pkg.py: {package} holds no directory of the store")

    return "/" + first.name


def entries(package):
    """A package's entries below its directory: (path, kind, bytes or
    target, mode), the kind "dir", "file" or "link"."""
    top = root(package).lstrip("/")

    with tarfile.open(package, "r:zst") as archive:
        for entry in archive.getmembers():
            if entry.name == top:
                continue

            path = entry.name[len(top) + 1:]

            if entry.isdir():
                yield path, "dir", None, stat.S_IMODE(entry.mode)
            elif entry.issym():
                yield path, "link", entry.linkname, 0o777
            else:
                yield path, "file", archive.extractfile(entry).read(), stat.S_IMODE(entry.mode)


def write(output, directory, tree):
    """The tar.zst of directory: tree {path: (bytes, mode) of a file or the
    target of a link, a str}, and every directory its paths are in."""
    dirs = set()

    for path in tree:
        parent = os.path.dirname(path)

        while parent:
            dirs.add(parent)
            parent = os.path.dirname(parent)

    if clash := dirs & set(tree):
        raise SystemExit(f"pkg.py: {sorted(clash)[0]} is a directory and a file")

    top = directory.strip("/")
    Path(output).parent.mkdir(parents=True, exist_ok=True)

    with tarfile.open(output, "w:zst") as archive:
        for path in ["", *sorted(dirs | set(tree))]:
            info = tarfile.TarInfo(top + "/" + path if path else top)
            info.mtime, info.uid, info.gid, info.uname, info.gname = 0, 0, 0, "", ""
            item = tree.get(path)

            if item is None:
                info.type, info.mode = tarfile.DIRTYPE, 0o755
                archive.addfile(info)
            elif isinstance(item, str):
                info.type, info.mode, info.linkname = tarfile.SYMTYPE, 0o777, item
                archive.addfile(info)
            else:
                info.mode, info.size = item[1], len(item[0])
                archive.addfile(info, io.BytesIO(item[0]))


def pack(output, directory, args):
    tree = {}

    while args:
        match args:
            case ["--file", path, host, *rest]:
                tree[path.strip("/")] = (Path(host).read_bytes(), stat.S_IMODE(os.stat(host).st_mode))
            case ["--link", path, target, *rest]:
                tree[path.strip("/")] = target
            case ["--links", where, target, names, *rest]:
                for name in Path(names).read_text().split():
                    tree[where.strip("/") + "/" + os.path.basename(name)] = target
            case ["--tar", path, tarball, member, *rest]:
                with tarfile.open(tarball) as archive:
                    for entry in archive.getmembers():
                        if not entry.name.startswith(member.rstrip("/") + "/"):
                            continue

                        inside = path.strip("/") + "/" + entry.name[len(member.rstrip("/")) + 1:].rstrip("/")

                        if entry.issym():
                            tree[inside] = entry.linkname
                        elif entry.isfile():
                            tree[inside] = (archive.extractfile(entry).read(), stat.S_IMODE(entry.mode))
            case ["--tree", path, host, *rest]:
                for entry in sorted(Path(host).rglob("*")):
                    inside = path.strip("/") + "/" + entry.relative_to(host).as_posix()

                    if entry.is_symlink():
                        tree[inside] = os.readlink(entry)
                    elif entry.is_file():
                        tree[inside] = (entry.read_bytes(), stat.S_IMODE(entry.stat().st_mode))
            case _:
                raise SystemExit(f"pkg.py: unknown arguments {args}")

        args = rest

    write(output, directory, tree)


def realm(output, directory, packages):
    tree = {}

    for package in packages:
        top = root(package)

        for path, kind, _, _ in entries(package):
            if kind != "dir" and "/" in path:
                tree[path] = top + "/" + path

    write(output, directory, tree)


def main():
    match sys.argv[1:]:
        case ["pack", output, directory, *args]:
            pack(output, directory, args)
        case ["realm", output, directory, *packages]:
            realm(output, directory, packages)
        case _:
            sys.exit(__doc__)


if __name__ == "__main__":
    main()
