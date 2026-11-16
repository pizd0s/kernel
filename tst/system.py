"""Programs run in the system, a batch to a boot: rootfs boots once, from
the image linked into it, and runs each program of a batch in turn, in a
directory of its own under /tmp, its standard input given; its standard
output and error and its exit status come back.

    run(vm, rootfs, programs, env=None, flags=(), timeout=600)

A program is (name, executable or module, args, stdin): a module of
WebAssembly is packed into an executable (dev/exe.py) first; name is its
argv[0], by which PATH finds it. The results are (status, stdout, stderr),
the programs' order: the status the shell's, 128 and the signal for a
program a signal ended. env is vm's, its compiler's modes; flags are vm
run's options, --asan.
"""
import io
import shlex
import subprocess
import sys
import tarfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "dev"))

import exe  # noqa: E402

# The programs' directories come as a tar on the standard input; each runs
# by its script, run, which gives the program its files and execs it, so
# what the shell says of a program a signal ended is none of its standard
# error; a tar of what they gave goes out on the standard output.
SCRIPT = """\
mkdir /tmp/b && cd /tmp/b && tar xf - || exit 1
for d in *; do
    (cd "$d" && PATH="$PWD:$PATH" sh ./run 2> /dev/null; echo $? > status)
done
tar cf - */out */err */status
"""


def add(archive, name, data, mode=0o644):
    entry = tarfile.TarInfo(name)
    entry.size, entry.mode = len(data), mode
    archive.addfile(entry, io.BytesIO(data))


def run(vm, rootfs, programs, env=None, flags=(), timeout=600):
    payload = io.BytesIO()

    with tarfile.open(fileobj=payload, mode="w") as archive:
        for i, (name, program, args, stdin) in enumerate(programs):
            if program[:4] != b"PZD1":
                program = exe.pack(program)

            add(archive, f"{i:05d}/{name}", program, 0o755)
            add(archive, f"{i:05d}/run", b"exec < stdin > out 2> err\n" + shlex.join(["exec", name, *args]).encode() + b"\n")
            add(archive, f"{i:05d}/stdin", stdin or b"")

    result = subprocess.run([str(vm), "run", *flags, "--", str(rootfs), "-c", SCRIPT], input=payload.getvalue(), capture_output=True,
                            env=env, timeout=timeout)

    if result.returncode != 0:
        raise RuntimeError(f"the batch's boot: exit {result.returncode}\n{result.stderr.decode(errors='replace')}")

    files = {}

    with tarfile.open(fileobj=io.BytesIO(result.stdout)) as archive:
        for entry in archive.getmembers():
            if entry.isfile():
                files[entry.name] = archive.extractfile(entry).read()

    return [(int(files[f"{i:05d}/status"]), files[f"{i:05d}/out"], files[f"{i:05d}/err"]) for i in range(len(programs))]
