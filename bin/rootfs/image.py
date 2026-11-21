#!/usr/bin/env python3
"""The image of the system, read-only: its packages and the root that
names them, in a layout of its own, which follows rootfs in its file and
erofs serves (bin/tools), with nothing to build at start.

    image.py <rootfs.img> --realm <realm.tar.zst> [--package <package.tar.zst>]...
             [--file <path> <host path>]... [--tree <path> <host directory>]... [--tar <path> <tarball> <member>]...
             --server <path>

The root is as ix makes a host's: each package is its directory of the
store, /pzd/store/<uid>-<name> (dev/pkg.py), the realm one of them, of
links to the files of the others, /pzd/realm/system is a link to the
realm's directory, /bin and /etc links to its bin and etc, /usr a link to
the root, and /dev and /run are directories, where rootfs mounts devfs and
tmpfs. A link of the realm to no file of the image stops the build. A
--file is the image's file at path, of the host file's bytes and its mode,
a --tree the host directory's files and links under path, as the image of
the tests has them, and a --tar the files and links of a tarball under its
member, a directory, under path. The program at --server's path, through
the links, serves the image: rootfs runs it from the image itself, as
erofs. No times, no owners: the same inputs are the same image.

The layout, little-endian, its tables at offsets a multiple of 16:

    header   "PZIM", version u32 2, nodes u32, entries u32,
             node table u32, entry table u32, names u32, data u32,
             size u32, server u32, server size u32
    node     mode u32, nlink u32, uid u32, gid u32, size u32, data u32,
             parent u32, entry u32
    entry    name u32, length u32, node u32

A node's number is its place in the node table, the root's 0, its inode
the number plus one. A directory's entries are entry table[data, data +
size), sorted by their names' bytes; its parent is the node of "..", the
root's itself, and entry is its own entry in its parent's, the root's
none (all ones). A file's or a link's bytes are the image's [data, data +
size), a file's at an offset a multiple of 16, all of them from the
header's data on: the header and the tables are before it. A directory's
nlink is two and one for each directory in it, every other node's one.
The header's server is where the bytes of the server's program are, the
file --server names.
"""
import os
import stat
import struct
import sys
import tarfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "dev"))

import pkg  # noqa: E402

DIRS = {"dev", "run"}
ROOT = {"bin": "pzd/realm/system/bin", "etc": "pzd/realm/system/etc", "usr": "/"}
NONE = 0xFFFFFFFF
HEADER = struct.Struct("<4s10I")
NODE = struct.Struct("<8I")
ENTRY = struct.Struct("<3I")


def align(n, to=16):
    return (n + to - 1) // to * to


def layout(dirs, files, symlinks, server):
    """The image's bytes: dirs a set of directories, files their bytes and
    modes, symlinks their targets, by path, server the path of the file
    that serves it."""
    children = {"": {}}

    for path in sorted(dirs):
        children[path] = {}

    for path in sorted(dirs | set(files) | set(symlinks)):
        parent, name = os.path.split(path)
        children[parent][name] = path

    # The nodes, a directory's children before what is under them.
    order = [""]
    i = 0

    while i < len(order):
        path = order[i]

        for name in sorted(children.get(path, {}), key=lambda n: n.encode()):
            order.append(children[path][name])

        i += 1

    number = {path: n for n, path in enumerate(order)}
    names = bytearray()
    entries = []
    nodes = []
    data = []
    first = {}

    for path in order:
        if path in children:
            kids = sorted(children[path], key=lambda n: n.encode())
            first[path] = len(entries)

            for name in kids:
                encoded = name.encode()
                entries.append((len(names), len(encoded), number[children[path][name]]))
                names += encoded

    for path in order:
        parent, name = os.path.split(path)

        if path in children:
            subdirs = sum(1 for kid in children[path].values() if kid in children)
            entry = NONE if path == "" else first[parent] + sorted(children[parent], key=lambda n: n.encode()).index(name)
            nodes.append([stat.S_IFDIR | 0o755, 2 + subdirs, 0, 0, len(children[path]), first[path], number[parent] if path else 0, entry])
        elif path in files:
            content, mode = files[path]
            nodes.append([stat.S_IFREG | mode, 1, 0, 0, len(content), 0, number[parent], NONE])
            data.append((len(nodes) - 1, content, 16))
        else:
            target = symlinks[path].encode()
            nodes.append([stat.S_IFLNK | 0o777, 1, 0, 0, len(target), 0, number[parent], NONE])
            data.append((len(nodes) - 1, target, 1))

    node_table = align(HEADER.size)
    entry_table = align(node_table + NODE.size * len(nodes))
    names_at = align(entry_table + ENTRY.size * len(entries))
    data_at = align(names_at + len(names))
    blob = bytearray()

    for n, content, alignment in data:
        blob += bytes(align(data_at + len(blob), alignment) - data_at - len(blob))
        nodes[n][5] = data_at + len(blob)
        blob += content

    out = bytearray(HEADER.size)
    out += bytes(node_table - len(out))

    for node in nodes:
        out += NODE.pack(*node)

    out += bytes(entry_table - len(out))

    for entry in entries:
        out += ENTRY.pack(*entry)

    out += bytes(names_at - len(out))
    out += names
    out += bytes(data_at - len(out))
    out += blob
    program = nodes[number[server]]
    HEADER.pack_into(out, 0, b"PZIM", 2, len(nodes), len(entries), node_table, entry_table, names_at, data_at, len(out), program[5],
                     program[4])

    return bytes(out)


def resolve(path, symlinks):
    """Where path is in the image through its links, or None."""
    parts, at, hops = path.strip("/").split("/"), [], 0

    while parts:
        part = parts.pop(0)

        if part in ("", "."):
            continue

        if part == "..":
            at = at[:-1]
            continue

        here = "/".join(at + [part])

        if here in symlinks:
            hops += 1

            if hops > 40:
                return None

            target = symlinks[here]
            parts = target.strip("/").split("/") + parts

            if target.startswith("/"):
                at = []

            continue

        at.append(part)

    return "/".join(at)


def main():
    output = sys.argv[1]
    args = sys.argv[2:]
    files, symlinks, dirs = {}, dict(ROOT), set(DIRS)
    realm = server = None

    def unpack(package):
        top = pkg.root(package).strip("/")
        dirs.add(top)

        for path, kind, content, mode in pkg.entries(package):
            if kind == "dir":
                dirs.add(top + "/" + path)
            elif kind == "link":
                symlinks[top + "/" + path] = content
            else:
                files[top + "/" + path] = (content, mode)

    while args:
        match args:
            case ["--realm", package, *rest]:
                realm = package
                unpack(package)
            case ["--package", package, *rest]:
                unpack(package)
            case ["--file", path, host, *rest]:
                files[path.strip("/")] = (Path(host).read_bytes(), stat.S_IMODE(os.stat(host).st_mode))
            case ["--tree", path, host, *rest]:
                for entry in sorted(Path(host).rglob("*")):
                    inside = path.strip("/") + "/" + entry.relative_to(host).as_posix()

                    if entry.is_symlink():
                        symlinks[inside] = os.readlink(entry)
                    elif entry.is_dir():
                        dirs.add(inside)
                    else:
                        files[inside] = (entry.read_bytes(), stat.S_IMODE(entry.stat().st_mode))
            case ["--tar", path, tarball, member, *rest]:
                with tarfile.open(tarball) as archive:
                    for entry in archive.getmembers():
                        if not entry.name.startswith(member.rstrip("/") + "/"):
                            continue

                        inside = path.strip("/") + "/" + entry.name[len(member.rstrip("/")) + 1:].rstrip("/")

                        if entry.issym():
                            symlinks[inside] = entry.linkname
                        elif entry.isdir():
                            dirs.add(inside)
                        elif entry.isfile():
                            files[inside] = (archive.extractfile(entry).read(), stat.S_IMODE(entry.mode))
            case ["--server", path, *rest]:
                server = path
            case _:
                raise SystemExit(f"image.py: unknown arguments {args}")

        args = rest

    if realm is None:
        raise SystemExit("image.py: no --realm")

    symlinks["pzd/realm/system"] = pkg.root(realm)

    for path in list(files) + list(symlinks):
        parent = os.path.dirname(path)

        while parent:
            dirs.add(parent)
            parent = os.path.dirname(parent)

    for path in list(files) + list(symlinks):
        dirs.discard(path)

    top = pkg.root(realm).strip("/")

    for path in symlinks:
        if path.startswith(top + "/") and (end := resolve(path, symlinks)) not in files and end not in dirs:
            raise SystemExit(f"image.py: /{path} of the realm is a link to no file of the image")

    server = server and resolve(server, symlinks)

    if server not in files:
        raise SystemExit("image.py: --server names no file of the image")

    Path(output).parent.mkdir(parents=True, exist_ok=True)
    Path(output).write_bytes(layout(dirs, files, symlinks, server))


if __name__ == "__main__":
    main()
