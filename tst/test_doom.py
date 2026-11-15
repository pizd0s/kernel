#!/usr/bin/env python3
"""doom in the system: doomgeneric's frame is ANSI half blocks, its clock
the kernel's monotonic time. Headless it plays the shareware demo from the
wad, both the image of the tests' /tests/doom, and hashes each second of
frames, which are the same in two runs.

    test_doom.py <vm> <rootfs> <tests.img>
"""
import subprocess
import sys

COMMAND = "cd /tmp && exec env DG_HEADLESS=1 HOME=/tmp /tests/doom/doom -iwad /tests/doom/doom1.wad -nosound -timedemo demo1"


def timedemo(vm, rootfs, image):
    """The shareware demo plays twice, headless: the frames hash the same."""
    hashes = []

    for _ in range(2):
        r = subprocess.run([vm, "run", "--disk", f"image={image}", "--", rootfs, "-c", COMMAND], capture_output=True, timeout=300)
        out = r.stdout.decode(errors="replace")
        err = r.stderr.decode(errors="replace")

        if r.returncode != 255 or "timed" not in err or "fps" not in err:
            return None, f"timedemo: exit {r.returncode}, stdout {out[-500:]!r}, stderr {err[-500:]!r}"

        hashes.append([line for line in out.splitlines() if line.startswith("frame ")])

    if not hashes[0] or hashes[0] != hashes[1]:
        return None, f"the demo's frames differ between runs: {hashes[0][-3:]} vs {hashes[1][-3:]}"

    return hashes[0], None


def main():
    vm, rootfs, image = sys.argv[1:4]
    frames, error = timedemo(vm, rootfs, image)

    if not error and len(frames) < 10:
        error = f"timedemo hashed only {len(frames)} seconds of frames"

    assert not error, error
    print(f"{len(frames)} seconds of frames, identical runs passed")


if __name__ == "__main__":
    main()
