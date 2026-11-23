"""Programs run in the system, a batch to a boot: rootfs boots once, from
the image after it in its file, and runs each program of a batch in turn, in a
directory of its own under /run, its standard input given; its standard
output and error and its exit status come back. The batch goes in on the
disk in, the results come out on the disk out: the system's standard
streams are a terminal's, which carries no binary data.

    run(vm, rootfs, programs, env=None, flags=(), timeout=600, faults=False)

A program is (name, executable or module, args, stdin): a module of
WebAssembly is packed into an executable (dev/exe.py) first; name is its
argv[0], by which PATH finds it. The results are (status, stdout, stderr),
the programs' order: the status the shell's, 128 and the signal for a
program a signal ended. env is vm's, its compiler's modes; flags are vm
run's options, --asan. With faults each program runs by spawn
--strace=faults: its standard error has the trace of its faults, a trap's
kind and stack, as nothing else writes them.

    shell(vm, rootfs, script, flags=(), **kwargs)

runs script by the system's shell, as rootfs -c, its input /dev/null: its
standard output is the terminal, vm's standard output, where ls writes
columns, its standard error /dev/console, vm's standard error; the
status is the script's. kwargs are subprocess.run's; the CR the
terminals put before each newline is taken off.
"""
import io
import shlex
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "dev"))

import exe  # noqa: E402

# The programs' directories come as a tar on the disk in; each runs by its
# script, run, which gives the program its files and execs it, so what the
# shell says of a program a signal ended is none of its standard error; a
# tar of what they gave goes out on the disk out.
SCRIPT = """\
mkdir /run/b && cd /run/b && tar xf /dev/blk/in/data || exit 1
for d in *; do
    (cd "$d" && PATH="$PWD:$PATH" sh ./run 2> /dev/null; echo $? > status)
done
tar cf /dev/blk/out/data */out */err */status
"""


def shell(vm, rootfs, script, flags=(), **kwargs):
    command = [str(vm), "run", *flags, "--", str(rootfs), "-c", "exec < /dev/null 2> /dev/console\n" + script]
    result = subprocess.run(command, stdin=subprocess.DEVNULL, capture_output=True, **kwargs)
    out, err = (b.replace(b"\r\n", b"\n") for b in (result.stdout, result.stderr))
    return subprocess.CompletedProcess(result.args, result.returncode, out, err)


def add(archive, name, data, mode=0o644):
    entry = tarfile.TarInfo(name)
    entry.size, entry.mode = len(data), mode
    archive.addfile(entry, io.BytesIO(data))


def run(vm, rootfs, programs, env=None, flags=(), timeout=600, faults=False):
    payload = io.BytesIO()

    with tarfile.open(fileobj=payload, mode="w") as archive:
        for i, (name, program, args, stdin) in enumerate(programs):
            if program[:4] != b"PZD1":
                program = exe.pack(program)

            add(archive, f"{i:05d}/{name}", program, 0o755)
            command = ["spawn", "--strace=faults", "--", name, *args] if faults else [name, *args]
            add(archive, f"{i:05d}/run", b"exec < stdin > out 2> err\n" + shlex.join(["exec", *command]).encode() + b"\n")
            add(archive, f"{i:05d}/stdin", stdin or b"")

    with tempfile.TemporaryDirectory(prefix="pizdos-batch-") as directory:
        disks = Path(directory)
        (disks / "in.tar").write_bytes(payload.getvalue())

        with open(disks / "out.tar", "wb") as f:
            f.truncate(1 << 30)

        result = subprocess.run([str(vm), "run", *flags, "--disk", f"in={disks / 'in.tar'}", "--disk", f"out={disks / 'out.tar'}", "--",
                                 str(rootfs), "-c", SCRIPT], stdin=subprocess.DEVNULL, capture_output=True, env=env, timeout=timeout)

        if result.returncode != 0:
            raise RuntimeError(f"the batch's boot: exit {result.returncode}\n{result.stdout.decode(errors='replace')}{result.stderr.decode(errors='replace')}")

        files = {}

        with tarfile.open(disks / "out.tar") as archive:
            for entry in archive.getmembers():
                if entry.isfile():
                    files[entry.name] = archive.extractfile(entry).read()

    return [(int(files[f"{i:05d}/status"]), files[f"{i:05d}/out"], files[f"{i:05d}/err"]) for i in range(len(programs))]
