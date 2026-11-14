#!/usr/bin/env python3
"""The image of the system, read-only: the programs of the system and the
directories it expects, in a layout of its own, which rootfs has linked
into it and erofs serves (bin/tools), with nothing to build at start.

    image.py <rootfs.img> --busybox <busybox> <busybox.links> --python <python3> [--program <name> <path>]...
             [--link <name> <target>]... --server <name>

The programs are executables (dev/exe.py), whose modules the kernel
unpacks and compiles as they are executed, and every one is in /bin:
busybox is /bin/busybox, and each name of busybox.links, wherever busybox
would install it, a link to it in /bin; python is /bin/python3, its
standard library the tail of its executable; each --program is
/bin/<name>, each --link a link /bin/<name> to target, as the tools of the
system are to /bin/tools. /usr is a link to the root, for what names
/usr/bin and the like: /usr/bin is /bin. The --server program serves the
image: rootfs runs it from the image itself, as erofs. No times, no
owners: the same inputs are the same image.

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
file of /bin/<server>.
"""
import os
import stat
import struct
import sys
from pathlib import Path

DIRS = {"bin", "dev", "etc", "tmp", "var"}
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


def main():
    output = sys.argv[1]
    args = sys.argv[2:]
    files = {"etc/motd": (b"pizdos\n", 0o644), "etc/passwd": (b"root:x:0:0:root:/:/bin/sh\n", 0o644), "etc/group": (b"root:x:0:\n", 0o644)}
    symlinks = {"usr": "/"}
    dirs = set(DIRS)
    server = None

    while args:
        match args:
            case ["--busybox", busybox, links, *rest]:
                files["bin/busybox"] = (Path(busybox).read_bytes(), 0o755)

                for link in Path(links).read_text().split():
                    symlinks["bin/" + os.path.basename(link)] = "busybox"
            case ["--python", python, *rest]:
                files["bin/python3"] = (Path(python).read_bytes(), 0o755)
                symlinks["bin/python"] = "python3"
            case ["--program", name, program, *rest]:
                files["bin/" + name] = (Path(program).read_bytes(), 0o755)
            case ["--link", name, target, *rest]:
                symlinks["bin/" + name] = target
            case ["--server", name, *rest]:
                server = "bin/" + name
            case _:
                raise SystemExit(f"image.py: unknown arguments {args}")

        args = rest

    for path in list(files) + list(symlinks):
        parent = os.path.dirname(path)

        while parent:
            dirs.add(parent)
            parent = os.path.dirname(parent)

    for path in list(files) + list(symlinks):
        dirs.discard(path)

    if server not in files:
        raise SystemExit("image.py: --server names no --program")

    Path(output).parent.mkdir(parents=True, exist_ok=True)
    Path(output).write_bytes(layout(dirs, files, symlinks, server))


if __name__ == "__main__":
    main()
