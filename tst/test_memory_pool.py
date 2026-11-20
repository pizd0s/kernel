#!/usr/bin/env python3
"""The guests' memory is one part of fixed size the kernel places their
regions in and moves them within (bin/vm/pool.go).

    test_memory_pool.py <vm> <memory_pool> <busybox>

tst/memory_pool.c runs as the first process of a vm of 256 MiB: a filler
takes what its children leave, a fork finds no room, holes open between
the children and a child's growth takes three of them together, a child
between two of them moved as it waits in a read and, the second time, as
it runs a loop. A program the kernel has to compile with no room left
fails its exec with ENOMEM: a shell does not run it as a script. A memory
too small for the first process ends it with ENOMEM; --memory takes
bytes, K, M or G.
"""
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile


def main():
    vm, program, busybox = sys.argv[1:]
    expected = (b"filler: took the memory left\n"
                b"fork with no room: ENOMEM\n"
                b"ended: 3 children kept their blocks\n"
                b"grown: y\n"
                b"fourth: y\n"
                b"sixth: y\n"
                b"end: 4 of 4 ended clean\n")
    for mode in ("waiting", "running"):
        result = subprocess.run([vm, "run", "--memory", "256M", "--", program, mode], capture_output=True, timeout=300)
        assert (result.returncode, result.stdout, result.stderr) == (0, expected, b""), (mode, result)

    with tempfile.TemporaryDirectory(prefix="pizdos-memory-pool-") as directory:
        disk = Path(directory) / "busybox"
        shutil.copyfile(busybox, disk)
        result = subprocess.run([vm, "run", "--memory", "256M", "--disk", f"program={disk}", "--", program, "exec"],
                                capture_output=True, timeout=300)
        assert (result.returncode, result.stdout, result.stderr) == (0, b"exec with no room: ENOMEM\n", b""), result

    result = subprocess.run([vm, "run", "--memory", "64K", "--", program], capture_output=True, timeout=60)
    assert result.returncode == 1 and b"cannot allocate memory" in result.stderr, result

    for size in ("0", "12X", "", "M", "99999999999G"):
        result = subprocess.run([vm, "run", "--memory", size, "--", program], capture_output=True, timeout=60)
        assert result.returncode == 1 and b"usage:" in result.stderr, (size, result)

    print("memory pool: a growth gathers holes, moving a guest that waits and one that runs; a full pool fails fork, growth and a compile (ENOMEM); sizes parse")


if __name__ == "__main__":
    main()
