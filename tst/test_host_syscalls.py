#!/usr/bin/env python3
"""Propagate real Linux syscall failures of vm's own: its read of the
program it starts, and the host's calls of the WASI commands it runs on
their own. The guest's calls are the kernel's, none of the host's.

    test_host_syscalls.py <vm> <native-aot> <filter>

A seccomp filter restricts only each child VM process. It returns selected error numbers for
selected syscalls, leaving the parent and later commands unaffected.
"""
import errno
import os
from pathlib import Path
import socket
import stat
import subprocess
import sys
import tempfile

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "dev"))

from exe import pack  # noqa: E402


def leb(value, signed=False):
    result = bytearray()
    while True:
        byte = value & 127
        value >>= 7
        if value == 0 and (not signed or byte < 64) or signed and value == -1 and byte >= 64:
            return bytes(result + bytes([byte]))
        result.append(byte | 128)


def section(kind, data):
    return bytes([kind]) + leb(len(data)) + data


def wasi_clock(name):
    signature = b"\x60\3\x7f\x7e\x7f\1\x7f" if name == "clock_time_get" else b"\x60\2\x7f\x7f\1\x7f"
    owner, function = b"wasi_snapshot_preview1", name.encode()
    module = b"\0asm\1\0\0\0" + section(1, b"\2" + signature + b"\x60\0\0")
    module += section(2, b"\1" + leb(len(owner)) + owner + leb(len(function)) + function + b"\0\0")
    module += section(3, b"\1\1") + section(5, b"\1\0\1") + section(7, b"\1\6_start\0\1")
    body = b"\0\x41\0" + (b"\x42\0" if name == "clock_time_get" else b"")
    body += b"\x41\x80\10\x10\0\x41\x1d\x47\x04\x40\0\x0b\x0b"
    return module + section(10, b"\1" + leb(len(body)) + body)


def wasi_descriptor(name, error=29, filetype=None):
    owner, function = b"wasi_snapshot_preview1", name.encode()
    module = b"\0asm\1\0\0\0" + section(1, b"\2\x60\2\x7f\x7f\1\x7f\x60\0\0")
    module += section(2, b"\1" + leb(len(owner)) + owner + leb(len(function)) + function + b"\0\0")
    module += section(3, b"\1\1") + section(5, b"\1\0\1") + section(7, b"\1\6_start\0\1")
    body = b"\0\x41\0\x41\x80\10\x10\0\x41" + leb(error) + b"\x47\x04\x40\0\x0b"
    if filetype is not None:
        offset = 0 if name == "fd_fdstat_get" else 16
        body += b"\x41\x80\10\x2d\0" + leb(offset) + b"\x41" + leb(filetype) + b"\x47\x04\x40\0\x0b"
    body += b"\x0b"
    return module + section(10, b"\1" + leb(len(body)) + body)


def wasi_directory(path, name, filetype):
    owner, function = b"wasi_snapshot_preview1", b"fd_readdir"
    module = b"\0asm\1\0\0\0" + section(1, b"\3\x60\5\x7f\x7f\x7f\x7e\x7f\1\x7f"
                                              b"\x60\11\x7f\x7f\x7f\x7f\x7f\x7e\x7e\x7f\x7f\1\x7f\x60\0\0")
    module += section(2, b"\2" + leb(len(owner)) + owner + leb(len(function)) + function + b"\0\0"
                      + leb(len(owner)) + owner + b"\11path_open\0\1")
    module += section(3, b"\1\2") + section(5, b"\1\0\1") + section(7, b"\1\6_start\0\2")
    integer = lambda n: b"\x41" + leb(n, True)
    path = str(path).encode()
    code = b"".join(integer(n) for n in (3, 0, 1024, len(path), 2)) + b"\x42" + leb(1 << 14, True) + b"\x42\0"
    code += integer(0) + integer(3072) + b"\x10\1\x04\x40\0\x0b" + integer(3072) + b"\x28\2\0\x21\3"
    code += b"\x20\3" + integer(4096) + integer(60000) + b"\x42\0" + integer(2048) + b"\x10\0\x04\x40\0\x0b"
    code += integer(4096) + b"\x21\0" + integer(4096) + integer(2048) + b"\x28\2\0\x6a\x21\1"
    code += b"\x02\x40\x03\x40\x20\0\x20\1\x4f\x0d\1"
    code += b"\x20\0\x28\2\x10" + integer(4) + b"\x46\x04\x40\x20\0\x28\2\x18"
    code += integer(int.from_bytes(name, "little")) + b"\x46\x04\x40\x20\0\x2d\0\x14" + integer(filetype)
    code += b"\x47\x04\x40\0\x0b" + integer(1) + b"\x21\2\x0b\x0b"
    code += b"\x20\0\x20\0\x28\2\x10" + integer(24) + b"\x6a\x6a\x21\0\x0c\0\x0b\x0b"
    code += b"\x20\2\x45\x04\x40\0\x0b\x0b"
    body = b"\1\4\x7f" + code
    return module + section(10, b"\1" + leb(len(body)) + body) + section(11, b"\1\0" + integer(1024) + b"\x0b" + leb(len(path)) + path)


def kernel_program():
    """A program of the kernel that ends at once: it imports getpid."""
    module = b"\0asm\1\0\0\0" + section(1, b"\2\x60\0\1\x7f\x60\0\0") + section(2, b"\1\6kernel\6getpid\0\0")
    return module + section(3, b"\1\1") + section(7, b"\1\6_start\0\1") + section(10, b"\1\5\0\x10\0\x1a\x0b")


def main():
    vm, native, wrapper = sys.argv[1:]
    env = dict(os.environ, W2X_MEMORY_TAIL="strict", W2X_TAIL_PROFILE="")
    with tempfile.TemporaryDirectory(prefix="pizdos-host-syscalls-") as directory:
        work = Path(directory)
        (work / "directory").mkdir()
        os.mkfifo(work / "directory/pipe")
        program = work / "program"
        program.write_bytes(pack(kernel_program()))
        compiled, embedded = work / "native.o", work / "embedded.o"
        # Startup reads the module using fstat and pread, as exec does.
        # Returning zero from a suppressed pread models EOF after its
        # recorded size.
        for number, error, diagnostic in ((5, errno.EIO, b"input/output error"),
                                           (17, errno.EIO, b"input/output error"), (17, 0, b"exec format error")):
            result = subprocess.run([wrapper, f"{error}/{number}", "--", vm, "run", "--", str(program)],
                                    capture_output=True, env=env, timeout=30)
            assert result.returncode == 1 and result.stdout == b"" and diagnostic in result.stderr and b"panic:" not in result.stderr, (number, error, result)
        for name, number in (("clock_time_get", 228), ("clock_res_get", 229)):
            source = work / (name + ".wasm")
            data = wasi_clock(name)
            source.write_bytes(data)
            result = subprocess.run([native, "--strict"], input=data, capture_output=True, env=env, timeout=60)
            assert result.returncode == 0, result
            compiled.write_bytes(result.stdout)
            result = subprocess.run([vm, "-o", str(embedded), str(source)], capture_output=True, env=env, timeout=60)
            assert result.returncode == 0 and embedded.read_bytes() == compiled.read_bytes(), result
            result = subprocess.run([wrapper, f"{errno.EIO}/{number}", "--", vm, "run", "--", str(source)],
                                    capture_output=True, env=env, timeout=30)
            assert (result.returncode, result.stdout, result.stderr) == (0, b"", b""), (name, result)
        for name in ("fd_fdstat_get", "fd_filestat_get"):
            source = work / (name + ".wasm")
            data = wasi_descriptor(name, error=0, filetype=6)
            source.write_bytes(data)
            result = subprocess.run([native, "--strict"], input=data, capture_output=True, env=env, timeout=60)
            assert result.returncode == 0, result
            compiled.write_bytes(result.stdout)
            result = subprocess.run([vm, "-o", str(embedded), str(source)], capture_output=True, env=env, timeout=60)
            assert result.returncode == 0 and embedded.read_bytes() == compiled.read_bytes(), result
            reader, writer = socket.socketpair()
            with reader, writer:
                result = subprocess.run([vm, "run", "--", str(source)], stdin=reader, capture_output=True, env=env, timeout=30)
                assert (result.returncode, result.stdout, result.stderr) == (0, b"", b""), (name, result)
        devices = [path for path in sorted(Path("/dev").iterdir()) if stat.S_ISBLK(path.lstat().st_mode)]
        if devices:
            for name in ("fd_fdstat_get", "fd_filestat_get"):
                source = work / (name + "-block.wasm")
                data = wasi_descriptor(name, error=0, filetype=1)
                source.write_bytes(data)
                result = subprocess.run([native, "--strict"], input=data, capture_output=True, env=env, timeout=60)
                assert result.returncode == 0, result
                compiled.write_bytes(result.stdout)
                result = subprocess.run([vm, "-o", str(embedded), str(source)], capture_output=True, env=env, timeout=60)
                assert result.returncode == 0 and embedded.read_bytes() == compiled.read_bytes(), result
                # O_PATH permits fstat without opening the device for I/O.
                fd = os.open(devices[0], os.O_PATH | os.O_CLOEXEC)
                try:
                    result = subprocess.run([vm, "run", "--", str(source)], stdin=fd, capture_output=True, env=env, timeout=30)
                    assert (result.returncode, result.stdout, result.stderr) == (0, b"", b""), (name, result)
                finally:
                    os.close(fd)
        for directory, name, filetype in ((Path("/dev"), b"null", 2), (work / "directory", b"pipe", 0)):
            source = work / "directory.wasm"
            data = wasi_directory(directory, name, filetype)
            source.write_bytes(data)
            result = subprocess.run([native, "--strict"], input=data, capture_output=True, env=env, timeout=60)
            assert result.returncode == 0, result
            compiled.write_bytes(result.stdout)
            result = subprocess.run([vm, "-o", str(embedded), str(source)], capture_output=True, env=env, timeout=60)
            assert result.returncode == 0 and embedded.read_bytes() == compiled.read_bytes(), result
            result = subprocess.run([vm, "run", "--", str(source)], capture_output=True, env=env, timeout=30)
            assert (result.returncode, result.stdout, result.stderr) == (0, b"", b""), (directory, result)
        # ENOTTY has no WASI preview1 equivalent and must become generic I/O error.
        for preset, error, names in (("stdin-stat", errno.EIO, ("fd_fdstat_get", "fd_filestat_get")),
                                     ("stdin-flags", errno.ENOTTY, ("fd_fdstat_get", "fd_fdstat_set_flags"))):
            for name in names:
                source = work / (name + ".wasm")
                data = wasi_descriptor(name)
                source.write_bytes(data)
                result = subprocess.run([native, "--strict"], input=data, capture_output=True, env=env, timeout=60)
                assert result.returncode == 0, result
                compiled.write_bytes(result.stdout)
                result = subprocess.run([vm, "-o", str(embedded), str(source)], capture_output=True, env=env, timeout=60)
                assert result.returncode == 0 and embedded.read_bytes() == compiled.read_bytes(), result
                result = subprocess.run([wrapper, f"{error}/{preset}", "--", vm, "run", "--", str(source)],
                                        capture_output=True, env=env, timeout=30)
                assert (result.returncode, result.stdout, result.stderr) == (0, b"", b""), (name, preset, result)
    print("host syscalls: vm's read of its program and the WASI system, file, polling and clock errors propagate through both AOTs")


if __name__ == "__main__":
    main()
