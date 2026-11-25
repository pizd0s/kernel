#!/usr/bin/env python3
"""How the kernel writes a traced call (bin/vm/traced_imports.go,
callFormats) against lib/c's declarations of the kernel's functions
(lib/c/src/kernel.c, as clang has it for wasm32): a function's format is
a letter for its result and one for each argument the kernel gets, by its
C type: s a string, const char* and a result of char*; c a struct ctl*,
whose request's bytes and descriptors the trace shows; p another pointer,
a va_list among them; o a mode_t; f a floating number; d any other number;
v no result.

The kernel's table must be the declarations' of its Go functions
(bin/vm/kernel.go's table of them): the test tells what differs and prints
the table as it should be. Every other function declared is libc.wasm's,
its __kernel_ and the name, and none is both's.

    test_call_formats.py <clang> <source root> <libc.wasm>
"""
import re
import subprocess
import sys
import textwrap
from pathlib import Path

DECLARATION = re.compile(r'([^;{}]*?)\b__kernel_\w+\s*\(([^;{}]*?)\)\s*(?:__attribute__\(\(__\w+__\)\)\s*)*'
                         r'__attribute__\(\(__import_module__\("kernel"\), __import_name__\("(\w+)"\)\)\)')


def split(params):
    """The parameters of a list, a comma inside parentheses none between them."""
    out, depth, at = [], 0, 0

    for i, c in enumerate(params):
        depth += (c == "(") - (c == ")")

        if c == "," and depth == 0:
            out.append(params[at:i])
            at = i + 1

    return [p.strip() for p in [*out, params[at:]] if p.strip()]


def kind(text, result=False):
    text = re.sub(r"__attribute__\(\(.*?\)\)|\b(__restrict|restrict|extern|static)\b", " ", text)
    text = " ".join(text.split())

    if text == "void":
        return "v"

    if re.fullmatch(r"(const )?struct ctl ?\*( ?\w+)?", text):
        return "c"

    if "*" in text or "[" in text or "va_list" in text or re.search(r"\b\w*jmp_buf\b", text):
        string = r"char ?\*( ?\w+)?" if result else r"const char ?\*( ?\w+)?"

        return "s" if re.fullmatch(("(const )?" if result else "") + string, text) else "p"

    if re.search(r"\bmode_t\b", text):
        return "o"

    if re.search(r"\b(float|double)\b", text):
        return "f"

    return "d"


def formats(clang, root):
    include, source = root / "lib/c/include", root / "lib/c/src"
    result = subprocess.run([clang, "--target=wasm32-unknown-unknown", "-Xclang", "-mlong-double-64", "-nostdinc", "-isystem", str(include),
                             "-I", str(source), "-std=c99", "-D_XOPEN_SOURCE=700", "-D_GNU_SOURCE", "-w", "-E", "-P", str(source / "kernel.c")],
                            capture_output=True, text=True, timeout=300)
    assert result.returncode == 0, result.stderr[:2000]
    out = {}

    for ret, params, name in DECLARATION.findall(result.stdout):
        params = split(params)
        out[name] = kind(ret, result=True) + "".join(kind(p) for p in params if p != "void")

    return out


def go_functions(root):
    """The kernel's functions of its Go."""
    text = (root / "bin/vm/kernel.go").read_text()
    table = text[text.index("func (k *Kernel) functionTable()"):]

    return set(re.findall(r'^\t\t"(\w+)":', table[:table.index("\n}\n")], re.M))


def libc_wasm(root, path):
    """The kernel's functions libc.wasm defines."""
    sys.path.insert(0, str(root / "lib/libc"))

    import symbols

    return {name[len("__kernel_"):] for name in symbols.Object(Path(path).read_bytes()).defined() if name.startswith("__kernel_")}


def main():
    clang, root = sys.argv[1], Path(sys.argv[2])
    declared, go, libc = formats(clang, root), go_functions(root), libc_wasm(root, sys.argv[3])
    nowhere, twice = sorted(set(declared) - go - libc), sorted(go & libc)
    assert not nowhere, f"declared, and neither the kernel's Go nor libc.wasm's: {nowhere}"
    assert not twice, f"both the kernel's Go and libc.wasm's: {twice}"
    want = {name: kind for name, kind in declared.items() if name in go}
    go = (root / "bin/vm/traced_imports.go").read_text()
    block = re.search(r"callFormats\s*=.*?strings\.Fields\(`(.*?)`\)", go, re.S)
    assert block, "traced_imports.go has no table of formats"
    have = dict(word.split(":") for word in block.group(1).split())
    wrong = sorted(name for name in want.keys() | have.keys() if want.get(name) != have.get(name))

    if wrong:
        for name in wrong:
            print(f"{name}: the kernel's {have.get(name)}, the declaration's {want.get(name)}")

        print("\nthe table as it should be:\n")
        print(textwrap.indent("\n".join(textwrap.wrap(" ".join(f"{k}:{v}" for k, v in sorted(want.items())), 130)), "\t\t"))
        sys.exit(1)

    print(f"call formats: the kernel's {len(have)} are the declarations'")


if __name__ == "__main__":
    main()
