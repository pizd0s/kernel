#!/usr/bin/env python3
"""Fetch what a build takes of a git repository that has no release
tarballs of stable bytes, as AOSP's: the paths a list names, at a commit,
packed into a tarball of its own.

    fetch_git.py <url> <ref> <commit> <prefixes> <output.tar.gz>

The ref, a tag, is fetched alone and with no blobs but those of the paths,
and must be the commit, whose hash is the content's: a ref that moved is
an error. prefixes is a file of paths in the tree, one a line, # a
comment. The tarball's entries are named from the tree's root, in their
order, with no times or owners: the same commit is the same tarball.
"""
import gzip
import io
import os
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path


def git(work, *args):
    return subprocess.run(["git", "-C", str(work), *args], check=True, capture_output=True, text=True).stdout.strip()


def main():
    url, ref, commit, listing, output = sys.argv[1:6]
    prefixes = [line.split("#", 1)[0].strip() for line in open(listing)]
    prefixes = [prefix.rstrip("/") for prefix in prefixes if prefix]

    with tempfile.TemporaryDirectory(prefix="fetch-git-") as temporary:
        work = Path(temporary)
        git(work, "init", "-q")
        git(work, "fetch", "-q", "--depth", "1", "--filter=blob:none", url, ref)
        fetched = git(work, "rev-parse", "FETCH_HEAD^{commit}")

        if fetched != commit:
            sys.exit(f"fetch_git.py: {url} {ref} is {fetched}, expected {commit}")

        git(work, "sparse-checkout", "set", "--no-cone", *prefixes)
        git(work, "checkout", "-q", "--detach", commit)
        files = []

        for prefix in prefixes:
            path = work / prefix
            found = sorted(p for p in path.rglob("*") if p.is_file()) if path.is_dir() else [path] if path.is_file() else []

            if not found:
                sys.exit(f"fetch_git.py: not in {url} at {commit}: {prefix}")

            files += found

        target = Path(output)
        target.parent.mkdir(parents=True, exist_ok=True)
        data = io.BytesIO()

        with gzip.GzipFile(fileobj=data, mode="wb", mtime=0) as packed, tarfile.open(fileobj=packed, mode="w") as archive:
            for path in sorted(set(files)):
                info = archive.gettarinfo(path, str(path.relative_to(work)))
                info.mtime, info.uid, info.gid, info.uname, info.gname = 0, 0, 0, "", ""
                info.mode = 0o755 if os.access(path, os.X_OK) else 0o644

                with open(path, "rb") as content:
                    archive.addfile(info, content)

        partial = target.with_name(target.name + ".part")
        partial.write_bytes(data.getvalue())
        partial.replace(target)


if __name__ == "__main__":
    main()
