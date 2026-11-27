"""The objects the kernel's compiler makes (bin/aot/aot.cpp's writeObject),
as tests read them: the image of the code as the kernel lays it in memory,
the header and the GOT in pages before the text, the GOT's slots last, the
text, the constants from the page after it; then the Go runtime's tables of
the text, the functions' names, the GOT's symbols and the description.

    read(path) -> Object(text, got_at, got, functions)

text is the code; the GOT is at got_at from the text's start, before it,
its k-th slot the k-th symbol of got; functions are the text's, each its
name, start and end, in the text's order.
"""
import collections
from pathlib import Path
import struct

Object = collections.namedtuple("Object", "text got_at got functions")


def read(path):
    data = Path(path).read_bytes()
    assert data[:4] == b"PZO1" and struct.unpack_from("<Q", data, 8)[0] == len(data), path
    head, text = struct.unpack_from("<QQ", data, 16)
    tables, names, got, _ = (data[at:at + size] for at, size in (struct.unpack_from("<QQ", data, 40 + 16 * k) for k in range(4)))
    # The tables' header: the functions and the end of the last, then where
    # the tables of values, the entries and the records are.
    count, end, _, _, entries_at, records_at = struct.unpack_from("<6I", tables)
    entries = [struct.unpack_from("<II", tables, entries_at + 8 * k) for k in range(count + 1)]
    assert entries[count][0] == end, path

    def name(record):
        at = struct.unpack_from("<I", tables, records_at + record + 4)[0]

        return names[at:names.index(0, at)].decode()

    functions = [(name(record), start, entries[k + 1][0]) for k, (start, record) in enumerate(entries[:count])]
    symbols = [s.decode() for s in got.split(b"\0")[:-1]]

    return Object(data[head:head + text], -8 * len(symbols), symbols, functions)
