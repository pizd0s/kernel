#!/usr/bin/env python3
"""Cut what a build takes of a big source tarball into a small one, once
after its download, so a build that takes it unpacks only that.

    slice.py <tarball> <root> <prefixes> <output.tar.gz>

root is the tree's directory in the tarball, usr/src of FreeBSD's
src.txz. prefixes is a file of paths in the tree, one a line, # a comment:
a member under one of them goes to the output, named from the tree's root.
"""
import os
import sys
import tarfile


def wanted(name, root, prefixes):
    if not name.startswith(root + "/"):
        return None

    path = name[len(root) + 1:]

    for prefix in prefixes:
        if path == prefix or path.startswith(prefix.rstrip("/") + "/"):
            return path

    return None


def main():
    tarball, root, listing, output = sys.argv[1:5]
    prefixes = [line.split("#", 1)[0].strip() for line in open(listing)]
    prefixes = [prefix for prefix in prefixes if prefix]
    root = root.strip("/")
    temporary = output + ".tmp"
    found = set()

    with tarfile.open(tarball) as source, tarfile.open(temporary, "w:gz") as sliced:
        for member in source:
            path = wanted(member.name.lstrip("./"), root, prefixes)

            if path is None or not (member.isfile() or member.isdir()):
                continue

            found.update(prefix for prefix in prefixes if path == prefix or path.startswith(prefix.rstrip("/") + "/"))
            member.name = path
            sliced.addfile(member, source.extractfile(member) if member.isfile() else None)

    missing = [prefix for prefix in prefixes if prefix not in found]

    if missing:
        os.unlink(temporary)
        sys.exit(f"slice.py: not in {tarball}: {' '.join(missing)}")

    os.replace(temporary, output)


if __name__ == "__main__":
    main()
