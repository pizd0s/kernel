#!/usr/bin/env python3
"""Calls through function pointers of another type, for a linked module.

    fpcast.py <module.wasm> <output.wasm>

C code calls a function through a pointer of a type that is not the
function's own: GLib frees a list with a function of one parameter called
as one of two, a callback that returns nothing is called for a result.
The native ABIs let that pass, a parameter too many is in a register
nobody reads; WebAssembly does not, call_indirect checks the type and
traps. This rewrites the module as binaryen's FuncCastEmulation does for
Emscripten's EMULATE_FUNCTION_POINTER_CASTS, so that such a call does what
it does natively:

  - every function of an element segment is in the table as a thunk of
    one type, the most parameters any of them or any call takes, all i64,
    and an i64 result: the thunk gives the function the parameters it has,
    each converted from i64, and returns its result as i64, zero for none;
  - every call_indirect is a call of a thunk of its type, which converts
    the arguments to i64, fills the rest with zeros, calls the table with
    the one type and converts the result back.

A parameter the caller did not pass is zero and one too many is dropped; a
value of another type is its bits, a float's in an integer's place and the
reverse, an i64 cut to an i32. A function pointer is still the function's
place in the table, and a direct call is untouched.

The thunks are appended, so every function keeps its index; the name
section names them. The code moves, so the sections that hold code
offsets, debug information, go.
"""
import sys
from pathlib import Path

I32, I64, F32, F64 = 0x7F, 0x7E, 0x7D, 0x7C
LETTERS = {I32: "i", I64: "j", F32: "f", F64: "d"}

# What follows an opcode: nothing, numbers of LEB128, or its own form.
NONE, LEB, LEB2, MEMARG, TABLE, CONST32, CONST64, SELECT, INDIRECT, PREFIX, BAD = range(11)
IMMEDIATES = [BAD] * 256

for op in (0x00, 0x01, 0x05, 0x0B, 0x0F, 0x1A, 0x1B, 0xD1, *range(0x45, 0xC5)):
    IMMEDIATES[op] = NONE

for op in (0x02, 0x03, 0x04, 0x0C, 0x0D, 0x10, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x3F, 0x40, 0x41, 0x42, 0xD0, 0xD2):
    IMMEDIATES[op] = LEB

for op in range(0x28, 0x3F):
    IMMEDIATES[op] = MEMARG

IMMEDIATES[0x0E], IMMEDIATES[0x11], IMMEDIATES[0x1C] = TABLE, INDIRECT, SELECT
IMMEDIATES[0x43], IMMEDIATES[0x44], IMMEDIATES[0xFC] = CONST32, CONST64, PREFIX

# The numbers after an opcode of the 0xFC prefix.
PREFIXED = {**{op: 0 for op in range(8)}, 8: 2, 9: 1, 10: 2, 11: 1, 12: 2, 13: 1, 14: 2, 15: 1, 16: 1, 17: 1}


def uleb(value):
    out = bytearray()

    while True:
        byte = value & 0x7F
        value >>= 7

        if value:
            out.append(byte | 0x80)
        else:
            out.append(byte)

            return bytes(out)


def vector(items):
    return uleb(len(items)) + b"".join(items)


def sized(payload):
    return uleb(len(payload)) + payload


class Reader:
    def __init__(self, data, at=0):
        self.data = data
        self.at = at

    def byte(self):
        value = self.data[self.at]
        self.at += 1

        return value

    def uleb(self):
        result = 0
        shift = 0

        while True:
            byte = self.data[self.at]
            self.at += 1
            result |= (byte & 0x7F) << shift
            shift += 7

            if not byte & 0x80:
                return result

    def skip_leb(self):
        while self.data[self.at] & 0x80:
            self.at += 1

        self.at += 1

    def take(self, length):
        value = self.data[self.at:self.at + length]
        self.at += length

        return value

    def done(self):
        return self.at >= len(self.data)


def sections(module):
    """The sections of a module, (id, payload) in order."""
    if module[:8] != b"\0asm\1\0\0\0":
        raise SystemExit("fpcast: not a WebAssembly module")

    reader = Reader(module, 8)
    found = []

    while not reader.done():
        kind = reader.byte()
        found.append((kind, reader.take(reader.uleb())))

    return found


def types_of(payload):
    """The function types of a type section, (params, results) of value types."""
    reader = Reader(payload)
    types = []

    for _ in range(reader.uleb()):
        if reader.byte() != 0x60:
            raise SystemExit("fpcast: a type that is not a function's")

        params = list(reader.take(reader.uleb()))
        types.append((params, list(reader.take(reader.uleb()))))

    return types


def imported_functions(payload):
    """The type of every function the module imports, in order."""
    reader = Reader(payload)
    found = []

    for _ in range(reader.uleb()):
        reader.take(reader.uleb())
        reader.take(reader.uleb())
        kind = reader.byte()

        if kind == 0:
            found.append(reader.uleb())
        elif kind == 1:
            reader.byte()
            limits(reader)
        elif kind == 2:
            limits(reader)
        elif kind == 3:
            reader.take(2)
        else:
            raise SystemExit(f"fpcast: an import of kind {kind}")

    return found


def limits(reader):
    flags = reader.byte()
    reader.uleb()

    if flags & 1:
        reader.uleb()


def constant(reader):
    """Skips a constant expression, an offset's."""
    start = reader.at

    while True:
        op = reader.byte()

        if op == 0x0B:
            return reader.data[start:reader.at]

        kind = IMMEDIATES[op]

        if kind == LEB:
            reader.skip_leb()
        elif kind == CONST32:
            reader.take(4)
        elif kind == CONST64:
            reader.take(8)
        elif kind != NONE:
            raise SystemExit(f"fpcast: opcode {op:#x} in a constant expression")


def elements(payload, thunk):
    """An element section with thunk(function) in place of every function of
    a segment that fills a table."""
    reader = Reader(payload)
    out = []

    for _ in range(reader.uleb()):
        flags = reader.uleb()

        if flags > 3:
            raise SystemExit("fpcast: an element segment of expressions")

        head = uleb(flags)

        if flags == 2:
            head += uleb(reader.uleb())

        if flags in (0, 2):
            head += constant(reader)

        if flags != 0:
            head += bytes([reader.byte()])

        functions = [reader.uleb() for _ in range(reader.uleb())]

        if flags != 3:
            functions = [thunk(function) for function in functions]

        out.append(head + vector([uleb(function) for function in functions]))

    return vector(out)


def rewritten(body, caller):
    """A function body with caller(type) called in place of every
    call_indirect of that type."""
    data = body
    size = len(data)
    reader = Reader(data)

    for _ in range(reader.uleb()):
        reader.skip_leb()
        reader.at += 1

    at = reader.at
    out = None
    copied = 0
    immediates = IMMEDIATES

    while at < size:
        op = data[at]
        kind = immediates[op]
        at += 1

        if kind == NONE:
            continue

        if kind == LEB:
            while data[at] & 0x80:
                at += 1

            at += 1
        elif kind == MEMARG:
            flags = data[at]

            for _ in range(3 if flags & 0x40 and flags < 0x80 else 2):
                while data[at] & 0x80:
                    at += 1

                at += 1
        elif kind == INDIRECT:
            reader.at = at
            kind_at = at - 1
            index = reader.uleb()

            if reader.uleb() != 0:
                raise SystemExit("fpcast: a call through a table that is not the first")

            at = reader.at

            if out is None:
                out = bytearray()

            out += data[copied:kind_at]
            out.append(0x10)
            out += uleb(caller(index))
            copied = at
        elif kind == CONST32:
            at += 4
        elif kind == CONST64:
            at += 8
        elif kind == TABLE:
            reader.at = at

            for _ in range(reader.uleb() + 1):
                reader.skip_leb()

            at = reader.at
        elif kind == SELECT:
            reader.at = at
            reader.take(reader.uleb())
            at = reader.at
        elif kind == PREFIX:
            reader.at = at
            sub = reader.uleb()

            if sub not in PREFIXED:
                raise SystemExit(f"fpcast: opcode 0xfc {sub}")

            for _ in range(PREFIXED[sub]):
                reader.skip_leb()

            at = reader.at
        else:
            raise SystemExit(f"fpcast: opcode {op:#x}")

    if at != size:
        raise SystemExit("fpcast: a function body ends in the middle of an instruction")

    if out is None:
        return data

    out += data[copied:]

    return bytes(out)


def to_i64(kind):
    """Instructions that make a value of a type an i64 of its bits."""
    return {I32: b"\xad", I64: b"", F32: b"\xbc\xad", F64: b"\xbd"}[kind]


def from_i64(kind):
    return {I32: b"\xa7", I64: b"", F32: b"\xa7\xbe", F64: b"\xbf"}[kind]


def checked(signature):
    params, results = signature

    if len(results) > 1 or any(kind not in LETTERS for kind in params + results):
        raise SystemExit("fpcast: a function in a table with a type the thunks cannot pass")

    return signature


def callee_thunk(function, signature):
    """The one type's function that calls function with its own parameters."""
    params, results = checked(signature)
    code = b"".join(b"\x20" + uleb(i) + from_i64(kind) for i, kind in enumerate(params))
    code += b"\x10" + uleb(function)
    code += to_i64(results[0]) if results else b"\x42\x00"

    return sized(b"\x00" + code + b"\x0b")


def caller_thunk(signature, one, width):
    """The function of a call's type, and the table's index after, that calls
    the table with the one type."""
    params, results = checked(signature)
    code = b"".join(b"\x20" + uleb(i) + to_i64(kind) for i, kind in enumerate(params))
    code += b"\x42\x00" * (width - len(params))
    code += b"\x20" + uleb(len(params)) + b"\x11" + uleb(one) + b"\x00"
    code += from_i64(results[0]) if results else b"\x1a"

    return sized(b"\x00" + code + b"\x0b")


def function_type(params, results):
    return b"\x60" + vector([bytes([kind]) for kind in params]) + vector([bytes([kind]) for kind in results])


def letters(signature):
    params, results = signature

    return "".join(LETTERS[kind] for kind in params) + "_" + ("".join(LETTERS[kind] for kind in results) or "v")


def names_with(payload, added):
    """A name section with the names of added, {function: name}, after its own."""
    reader = Reader(payload)
    title = reader.take(reader.uleb())
    out = sized(title)
    known = {}
    seen = False

    while not reader.done():
        kind = reader.byte()
        data = reader.take(reader.uleb())

        if kind != 1:
            out += bytes([kind]) + sized(data)

            continue

        seen = True
        sub = Reader(data)
        entries = []

        for _ in range(sub.uleb()):
            index = sub.uleb()
            name = sub.take(sub.uleb())
            known[index] = name
            entries.append(uleb(index) + sized(name))

        for index in sorted(added):
            entries.append(uleb(index) + sized(added[index](known)))

        out += b"\x01" + sized(vector(entries))

    return out if seen else payload


def convert(module):
    parts = sections(module)
    by_id = {kind: payload for kind, payload in parts if kind != 0}
    types = types_of(by_id.get(1, b"\x00"))
    imports = imported_functions(by_id[2]) if 2 in by_id else []
    reader = Reader(by_id.get(3, b"\x00"))
    defined = [reader.uleb() for _ in range(reader.uleb())]
    signatures = [types[index] for index in imports + defined]
    base = len(signatures)

    # The thunks, after the module's functions: a table's function's first,
    # numbered as the element segments name them, then a call's by its
    # type, as the bodies meet them.
    callees, callers = {}, {}

    def callee(function):
        if function not in callees:
            callees[function] = base + len(callees)

        return callees[function]

    table = elements(by_id[9], callee) if 9 in by_id else None
    first_caller = base + len(callees)

    def caller(index):
        if index not in callers:
            callers[index] = first_caller + len(callers)

        return callers[index]

    reader = Reader(by_id.get(10, b"\x00"))
    bodies = [rewritten(reader.take(reader.uleb()), caller) for _ in range(reader.uleb())]

    if not callers and not callees:
        return module

    width = max([len(signatures[function][0]) for function in callees] + [len(types[index][0]) for index in callers])
    one = len(types)
    new_types = [function_type([I64] * width, [I64])]
    new_functions, new_bodies, names = [], [], {}

    for function in callees:
        new_functions.append(one)
        new_bodies.append(callee_thunk(function, signatures[function]))
        names[callees[function]] = lambda known, function=function: b"fpcast$" + known.get(function, b"f%d" % function)

    for index in callers:
        params, results = types[index]
        new_functions.append(one + len(new_types))
        new_types.append(function_type(params + [I32], results))
        new_bodies.append(caller_thunk(types[index], one, width))
        names[callers[index]] = lambda known, index=index: b"fpcast$call$" + letters(types[index]).encode()

    reader = Reader(by_id.get(1, b"\x00"))
    count = reader.uleb()
    type_section = uleb(count + len(new_types)) + by_id.get(1, b"\x00")[reader.at:] + b"".join(new_types)
    reader = Reader(by_id.get(3, b"\x00"))
    count = reader.uleb()
    function_section = uleb(count + len(new_functions)) + by_id.get(3, b"\x00")[reader.at:] + b"".join(uleb(index) for index in new_functions)
    code_section = uleb(len(bodies) + len(new_bodies)) + b"".join(sized(body) for body in bodies) + b"".join(new_bodies)
    out = bytearray(module[:8])

    for kind, payload in parts:
        if kind == 0:
            reader = Reader(payload)
            title = reader.take(reader.uleb())

            if title.startswith((b".debug", b"reloc.", b"linking")):
                continue

            if title == b"name":
                payload = names_with(payload, names)
        elif kind == 1:
            payload = type_section
        elif kind == 3:
            payload = function_section
        elif kind == 9:
            payload = table
        elif kind == 10:
            payload = code_section

        out += bytes([kind]) + sized(payload)

    return bytes(out)


def main():
    if len(sys.argv) != 3:
        raise SystemExit(__doc__)

    Path(sys.argv[2]).write_bytes(convert(Path(sys.argv[1]).read_bytes()))


if __name__ == "__main__":
    main()
