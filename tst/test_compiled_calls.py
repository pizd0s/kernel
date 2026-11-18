#!/usr/bin/env python3
"""The compiler's own code for a function of the kernel is the kernel's function.

    test_compiled_calls.py <vm> <rootfs> <busybox.wasm> <busybox.links>

libc is the ABI: each of its functions is an import of the kernel, and the
kernel has every one. Where a module calls one directly the compiler may
compile the call its own way (lib/str's functions under the kernel's checks, a
comparison in place): that changes what the call costs and nothing else. For
every such function this makes modules that call it three ways,

    direct   the import, called directly: the compiler's code;
    forward  a function of the module that only calls the import, as lib/c's
             are, called directly: the compiler's code too;
    pointer  the import called through a table: the kernel's function;

with arguments the compiler knows, constants, and ones it does not, loaded
from the memory, and holds all three to a model of the kernel's function kept
here:

    results  the same results and the same memory after a sequence of calls;
    traps    the same trap for arguments past the memory's end, a call each.
             With a tail (the mode vm runs in) the memory is its tail longer,
             to the kernel as to the code: there the calls that trap past the
             tail are held, and in the strict mode, which has no tail, all;
    heap     under --asan the same verdict on a call that stays in its blocks
             of the heap and on one that leaves them;
    objects  the object of the direct and of the forward calls has the
             kernel's function nowhere but in the forwarding function, that
             of the pointer has it for the table.

lib/c's functions over these are such forwarding functions in a program the C
compiler made, busybox: there the compiler sees through them.
"""
import os
import random
import struct
import subprocess
import sys
import tempfile
from pathlib import Path

import system

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "dev"))

import wasm_info  # noqa: E402

PAGE = 65536
# The memory's tail (boundSlack).
SLACK = 1024
I32, I64 = 0x7f, 0x7e
PATHS = ("direct", "forward", "pointer")

# The memory of a module: strings and bytes from BASE, buffers to write to
# from SCRATCH, copies of what strdup gave from COPIES, a slot of 8 bytes for
# each call's result from RESULTS and for each argument from ARGUMENTS; at
# the memory's end a string and, the last 16 bytes, bytes with no NUL after
# them.
BASE, SCRATCH, COPIES, RESULTS, ARGUMENTS, DUMPED, TAIL = 1024, 8192, 12800, 16384, 20480, 24576, PAGE - 64
BUFFER = 512
STRINGS = [b"", b"a", b"abc", b"abd", b"abcabcabd", b"hello, world", b"lo", b"xyz", b", ", b"dlrow", b"A" * 40 + b"B", b"ab" * 35 + b"abc",
           b"abc" * 3, bytes(range(1, 256)), b"The quick brown fox jumps over the lazy dog. " * 7, b"\x80\xff\x7f", b"B" + b"A" * 40]
DESTINATIONS = [b"", b"x", b"dest", b"0123456789"]


class Trap(Exception):
    pass


class Kernel:
    """The kernel's view of a memory: what its functions check before they
    read or write, and the ranges they did, touched."""

    def __init__(self, memory):
        self.m = bytearray(memory)
        self.touched = []

    def span(self, at, n):
        if at + n > len(self.m):
            raise Trap

        self.touched.append((at, n))

    def cstr(self, at):
        end = self.m.find(0, at) if at < len(self.m) else -1

        if end < 0:
            raise Trap

        self.touched.append((at, end - at + 1))

        return end - at

    def text(self, at):
        n = self.cstr(at)

        return bytes(self.m[at:at + n])

    def search(self, at, n, c):
        if at > len(self.m):
            raise Trap

        end = min(at + n, len(self.m))
        i = self.m.find(c, at, end)

        if i < 0 and end - at < n:
            raise Trap

        self.touched.append((at, n if i < 0 else i - at + 1))

        return i

    def strnlen(self, at, n):
        i = self.search(at, n, 0)

        return n if i < 0 else i - at

    def limited(self, at, n):
        if at > len(self.m):
            raise Trap

        end = min(at + n, len(self.m))
        i = self.m.find(0, at, end)

        if i >= 0:
            end = i + 1
        elif end - at < n:
            raise Trap

        self.touched.append((at, end - at))

        return bytes(self.m[at:end])

    def copy(self, d, s, n):
        self.span(d, n)
        self.span(s, n)
        self.m[d:d + n] = self.m[s:s + n]

    def put(self, at, byte):
        self.span(at, 1)
        self.m[at] = byte


def u32(x):
    return x & 0xffffffff


def order(a, b):
    return u32((a > b) - (a < b))


def memcpy(k, d, s, n):
    k.copy(d, s, n)

    return d


def mempcpy(k, d, s, n):
    k.copy(d, s, n)

    return u32(d + n)


def bcopy(k, s, d, n):
    k.copy(d, s, n)


def memset(k, d, c, n):
    k.span(d, n)
    k.m[d:d + n] = bytes([c & 255]) * n

    return d


def bzero(k, d, n):
    memset(k, d, 0, n)


def memcmp(k, a, b, n):
    k.span(a, n)
    k.span(b, n)

    return order(k.m[a:a + n], k.m[b:b + n])


def memchr(k, s, c, n):
    return max(k.search(s, n, c & 255), 0)


def strlen(k, s):
    return k.cstr(s)


def copied(k, d, s):
    n = k.cstr(s)
    k.copy(d, s, n + 1)

    return n


def strcpy(k, d, s):
    copied(k, d, s)

    return d


def stpcpy(k, d, s):
    return u32(d + copied(k, d, s))


def strcat(k, d, s):
    copied(k, d + k.cstr(d), s)

    return d


def strcmp(k, a, b):
    return order(k.text(a), k.text(b))


def spanned(k, s, members, reject):
    members, string = k.text(members), k.text(s)

    return next((i for i, c in enumerate(string) if (c in members) == reject), len(string))


def strspn(k, s, members):
    return spanned(k, s, members, False)


def strcspn(k, s, members):
    return spanned(k, s, members, True)


def strpbrk(k, s, members):
    at = s + spanned(k, s, members, True)
    k.span(at, 1)

    return at if k.m[at] else 0


def strnlen(k, s, n):
    return k.strnlen(s, n)


def strstr(k, h, n):
    i = k.text(h).find(k.text(n))

    return 0 if i < 0 else u32(h + i)


def memmem(k, h, hn, n, nn):
    k.span(h, hn)
    k.span(n, nn)
    i = bytes(k.m[h:h + hn]).find(bytes(k.m[n:n + nn]))

    return 0 if i < 0 else u32(h + i)


def strchr(k, s, c):
    string = k.text(s)
    i = string.find(c & 255) if c & 255 else len(string)

    return 0 if i < 0 else u32(s + i)


def strchrnul(k, s, c):
    string = k.text(s)
    i = string.find(c & 255) if c & 255 else -1

    return u32(s + (len(string) if i < 0 else i))


def strncmp(k, a, b, n):
    x, y = k.limited(a, n), k.limited(b, n)
    m = min(len(x), len(y))

    return order(x[:m], y[:m])


def bounded(k, d, s, n):
    length = k.strnlen(s, n)
    k.span(d, n)
    k.span(s, length)
    k.m[d:d + n] = k.m[s:s + length] + bytes(n - length)

    return length


def strncpy(k, d, s, n):
    bounded(k, d, s, n)

    return d


def stpncpy(k, d, s, n):
    return u32(d + bounded(k, d, s, n))


def strdup(k, s):
    """The string's length: the block is the kernel's to choose."""
    return k.strnlen(s, 0xffffffff)


def strncat(k, d, s, n):
    end = d + k.cstr(d)
    length = k.strnlen(s, n)
    k.copy(end, s, length)
    k.put(end + length, 0)

    return d


def strlcpy(k, d, s, n):
    length = k.cstr(s)

    if n:
        taken = min(length, n - 1)
        k.span(d, taken)
        k.m[d:d + taken] = k.m[s:s + taken]
        k.put(d + taken, 0)

    return length


def strlcat(k, d, s, n):
    had = k.strnlen(d, n)
    length = k.cstr(s)

    if had < n:
        taken = min(length, n - had - 1)
        k.span(d + had, taken)
        k.m[d + had:d + had + taken] = k.m[s:s + taken]
        k.put(d + had + taken, 0)

    return u32(had + length)


def strrchr(k, s, c):
    i = (k.text(s) + b"\0").rfind(c & 255)

    return 0 if i < 0 else u32(s + i)


def digit(c):
    return 48 <= c <= 57


def upper(c):
    return 65 <= c <= 90


def lower(c):
    return 97 <= c <= 122


def drawn(c):
    return 0x20 < c < 0x7f


CLASSES = {
    "isalnum": lambda c: digit(c) or upper(c) or lower(c),
    "isalpha": lambda c: upper(c) or lower(c),
    "isascii": lambda c: True,
    "isblank": lambda c: c in (32, 9),
    "iscntrl": lambda c: c < 0x20 or c == 0x7f,
    "isdigit": digit,
    "isgraph": drawn,
    "islower": lower,
    "isprint": lambda c: drawn(c) or c == 32,
    "ispunct": lambda c: drawn(c) and not (digit(c) or upper(c) or lower(c)),
    "isspace": lambda c: c == 32 or 9 <= c <= 13,
    "isupper": upper,
    "isxdigit": lambda c: digit(c) or 65 <= c <= 70 or 97 <= c <= 102,
}


def magnitude(bits):
    return lambda k, x: (1 << bits) - x if x >> (bits - 1) else x


# A function is its type, the compiler's notation (i an i32, l an i64, the
# results after >), the roles of its arguments, by which they are chosen, and
# the kernel's function: s a string, d where to write, D a string to write
# after, p bytes, n a count, c a byte, x an int, X a long long. replaced is
# false for one the compiler leaves a call of the kernel's.
FUNCTIONS = {
    "memcpy": ("iii>i", "dpn", memcpy), "memmove": ("iii>i", "dpn", memcpy), "mempcpy": ("iii>i", "dpn", mempcpy),
    "bcopy": ("iii>", "pdn", bcopy), "memset": ("iii>i", "dcn", memset), "bzero": ("ii>", "dn", bzero),
    "explicit_bzero": ("ii>", "dn", bzero), "memcmp": ("iii>i", "ppn", memcmp), "bcmp": ("iii>i", "ppn", memcmp),
    "memchr": ("iii>i", "pcn", memchr), "memmem": ("iiii>i", "pnpn", memmem), "strlen": ("i>i", "s", strlen),
    "strnlen": ("ii>i", "sn", strnlen), "strcpy": ("ii>i", "ds", strcpy), "stpcpy": ("ii>i", "ds", stpcpy),
    "strcat": ("ii>i", "Ds", strcat), "strncpy": ("iii>i", "dsn", strncpy), "stpncpy": ("iii>i", "dsn", stpncpy),
    "strncat": ("iii>i", "Dsn", strncat), "strlcpy": ("iii>i", "dsn", strlcpy), "strlcat": ("iii>i", "Dsn", strlcat),
    "strcmp": ("ii>i", "ss", strcmp), "strcoll": ("ii>i", "ss", strcmp), "strncmp": ("iii>i", "ssn", strncmp),
    "strspn": ("ii>i", "ss", strspn), "strcspn": ("ii>i", "ss", strcspn), "strpbrk": ("ii>i", "ss", strpbrk),
    "strstr": ("ii>i", "ss", strstr), "strchr": ("ii>i", "sc", strchr), "index": ("ii>i", "sc", strchr),
    "strchrnul": ("ii>i", "sc", strchrnul), "strdup": ("i>i", "s", strdup),
    "tolower": ("i>i", "x", lambda k, c: c + 32 if upper(c) else c), "toupper": ("i>i", "x", lambda k, c: c - 32 if lower(c) else c),
    "toascii": ("i>i", "x", lambda k, c: c & 0x7f), "abs": ("i>i", "x", magnitude(32)), "labs": ("i>i", "x", magnitude(32)),
    "llabs": ("l>l", "X", magnitude(64)),
    **{name: ("i>i", "x", lambda k, c, test=test: int(c < 0x80 and bool(test(c)))) for name, test in CLASSES.items()},
    "strrchr": ("ii>i", "sc", strrchr),
}
KEPT = {"strrchr"}
# memcpy's ranges do not overlap: what it does with ones that do is no
# one's to rely on.
APART = {"memcpy", "mempcpy"}


def image():
    """The memory a module starts with, and the addresses in it by role."""
    memory = bytearray(PAGE)
    at, strings = BASE, []

    for string in STRINGS:
        memory[at:at + len(string)] = string
        strings.append(at)
        at += len(string) + 1

    assert at <= SCRATCH
    memory[SCRATCH:SCRATCH + 8 * BUFFER] = b"\xee" * (8 * BUFFER)
    buffers = [SCRATCH + i * BUFFER for i in range(8)]

    for buffer, string in zip(buffers[4:], DESTINATIONS):
        memory[buffer:buffer + len(string) + 1] = string + b"\0"

    memory[PAGE - 32:PAGE] = b"terminated-tail\0" + b"A" * 16
    ends = [PAGE - 32, PAGE - 20, PAGE - 17, PAGE - 16, PAGE - 8, PAGE - 1, PAGE, PAGE + 1, 70000, 0xfffffff0]
    pools = {
        "s": strings + [s + 1 for s in strings[2:6]] + buffers[4:] + ends,
        "p": strings + buffers[:2] + ends,
        "d": buffers[:4] + [buffers[1] + 1, buffers[2] + 3] + buffers[4:6] + strings[4:6] + ends,
        "D": buffers[4:] * 3 + [PAGE - 32, PAGE - 16, PAGE, 70000],
        "n": [0, 1, 2, 3, 4, 7, 8, 9, 15, 16, 17, 31, 32, 33, 63, 64, 65, 100, 255, 256, 300, 511, 512, 4096, PAGE, 0x7fffffff, 0x80000000, 0xffffffff],
        "c": [0, 0x61, 0x62, 0x63, 0x64, 0x6c, 0x2c, 0x41, 0x42, 0x23, 0xee, 0xff, 0x80, 0x100, 0x161, 0xffffff62, 0xffffffff],
        "x": [0x7f, 0x80, 0xff, 0x100, 0x130, 0x141, 0x161, 0x7fffffff, 0x80000000, 0x80000001, 0xffffffff, 0xffffffd0, 12345, u32(-12345)],
        "X": [0, 1, 5, (1 << 64) - 1, (1 << 64) - 5, 1 << 63, (1 << 63) - 1, (1 << 63) + 1, 1 << 40, (1 << 64) - (1 << 40), 0xffffffff, 1 << 32],
    }

    return bytes(memory), pools


def chosen(name, pools, rng):
    """Arguments for a call of a function: by its roles, a count now and then
    the one that ends its range at the memory's end, or a byte either side."""
    roles = FUNCTIONS[name][1]
    args = [rng.choice(pools[role]) for role in roles]

    for i, role in enumerate(roles):
        near = [a for a, r in zip(args, roles) if r in "sdDp" and PAGE - 64 <= a <= PAGE]

        if role == "n" and near and rng.random() < 0.4:
            args[i] = max(PAGE - rng.choice(near) + rng.choice((-1, 0, 0, 1)), 0)

    return tuple(args)


def overlap(name, args):
    return name in APART and args[2] and abs(args[0] - args[1]) < args[2]


def apart(kernel):
    """Whether what the kernel's function touched is none of what a module
    keeps for us to see, the copies, the results and the arguments."""
    return not any(n and at < DUMPED and at + n > COPIES for at, n in kernel.touched)


def cases(name, memory, pools):
    """The calls of a function: a sequence that goes through, each on the
    memory the ones before left, with the results and the memory at its end,
    and calls that trap, each on the memory a module starts with: those that
    trap whatever tail the memory has, and those that trap without one."""
    rng = random.Random(name)
    _, roles, function = FUNCTIONS[name]
    numbers = roles in ("x", "X")
    candidates = [(x,) for x in list(range(256)) + pools[roles]] if numbers else [chosen(name, pools, rng) for _ in range(600)]
    kernel, sequence, results, traps, copies = Kernel(memory), [], [], {True: [], False: []}, COPIES

    for args in candidates:
        if overlap(name, args):
            continue

        initial = Kernel(memory)

        try:
            function(initial, *args)
        except Trap:
            try:
                function(Kernel(memory + bytes(SLACK)), *args)
                beyond = False
            except Trap:
                beyond = True

            if args not in traps[beyond] and len(traps[beyond]) < 3 and apart(initial):
                traps[beyond].append(args)

        if not numbers and len(sequence) == 64:
            continue

        saved, kernel.touched = bytes(kernel.m), []

        try:
            result, trapped = function(kernel, *args), False
        except Trap:
            trapped = True

        if trapped or not apart(kernel):
            kernel.m[:] = saved
            continue

        if name == "strdup":
            # The copy, where the module moves it for us to see.
            size = result + 1

            if copies + size > RESULTS:
                continue

            kernel.m[copies:copies + size] = kernel.m[args[0]:args[0] + size]
            result, copies = (1, copies, size), copies + size

        sequence.append(args)
        results.append(result)

    return sequence, results, bytes(kernel.m), traps


def leb(value, signed=False):
    result = bytearray()

    while True:
        byte = value & 127
        value >>= 7

        if value == 0 and (not signed or byte < 64) or signed and value == -1 and byte >= 64:
            return bytes(result + bytes([byte]))

        result.append(byte | 128)


def text(value):
    data = value.encode()

    return leb(len(data)) + data


def section(kind, body):
    return bytes([kind]) + leb(len(body)) + body


def vector(items):
    return leb(len(items)) + b"".join(items)


def constant(value, typ=I32):
    bits = 32 if typ == I32 else 64
    value &= (1 << bits) - 1

    if value >= 1 << (bits - 1):
        value -= 1 << bits

    return bytes([0x41 if typ == I32 else 0x42]) + leb(value, True)


def signature(typ):
    params, results = typ.split(">")
    kinds = {"i": I32, "l": I64}

    return b"\x60" + vector([bytes([kinds[c]]) for c in params]) + vector([bytes([kinds[c]]) for c in results])


def module(name, path, body, *, locals_=0, data=(), allocator=False, forwarder=None):
    """A module that runs body, the code of its _start: the function is
    import 0, write import 1 and, where asked for, malloc import 2; called(path)
    is the call of the function by a path. forwarder is the code of the
    function the forward path calls, the plain one if not given."""
    typ = FUNCTIONS[name][0]
    params = len(typ.split(">")[0])
    imports = [(name, 1), ("write", 2)] + ([("malloc", 3)] if allocator else [])
    result = b"\0asm\1\0\0\0" + section(1, vector([b"\x60\0\0", signature(typ), signature("iii>i"), signature("i>i")]))
    result += section(2, vector([text("kernel") + text(import_) + b"\0" + leb(index) for import_, index in imports]))
    result += section(3, vector([b"\1"] * (path == "forward") + [b"\0"]))

    if path == "pointer":
        result += section(4, b"\1\x70\0\1")

    # A heap grows the memory, from where the module says it begins.
    result += section(5, b"\1\0\1" if allocator else b"\1\1\1\1")
    exports = [text("_start") + b"\0" + leb(len(imports) + (path == "forward"))]

    if allocator:
        result += section(6, vector([bytes([I32, 0]) + constant(PAGE) + b"\x0b"]))
        exports.append(text("__heap_base") + b"\3\0")

    result += section(7, vector(exports))

    if path == "pointer":
        result += section(9, vector([b"\0" + constant(0) + b"\x0b" + vector([b"\0"])]))

    codes = []

    if path == "forward":
        code = forwarder or b"\0" + b"".join(b"\x20" + leb(i) for i in range(params)) + b"\x10\0\x0b"
        codes.append(leb(len(code)) + code)

    code = (vector([leb(locals_) + bytes([I32])]) if locals_ else b"\0") + body + b"\x0b"
    codes.append(leb(len(code)) + code)
    result += section(10, vector(codes))

    return result + section(11, vector([b"\0" + constant(at) + b"\x0b" + leb(len(part)) + part for at, part in data]))


def called(name, path):
    allocator = name == "strdup"

    return {"direct": b"\x10\0", "forward": b"\x10" + leb(2 + allocator), "pointer": constant(0) + b"\x11\1\0"}[path]


def arguments(sequence):
    return b"".join(struct.pack("<Q", arg) for args in sequence for arg in args)


def sequenced(name, path, memory, sequence, results, loaded):
    """The module of a sequence of calls: each call's result to its slot,
    then the memory to the standard output. loaded tells the calls whose
    arguments come from the memory."""
    typ = FUNCTIONS[name][0]
    kinds = [I32 if c == "i" else I64 for c in typ.split(">")[0]]
    body = b""
    assert len(sequence) * 8 <= ARGUMENTS - RESULTS and len(sequence) * len(kinds) * 8 <= DUMPED - ARGUMENTS

    for i, (args, result) in enumerate(zip(sequence, results)):
        if loaded(i):
            call = b"".join(constant(ARGUMENTS + 8 * (i * len(kinds) + j)) + (b"\x28\2\0" if kind == I32 else b"\x29\3\0")
                            for j, kind in enumerate(kinds))
        else:
            call = b"".join(constant(arg, kind) for arg, kind in zip(args, kinds))

        call += called(name, path)

        if typ.endswith(">"):
            body += call
            continue

        body += constant(RESULTS + 8 * i) + call

        if name == "strdup":
            # 1 for a block, and the string in it moved to where we look.
            _, at, size = result
            body += b"\x22\0" + constant(0) + b"\x47\xad\x37\3\0" + constant(at) + b"\x20\0" + constant(size) + b"\xfc\x0a\0\0"
        else:
            body += (b"\xad" if typ.endswith("i") else b"") + b"\x37\3\0"

    # The memory, to the standard output.
    body += b"".join(constant(1) + constant(at) + constant(size) + b"\x10\1\x1a" for at, size in ((BASE, DUMPED - BASE), (TAIL, PAGE - TAIL)))
    data = [(BASE, memory[BASE:COPIES]), (ARGUMENTS, arguments(sequence)), (TAIL, memory[TAIL:])]

    return module(name, path, body, locals_=name == "strdup", data=data, allocator=name == "strdup")


def expected(memory, sequence, results):
    """What the module of a sequence writes: the memory the kernel's
    functions left, the results in their slots, the arguments in theirs."""
    memory = bytearray(memory)
    memory[ARGUMENTS:ARGUMENTS + 8 * sum(map(len, sequence))] = arguments(sequence)

    for i, result in enumerate(results):
        value = result[0] if isinstance(result, tuple) else result or 0
        memory[RESULTS + 8 * i:RESULTS + 8 * i + 8] = struct.pack("<Q", value)

    return bytes(memory[BASE:DUMPED] + memory[TAIL:])


def differing(name, sequence, results, want, got):
    """Where a sequence's output is not the model's, for a failure."""
    if len(want) != len(got):
        return f"{len(got)} bytes, want {len(want)}"

    at = next(i for i in range(len(want)) if want[i] != got[i])
    address = BASE + at if at < DUMPED - BASE else TAIL + at - (DUMPED - BASE)

    if RESULTS <= address < ARGUMENTS:
        i = (address - RESULTS) // 8
        value = struct.unpack_from("<Q", got, RESULTS - BASE + 8 * i)[0]

        return f"call {i}, {name}{sequence[i]}: {value:#x}, want {results[i]!r}"

    return f"memory at {address:#x}: {got[at]:#x}, want {want[at]:#x}"


# Calls on blocks of the heap, by function: the blocks' bytes, each a block
# of that size, and the arguments, a number or a block and an offset in it.
# The model says which stay in their blocks.
STRING, BYTES, ROOM = b"abcdefg\0", b"abcdefgh", bytes(8)
PREFIX = b"ab\0" + bytes(5)
A, B = (0, 0), (1, 0)
MOVES = [([ROOM, BYTES], (A, B, 8)), ([ROOM, BYTES + ROOM], (A, B, 9)), ([ROOM + ROOM, BYTES], (A, B, 9)), ([ROOM, BYTES], (A, B, 0))]
FILLS = [([ROOM], (A, 8)), ([ROOM], (A, 9)), ([ROOM], ((0, 8), 0))]
COMPARED = [([BYTES, BYTES], (A, B, 8)), ([BYTES, BYTES + ROOM], (A, B, 9)), ([b"x" + ROOM, b"y" + ROOM], (A, B, 9)), ([b"x" + ROOM, BYTES], (A, B, 9))]
SEARCHED = [([STRING, b"efg\0"], (A, B)), ([BYTES, b"efg\0"], (A, B)), ([STRING, b"efg"], (A, B)), ([STRING, b"\0"], (A, B))]
COPIED = [([ROOM, STRING], (A, B)), ([bytes(7), STRING], (A, B)), ([ROOM + ROOM, BYTES], (A, B)), ([ROOM, b"\0"], ((0, 7), B))]
LIMITED = [([ROOM, BYTES], (A, B, 8)), ([ROOM, STRING], (A, B, 9)), ([ROOM + ROOM, BYTES], (A, B, 9)), ([ROOM, b"abc\0"], (A, B, 8))]
PAIRS = [([STRING, STRING], (A, B)), ([BYTES, STRING], (A, B)), ([STRING, BYTES], (A, B)), ([b"\0", b"\0"], (A, B))]
FOUND = [([STRING], (A, 0x64)), ([STRING], (A, 0)), ([STRING], (A, 0x7a)), ([BYTES], (A, 0x61))]
HEAP = {
    "memcpy": MOVES, "memmove": MOVES, "mempcpy": MOVES, "bcopy": [(blocks[::-1], args) for blocks, args in MOVES],
    "memset": [(blocks, (a, 0x41, n)) for blocks, (a, n) in FILLS], "bzero": FILLS, "explicit_bzero": FILLS,
    "memcmp": COMPARED, "bcmp": COMPARED,
    "memchr": [([BYTES], (A, 0x68, 8)), ([BYTES], (A, 0x61, 100)), ([BYTES], (A, 0x7a, 8)), ([BYTES], (A, 0x7a, 9))],
    "memmem": [([BYTES, b"fgh"], (A, 8, B, 3)), ([BYTES, b"fgh"], (A, 9, B, 3)), ([BYTES, b"fgh"], (A, 8, B, 4)), ([BYTES, b"x"], (A, 0, B, 0))],
    "strlen": [([STRING], (A,)), ([BYTES], (A,)), ([STRING], ((0, 7),))],
    "strnlen": [([BYTES], (A, 8)), ([BYTES], (A, 9)), ([STRING], (A, 100)), ([BYTES], (A, 0))],
    "strcpy": COPIED, "stpcpy": COPIED,
    "strcat": [([PREFIX, b"cdefg\0"], (A, B)), ([PREFIX, b"cdefgh\0"], (A, B)), ([BYTES, b"\0"], (A, B)), ([PREFIX, b"cdefg"], (A, B))],
    "strncpy": LIMITED, "stpncpy": LIMITED,
    "strncat": [([PREFIX, BYTES], (A, B, 5)), ([PREFIX, BYTES], (A, B, 6)), ([PREFIX, BYTES + ROOM], (A, B, 9)), ([PREFIX, b"c\0"], (A, B, 100))],
    "strlcpy": [([ROOM, STRING], (A, B, 8)), ([bytes(4), STRING], (A, B, 8)), ([ROOM, BYTES], (A, B, 4)), ([bytes(1), STRING], ((0, 1), B, 0)),
                ([bytes(4), STRING], (A, B, 4))],
    "strlcat": [([PREFIX, STRING], (A, B, 8)), ([PREFIX, STRING], (A, B, 9)), ([PREFIX, BYTES], (A, B, 8)), ([BYTES, STRING], (A, B, 8)),
                ([BYTES, STRING], (A, B, 9))],
    "strcmp": PAIRS, "strcoll": PAIRS,
    "strncmp": [([BYTES, BYTES], (A, B, 8)), ([BYTES, BYTES], (A, B, 9)), ([STRING, STRING], (A, B, 100)), ([b"x" + BYTES, b"y"], (A, B, 1))],
    "strspn": PAIRS, "strcspn": SEARCHED, "strpbrk": SEARCHED, "strstr": SEARCHED,
    "strchr": FOUND, "index": FOUND, "strchrnul": FOUND, "strdup": [([STRING], (A,)), ([BYTES], (A,)), ([b"\0"], (A,))],
}
PLACED = 4096


def verdict(name, blocks, args):
    """Whether a call on blocks of the heap stays in them, by the model: the
    blocks apart in a memory of zeros."""
    kernel = Kernel(bytes(PAGE))
    placed = [PLACED + 256 * i for i in range(len(blocks))]

    for at, block in zip(placed, blocks):
        kernel.m[at:at + len(block)] = block

    FUNCTIONS[name][2](kernel, *(placed[arg[0]] + arg[1] if isinstance(arg, tuple) else arg for arg in args))

    return all(n == 0 or any(at <= a and a + n <= at + len(block) for at, block in zip(placed, blocks)) for a, n in kernel.touched)


def heaped(name, path, blocks, args):
    """The module of a call on blocks of the heap: each from malloc and
    filled, then the call."""
    typ = FUNCTIONS[name][0]
    body = b""

    for i, block in enumerate(blocks):
        body += constant(len(block)) + b"\x10\2\x21" + leb(i)
        body += b"".join(b"\x20" + leb(i) + constant(byte) + b"\x3a\0" + leb(j) for j, byte in enumerate(block))

    body += b"".join(b"\x20" + leb(arg[0]) + constant(arg[1]) + b"\x6a" if isinstance(arg, tuple) else constant(arg) for arg in args)
    call = {"direct": b"\x10\0", "forward": b"\x10\3", "pointer": constant(0) + b"\x11\1\0"}[path]

    return module(name, path, body + call + (b"" if typ.endswith(">") else b"\x1a"), locals_=len(blocks), allocator=True)


def references(path):
    """The undefined symbols an object's functions refer to, by function."""
    data = path.read_bytes()
    offset, size, count = struct.unpack_from("<Q", data, 0x28)[0], *struct.unpack_from("<HH", data, 0x3a)
    sections = [struct.unpack_from("<IIQQQQIIQQ", data, offset + i * size) for i in range(count)]
    table = next(s for s in sections if s[1] == 2)
    names = sections[table[6]][4]
    symbols = [struct.unpack_from("<IBBHQQ", data, table[4] + i * 24) for i in range(table[5] // 24)]

    def named(symbol):
        return data[names + symbol[0]:data.index(0, names + symbol[0])].decode()

    result = {}

    for relocations in sections:
        # Those of the code.
        if relocations[1] != 4 or not sections[relocations[7]][2] & 4:
            continue

        functions = [s for s in symbols if s[1] & 15 == 2 and s[3] == relocations[7]]

        for i in range(relocations[5] // 24):
            at, info, _ = struct.unpack_from("<QQq", data, relocations[4] + i * 24)
            symbol = symbols[info >> 32]

            if symbol[3] == 0:
                inside = [named(f) for f in functions if f[4] <= at < f[4] + f[5]]
                assert len(inside) == 1, (path, at, inside)
                result.setdefault(inside[0], set()).add(named(symbol))

    return result


def kernels(refs, name):
    """The functions of an object that call the kernel's function of a name."""
    symbol = "w2o.import.kernel." + name.replace("_", "_5F") + "."

    return {function for function, symbols in refs.items() if any(s.startswith(symbol) for s in symbols)}


def objects(vm, work, name, modules):
    """The object of each path's module has the kernel's function where a
    pointer finds it and nowhere else, for one the compiler has code for."""
    refs = {path: compiled(vm, work, data) for path, data in modules.items()}
    allocator = name == "strdup"
    forward, start = f"f{2 + allocator}", f"f{3 + allocator}"

    if name in KEPT:
        # A call of the kernel's, and through the forwarding function a call
        # of the kernel's too, not of that function.
        assert kernels(refs["direct"], name) == {forward}, (name, refs["direct"])
        assert kernels(refs["forward"], name) == {forward, start}, (name, refs["forward"])
    else:
        assert not kernels(refs["direct"], name), (name, refs["direct"])
        assert kernels(refs["forward"], name) == {forward}, (name, refs["forward"])

    assert kernels(refs["pointer"], name), (name, refs["pointer"])

    if allocator:
        # Its block is the kernel's malloc's.
        assert kernels(refs["direct"], "malloc"), (name, refs["direct"])


def compiled(vm, work, data):
    source, output = work / "module.wasm", work / "module.o"
    source.write_bytes(data)
    result = subprocess.run([vm, "-o", str(output), str(source)], capture_output=True, timeout=60)
    assert result.returncode == 0, result.stderr

    return references(output)


def others(vm, rootfs, work):
    """A function that does anything but call the import with its own
    arguments is no forwarding function: it is called as it is, and the call
    in it is the compiler's code. strdup with no malloc among the imports
    stays the kernel's."""
    first, second = BASE, BASE + 16
    data = [(first, b"hello, world\0"), (second, b"lo\0")]
    call = constant(first) + constant(second) + b"\x10\2"
    # The function's code, what it gives for the two strings, and whether it
    # forwards: strcspn of them is 2, of them the other way round 0.
    functions = {
        "plain": (None, 2, True),
        "swapped": (b"\0\x20\1\x20\0\x10\0\x0b", 0, False),
        "same": (b"\0\x20\0\x20\0\x10\0\x0b", 0, False),
        "more": (b"\0\x20\0\x20\1\x10\0\x41\1\x6a\x0b", 3, False),
        "local": (b"\1\1\x7f\x20\0\x20\1\x10\0\x0b", 2, False),
    }
    programs = []

    for label, (code, result, forwards) in functions.items():
        program = module("strcspn", "forward", call + constant(result) + b"\x47\x04\x40\0\x0b", data=data, forwarder=code)
        assert kernels(compiled(vm, work, program), "strcspn") == ({"f2"} if forwards else set()), label
        programs.append(program)

    for label, result in zip(functions, system.run(vm, rootfs, [("calls", program, [], b"") for program in programs])):
        assert result == (0, b"", b""), (label, result)

    assert kernels(compiled(vm, work, module("strdup", "direct", constant(first) + b"\x10\0\x1a", data=data)), "strdup")


def forwarded(program):
    """The imports of the kernel that a function of a module only calls with
    its own arguments, as the compiler tells one (objCodeForward): no local,
    each parameter in its order, the call, the end."""
    imports, types, bodies = [], [], []

    for kind, reader in wasm_info.sections(program):
        if kind == 2:
            for _ in range(reader.uleb()):
                module, name, what = reader.name(), reader.name(), reader.byte()

                if what == 0:
                    imports.append((module, name))

                wasm_info.skip_import_description(reader, what)
        elif kind == 3:
            types = [reader.uleb() for _ in range(reader.uleb())]
        elif kind == 10:
            for _ in range(reader.uleb()):
                size = reader.uleb()
                bodies.append(reader.data[reader.at:reader.at + size])
                reader.at += size

    assert len(types) == len(bodies) > 0
    result = set()

    for body in bodies:
        # The call is where the parameters end, however many there are.
        at = 1

        while body[at:at + 2] == bytes([0x20, at // 2]):
            at += 2

        if body[0] == 0 and body[at] == 0x10 and body[-1] == 0x0b:
            reader = wasm_info.Reader(body[at + 1:-1])
            index = reader.uleb()

            if reader.at == len(reader.data) and index < len(imports) and imports[index][0] == "kernel":
                result.add(imports[index][1])

    return {name for _, name in imports}, result


def main():
    vm, rootfs, busybox = sys.argv[1:4]
    # What busybox calls of these, it calls through lib/c's forwarding
    # functions.
    imported, forwarding = forwarded(Path(busybox).read_bytes())
    ours = set(FUNCTIONS) & imported
    assert len(ours) >= 20 and ours <= forwarding, (sorted(ours), sorted(ours - forwarding))
    memory, pools = image()
    programs, checks, heaps, verdicts = [], [], [], []
    calls = traps = 0

    with tempfile.TemporaryDirectory(prefix="pizdos-compiled-calls-") as directory:
        work = Path(directory)

        for name, (_, roles, _) in FUNCTIONS.items():
            sequence, results, after, trapping = cases(name, memory, pools)
            numbers = roles in ("x", "X")
            assert len(sequence) >= 32 and (numbers or trapping[True] and trapping[False]), (name, len(sequence), trapping)
            want = expected(after, sequence, results)
            modules = {path: sequenced(name, path, memory, sequence, results, lambda i: i % 2) for path in PATHS}
            objects(vm, work, name, modules)
            calls += len(sequence)
            traps += len(trapping[True]) + len(trapping[False])

            for path in PATHS:
                programs.append(modules[path])
                checks.append((name, path, sequence, results, want))

                for beyond, args in ((beyond, args) for beyond in trapping for args in trapping[beyond]):
                    for loaded in (False, True):
                        programs.append(sequenced(name, path, memory, [args], [(1, COPIES, 1) if name == "strdup" else 0], lambda i: loaded))
                        checks.append((name, path, args, None, beyond))

            inside = [verdict(name, blocks, args) for blocks, args in HEAP.get(name, [])]
            assert numbers or name in KEPT or True in inside and False in inside, (name, inside)

            for (blocks, args), stays in zip(HEAP.get(name, []), inside):
                for path in PATHS:
                    heaps.append(heaped(name, path, blocks, args))
                    verdicts.append((name, path, blocks, args, stays))

        others(vm, rootfs, work)

    for mode in ("strict", "optimized"):
        # A trap's check is (name, path, arguments, None, whether past any tail).
        ran = [(check, program) for check, program in zip(checks, programs) if mode == "strict" or check[3] is not None or check[4]]
        env = dict(os.environ, W2X_MEMORY_TAIL=mode, W2X_TAIL_PROFILE="")
        outcomes = system.run(vm, rootfs, [("calls", program, [], b"") for _, program in ran], env=env)

        for ((name, path, sequence, results, want), _), (status, out, err) in zip(ran, outcomes):
            if results is None:
                assert status == 139 and out == b"" and b"out of bounds memory access" in err, (mode, name, path, sequence, want, status, out[:64], err)
            else:
                assert status == 0 and err == b"", (mode, name, path, status, err)
                assert out == want, (mode, name, path, differing(name, sequence, results, want, out))

    ran = system.run(vm, rootfs, [("calls", program, [], b"") for program in heaps], flags=("--asan",))

    for (name, path, blocks, args, stays), (status, out, err) in zip(verdicts, ran):
        if stays:
            assert (status, out, err) == (0, b"", b""), (name, path, blocks, args, status, err)
        else:
            assert status in (134, 139) and b"asan: " in err, (name, path, blocks, args, status, err)

    print(f"compiled calls: {len(FUNCTIONS)} functions by three paths each as the kernel's: {calls} calls, {traps} traps, "
          f"{len(verdicts) // len(PATHS)} calls on the heap under asan")


if __name__ == "__main__":
    main()
