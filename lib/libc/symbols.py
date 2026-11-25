"""A relocatable wasm object's functions and symbols, and the kernel's
names for those of libc.wasm.

A kernel function a program imports as ("kernel", X) is libc.wasm's
function __kernel_X (bin/vm/wasm_linker.go). FreeBSD's, LLVM's and Arm's
sources define X itself: named() gives each such function the name
__kernel_X beside its own, where lib/c's forwarders (kernel.c) import X of
the kernel by the type the function has; a function of an import's name and
another type is an error, as nothing would say which the import is.
"""
FUNCTION = 0
DATA = 1
SECTION = 3
UNDEFINED = 0x10
LOCAL = 0x2
HIDDEN = 0x4
EXPLICIT_NAME = 0x40
SYMBOL_TABLE = 8


def leb(data, at):
    value, shift = 0, 0

    while True:
        byte = data[at]
        at += 1
        value |= (byte & 0x7f) << shift
        shift += 7

        if byte < 0x80:
            return value, at


def uleb(value):
    out = bytearray()

    while True:
        byte = value & 0x7f
        value >>= 7

        if value:
            out.append(byte | 0x80)
        else:
            out.append(byte)

            return bytes(out)


def string(data, at):
    size, at = leb(data, at)

    return data[at:at + size].decode(), at + size


class Object:
    """An object's sections, the types of its functions, imported first,
    and its symbol table."""

    def __init__(self, data):
        self.data = data
        self.sections = []
        self.types = []
        self.imports = []
        self.functions = []
        self.symbols = []
        self.spans = []
        self.names = {}
        at = 8

        while at < len(data):
            kind = data[at]
            size, start = leb(data, at + 1)
            self.sections.append((kind, at, start, start + size))
            at = start + size

        for kind, at, start, end in self.sections:
            if kind == 1:
                self.types = self.read_types(start)
            elif kind == 2:
                self.read_imports(start)
            elif kind == 3:
                count, at = leb(data, start)

                for _ in range(count):
                    index, at = leb(data, at)
                    self.functions.append(self.types[index])
            elif kind == 0:
                name, at = string(data, start)

                if name == "linking":
                    self.linking = (start, end)
                    self.read_linking(at, end)
                elif name == "name":
                    self.read_names(at, end)

    def read_types(self, at):
        data = self.data
        count, at = leb(data, at)
        types = []

        for _ in range(count):
            start = at
            at += 1

            for _ in range(2):
                n, at = leb(data, at)
                at += n

            types.append(data[start:at])

        return types

    def read_imports(self, at):
        data = self.data
        count, at = leb(data, at)

        for _ in range(count):
            module, at = string(data, at)
            field, at = string(data, at)
            kind = data[at]
            at += 1

            if kind == 0:
                index, at = leb(data, at)
                self.imports.append((module, field, self.types[index]))
            elif kind == 1:
                at += 1
                flags, at = leb(data, at)
                _, at = leb(data, at)

                if flags & 1:
                    _, at = leb(data, at)
            elif kind == 2:
                flags, at = leb(data, at)
                _, at = leb(data, at)

                if flags & 1:
                    _, at = leb(data, at)
            elif kind == 3:
                at += 2
            else:
                raise SystemExit(f"an import of kind {kind}")

    def read_linking(self, at, end):
        data = self.data
        version, at = leb(data, at)

        if version != 2:
            raise SystemExit(f"a linking section of version {version}")

        while at < end:
            kind = data[at]
            size, start = leb(data, at + 1)

            if kind == SYMBOL_TABLE:
                self.table = (at, start, start + size)
                self.read_symbols(start)

            at = start + size

    def read_names(self, at, end):
        """The name section's names of the functions: index -> name."""
        data = self.data

        while at < end:
            kind = data[at]
            size, start = leb(data, at + 1)

            if kind == 1:
                count, here = leb(data, start)

                # wasm-ld's count of a relocatable object's names may be more
                # than the subsection has.
                for _ in range(count):
                    if here >= start + size:
                        break

                    index, here = leb(data, here)
                    self.names[index], here = string(data, here)

            at = start + size

    def read_symbols(self, at):
        data = self.data
        count, at = leb(data, at)
        self.count = (count, at)

        for _ in range(count):
            begin = at
            kind = data[at]
            flags, at = leb(data, at + 1)
            name, index = None, None

            if kind == DATA:
                name, at = string(data, at)

                if not flags & UNDEFINED:
                    for _ in range(3):
                        _, at = leb(data, at)
            elif kind == SECTION:
                _, at = leb(data, at)
            else:
                index, at = leb(data, at)

                if not flags & UNDEFINED or flags & EXPLICIT_NAME:
                    name, at = string(data, at)
                elif kind == FUNCTION:
                    name = self.imports[index][1]

            self.symbols.append((kind, flags, name, index))
            self.spans.append((begin, at))

    def type_of(self, index):
        if index < len(self.imports):
            return self.imports[index][2]

        return self.functions[index - len(self.imports)]

    def defined(self):
        """The functions the object defines for others: name -> index."""
        return {name: index for kind, flags, name, index in self.symbols
                if kind == FUNCTION and not flags & (UNDEFINED | LOCAL)}

    def kernel(self):
        """The kernel's functions the object imports: X -> (the symbol's
        name, the type)."""
        out = {}

        for kind, flags, name, index in self.symbols:
            if kind == FUNCTION and flags & UNDEFINED and self.imports[index][0] == "kernel":
                out[self.imports[index][1]] = (name, self.imports[index][2])

        return out

    def with_import_names(self):
        """The object with its kernel's imports named as their C functions
        are, the name section's: link time optimization leaves a symbol
        of an import the import's name, which a function of the C name it
        has may have too (kernel.c's forwarders)."""
        data = self.data
        at, start, end = self.table
        count, first = self.count
        entries = bytearray()

        for (kind, flags, name, index), (begin, stop) in zip(self.symbols, self.spans):
            if kind == FUNCTION and flags & UNDEFINED and not flags & EXPLICIT_NAME and self.imports[index][0] == "kernel":
                named = self.names.get(index, name)
                entries += bytes([FUNCTION]) + uleb(flags | EXPLICIT_NAME) + uleb(index) + uleb(len(named)) + named.encode()
            else:
                entries += data[begin:stop]

        payload = uleb(count) + entries + data[self.spans[-1][1] if self.spans else first:end]
        table = bytes([SYMBOL_TABLE]) + uleb(len(payload)) + payload
        section_at = next(s for s in self.sections if s[2] == self.linking[0])
        body = data[section_at[2]:at] + table + data[end:section_at[3]]

        return data[:section_at[1]] + bytes([0]) + uleb(len(body)) + body + data[section_at[3]:]

    def with_symbols(self, added):
        """The object with the function symbols added: (name, index)."""
        data = self.data
        at, start, end = self.table
        count, first = self.count
        entries = b"".join(bytes([FUNCTION]) + uleb(HIDDEN) + uleb(index) + uleb(len(name)) + name.encode() for name, index in added)
        payload = uleb(count + len(added)) + data[first:end] + entries
        table = bytes([SYMBOL_TABLE]) + uleb(len(payload)) + payload
        section_at = next(s for s in self.sections if s[2] == self.linking[0])
        body = data[section_at[2]:at] + table + data[end:section_at[3]]

        return data[:section_at[1]] + bytes([0]) + uleb(len(body)) + body + data[section_at[3]:]


def named(implementation, forwarders, sources):
    """libc.wasm's implementation, an object, with the kernel's names of
    its functions that the forwarders' object imports: of those of the
    sources' names, which the forwarders' of the same names are not."""
    code, kernel = Object(implementation), Object(forwarders).kernel()
    defined = {name: index for name, index in code.defined().items() if name in sources or name.startswith("__kernel_")}
    added = []

    for function, (symbol, kind) in sorted(kernel.items()):
        if symbol != "__kernel_" + function:
            continue

        if symbol in defined:
            if code.type_of(defined[symbol]) != kind:
                raise SystemExit(f"{symbol} is not of the type kernel.c imports {function} by")
        elif function in defined:
            if code.type_of(defined[function]) != kind:
                raise SystemExit(f"{function} is not of the type kernel.c imports it by, and there is no {symbol}")

            added.append((symbol, defined[function]))

    return code.with_symbols(added)
