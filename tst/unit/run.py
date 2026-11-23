#!/usr/bin/env python3
"""Compile a C++ program, check that both AOT executables compile it to the
same object, check its behaviour natively, and run it inside the system.

    run.py build <case.cpp> <output-prefix> <toolchain-json>
    run.py run <case.cpp> <output-prefix> <name> <toolchain-json>
    run.py all <case.cpp> <output-prefix> <name> <toolchain-json>

build compiles the case to <prefix>.wasm, checks that aot and vm -o make
the same object of it and that the native reference behaves as expected,
and packs <prefix>.exe, the image of the tests' /tests/unit/<name>. run
boots rootfs from that image (--disk image=) and runs the program there,
on the system's terminal, a pseudoterminal: its standard output is the
terminal's, vm's standard output; its standard error /dev/console, vm's
standard error; its input ends at once. What the terminals' output
processing adds, a CR before each newline, is taken off both. It runs
in /run, its environment rootfs's with the env directives; with root:
own, in a root of its own under /run instead, chroot's, which has /bin/check,
the program, and /run, its working directory the root, for a program
that makes /etc's files or execs itself as /bin/check. all is build and
run, as coverage runs a case with its own vm and aot.

Directives use trustme's //@ spelling. stdout/stderr are JSON strings;
compile-flags, aot-flags, run-flags and args use shell quoting. host: false
disables the native reference for programs that depend on this kernel.
run-flags are --asan, --strace, --user and --disk <name>=<path>: --strace
is vm's, a trace of every process of the system on vm's standard error, as
the trace of faults has a trap's kind and stack; --user is the
system's, everyone's in it, as vm's; a disk is /dev/blk/<name>/data in
the system, which the program gets open to read and write as its
descriptors after 2, in order, their names in DISKS, as the first process
gets vm's disks. $ROOT in a disk's path is a directory of the host with
the program's executable as bin/check, and in an env directive the
reference's working directory.
"""
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "dev"))
import wasm
import exe

LINK = ["--stack-first", "--max-memory=4294967296",
        "--export=__heap_base", "--export=__stack_pointer", "--gc-sections"]
DIRECTIVES = {"compile-flags", "aot-flags", "run-flags", "args", "env", "stdout", "stderr", "stderr-pattern", "exit-code", "host", "timeout", "max-memory", "root"}


def directives(source):
    result = {}

    for key, value in re.findall(r"^//@\s*([a-z-]+):\s*(.*?)\s*$", source.read_text(), re.M):
        if key not in DIRECTIVES:
            raise ValueError(f"{source}: unknown directive {key}")
        if key in result and key != "env":
            raise ValueError(f"{source}: repeated directive {key}")
        if key == "env":
            name, separator, text = value.partition("=")
            if not separator or not re.fullmatch(r"[A-Za-z_][A-Za-z_0-9]*", name):
                raise ValueError(f"{source}: env requires NAME=VALUE")
            result.setdefault(key, {})[name] = text
        else:
            result[key] = value

    if case_flag(result, "host", "true") not in (True, False):
        raise ValueError("host must be true or false")
    if result.get("root", "system") not in ("system", "own"):
        raise ValueError("root must be system or own")

    return result


def case_flag(case, key, default):
    return {"true": True, "false": False}.get(case.get(key, default))


def invoke(command, *, env, timeout, stdin=None, cwd=None):
    return subprocess.run(list(map(str, command)), input=stdin, capture_output=True,
                          env=env, timeout=timeout, cwd=cwd)


def successful(command, **options):
    result = invoke(command, **options)
    if result.returncode:
        raise RuntimeError(f"{shlex.join(list(map(str, command)))}: exit {result.returncode}\n"
                           + result.stdout.decode(errors="replace") + result.stderr.decode(errors="replace"))
    return result


def lines(result):
    """What the system's terminals gave, its standard output's and its console's: their lines' ends as the program wrote them, the CR before each newline that the terminals' output processing adds taken off."""
    return subprocess.CompletedProcess(result.args, result.returncode, result.stdout.replace(b"\r\n", b"\n"), result.stderr.replace(b"\r\n", b"\n"))


def check(result, case, phase):
    expected = (int(case.get("exit-code", "0")), json.loads(case.get("stdout", '""')).encode(),
                json.loads(case.get("stderr", '""')).encode())
    actual = result.returncode, result.stdout, result.stderr
    if "stderr-pattern" in case:
        if "stderr" in case:
            raise ValueError("stderr and stderr-pattern are mutually exclusive")
        pattern = json.loads(case["stderr-pattern"])
        if actual[:2] != expected[:2] or not re.fullmatch(pattern, result.stderr.decode(errors="replace")):
            raise RuntimeError(f"{phase}: got exit/stdout/stderr {actual!r}; expected {expected[:2]!r} and stderr pattern {pattern!r}")
        return
    if actual != expected:
        raise RuntimeError(f"{phase}: got exit/stdout/stderr {actual!r}; expected {expected!r}")


def environment(case):
    env = dict(os.environ)
    # The native AOT and the embedded compiler use the same mode.
    env["W2X_MEMORY_TAIL"] = "strict" if aot_flags(case) == ["--strict"] else "optimized"
    env["W2X_TAIL_PROFILE"] = ""
    return env


def aot_flags(case):
    flags = shlex.split(case.get("aot-flags", ""))
    if flags not in ([], ["--strict"], ["--asan"]):
        raise ValueError("aot-flags must be empty, --strict or --asan")
    return flags


def build(source, prefix, tools):
    case = directives(source)
    timeout = float(case.get("timeout", "60"))
    env = environment(case)
    flags = aot_flags(case)
    args = shlex.split(case.get("args", ""))
    cflags = ["-O2", "-std=c++23", "-fno-exceptions", "-fno-rtti", *shlex.split(case.get("compile-flags", ""))]
    maximum = int(case.get("max-memory", "4294967296"))
    if maximum <= 0 or maximum > 4294967296 or maximum % 65536:
        raise ValueError("max-memory must be a positive multiple of 65536 up to 4294967296")
    link = [flag if not flag.startswith("--max-memory=") else f"--max-memory={maximum}" for flag in LINK]
    prefix.parent.mkdir(parents=True, exist_ok=True)
    module = Path(str(prefix) + ".wasm")

    with tempfile.TemporaryDirectory(prefix="pizdos-case-") as temporary:
        work = Path(temporary)
        if case_flag(case, "host", "true"):
            reference = work / "reference"
            successful([tools["cc"], *tools["host_cflags"], "-x", "c++", *cflags, source,
                        "-o", reference, *tools["host_ldflags"]], env=env, timeout=timeout)
            host_env = dict(env, **{name: value.replace("$ROOT", str(work)) for name, value in case.get("env", {}).items()})
            check(invoke([reference, *args], env=host_env, timeout=timeout, cwd=work), case, "native reference")

        with tarfile.open(tools["cxx_headers"]) as archive:
            archive.extractall(work / "cxx", filter="data")
        obj = work / "case.o"
        successful([tools["cc"], *wasm.CFLAGS, *cflags, "-nostdinc++",
                    "-isystem", work / "cxx/include/c++/v1", "-isystem", tools["c_headers"],
                    "-c", source, "-o", obj], env=env, timeout=timeout)
        successful([tools["ld"], *link, "-o", module, obj, tools["libcxx"], tools["libc"]], env=env, timeout=timeout)

    native = Path(str(prefix) + ".native.o")
    embedded = Path(str(prefix) + ".embedded.o")
    asan = ["--asan"] if flags == ["--asan"] else []
    compiled = successful([tools["aot"], *flags], stdin=module.read_bytes(), env=env, timeout=timeout)
    native.write_bytes(compiled.stdout)
    successful([tools["vm"], *asan, "-o", embedded, module], env=env, timeout=timeout)
    if native.read_bytes() != embedded.read_bytes():
        raise RuntimeError("native and Wasm AOT produced different objects")
    executable = Path(str(prefix) + ".exe")
    executable.write_bytes(exe.pack(module.read_bytes()))
    executable.chmod(0o755)


def disks(case):
    flags = shlex.split(case.get("run-flags", ""))
    asan, named = [], []
    while flags:
        match flags:
            case ["--asan", *rest]:
                asan.append("--asan")
            case [flag, *rest] if flag == "--strace" or flag.startswith("--strace="):
                asan.append(flag)
            case ["--user", user, *rest]:
                asan += ["--user", user]
            case ["--disk", disk, *rest] if re.fullmatch(r"[A-Za-z0-9_.-]+=.+", disk):
                named.append(tuple(disk.split("=", 1)))
            case _:
                raise ValueError(f"run-flags: {shlex.join(flags)}: --asan, --strace, --user and --disk <name>=<path> only")
        flags = rest
    return asan, named


def run(source, prefix, name, tools):
    case = directives(source)
    timeout = float(case.get("timeout", "60"))
    env = environment(case)
    args = shlex.split(case.get("args", ""))
    executable = Path(str(prefix) + ".exe")

    with tempfile.TemporaryDirectory(prefix="pizdos-root-") as directory:
        root = Path(directory)
        (root / "bin").mkdir()
        shutil.copy(executable, root / "bin/check")
        asan, named = disks(case)
        flags = [*asan, *(arg for disk, path in named for arg in ("--disk", f"{disk}={path.replace('$ROOT', str(root))}"))]
        opened = " ".join(f"{3 + i}<>/dev/blk/{disk}/data" for i, (disk, _) in enumerate(named))
        variables = [f"{key}={value}" for key, value in case.get("env", {}).items()]
        if named:
            variables.append("DISKS=" + " ".join(disk for disk, _ in named))
        if case.get("root", "system") == "own":
            setup = f"mkdir /run/root /run/root/bin /run/root/run && cp /tests/unit/{name} /run/root/bin/check"
            program = ["chroot", "/run/root", "/bin/check"]
        else:
            setup, program = "cd /run", ["/tests/unit/" + name]
        script = f"{setup} && " + (f"exec {opened} && " if opened else "") + shlex.join(["exec", "env", *variables, *program, *args]) + " 2>/dev/console"
        result = invoke([tools["vm"], "run", *flags, "--disk", f"image={tools['image']}", "--", tools["rootfs"], "-c", script],
                        env=env, timeout=timeout, stdin=b"")
        check(lines(result), case, "kernel")

    Path(str(prefix) + ".stamp").write_text("native reference, native and Wasm AOT objects, kernel: passed\n")
    print(f"{source.stem}: native/Wasm AOT behaviour agrees", flush=True)


if __name__ == "__main__":
    try:
        match sys.argv[1:]:
            case ["build", source, prefix, tools]:
                build(Path(source).resolve(), Path(prefix).resolve(), json.loads(tools))
            case ["run", source, prefix, name, tools]:
                run(Path(source).resolve(), Path(prefix).resolve(), name, json.loads(tools))
            case ["all", source, prefix, name, tools]:
                build(Path(source).resolve(), Path(prefix).resolve(), json.loads(tools))
                run(Path(source).resolve(), Path(prefix).resolve(), name, json.loads(tools))
            case _:
                sys.exit(__doc__)
    except (RuntimeError, ValueError, subprocess.TimeoutExpired) as error:
        sys.exit(str(error))
