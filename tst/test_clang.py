#!/usr/bin/env python3
"""clang in the system: LLVM's multicall program, as /bin/llvm with clang,
clang++ and wasm-ld naming it, of the image of the system and the
toolchain, with lib/c's and lib/cxx's headers and libraries, compiles and
links a C and a C++ program in /run, its driver running cc1 and the linker
as processes of their own, and the shell runs what they link, packed by
pzd into executables: the kernel compiles each module once, LLVM's for all
its processes.

    test_clang.py <vm> <rootfs> <clang.img>
"""
import shlex
import subprocess
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent / "dev"))

import wasm  # noqa: E402

C = r"""
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int compare(const void* a, const void* b) {
    return *(const int*)a - *(const int*)b;
}

int main(int argc, char** argv) {
    int v[] = {5, 3, 9, 1, 7};
    qsort(v, 5, sizeof v[0], compare);
    char buf[64];
    snprintf(buf, sizeof buf, "%d %d %d %d %d %.3f", v[0], v[1], v[2], v[3], v[4], 22.0 / 7);
    printf("%s: %s %zu\n", argv[0], buf, strlen(buf));
    return 0;
}
"""

CXX = r"""
#include <algorithm>
#include <format>
#include <iostream>
#include <map>
#include <string>
#include <vector>

int main() {
    std::vector<std::string> words{"pear", "apple", "fig"};
    std::sort(words.begin(), words.end());
    std::map<std::string, size_t> lengths;
    for (const auto& w : words) {
        lengths[w] = w.size();
    }
    std::cout << std::format("{} {} {}", words[0], lengths["pear"], words.size()) << std::endl;
    return 0;
}
"""

LINK = ["-nostdlib", "-Wl,--stack-first", "-Wl,--max-memory=4294967296",
        "-Wl,--export=__heap_base", "-Wl,--export=__stack_pointer", "-Wl,--gc-sections"]


def main():
    vm, rootfs, image = sys.argv[1:4]
    c = ["clang", *wasm.CFLAGS, "-isystem", "/usr/include", "-O2", "/run/src/hello.c", "-o", "/run/src/hello.wasm", *LINK, "/usr/lib/libc.a"]
    cxx = ["clang++", *wasm.CFLAGS, "-nostdinc++", "-fno-exceptions", "-std=c++23", "-isystem", "/usr/include/c++/v1",
           "-isystem", "/usr/include", "-O2", "/run/src/hello.cpp", "-o", "/run/src/hello-cxx.wasm", *LINK, "/usr/lib/libc++.a", "/usr/lib/libc.a"]
    script = (f"set -e\nmkdir -p /run/src /run/bin\ncat > /run/src/hello.c <<'C_EOF'\n{C}C_EOF\n"
              f"cat > /run/src/hello.cpp <<'CXX_EOF'\n{CXX}CXX_EOF\nclang --version\necho ---\n{shlex.join(c)}\n{shlex.join(cxx)}\n"
              "pzd /run/src/hello.wasm /run/bin/hello\npzd /run/src/hello-cxx.wasm /run/bin/hello-cxx\nPATH=/run/bin:$PATH\nhello\nhello-cxx\n")
    r = subprocess.run([vm, "run", "--disk", f"image={image}", "--", rootfs, "-c", script], capture_output=True, text=True, timeout=1200)
    version, _, ran = r.stdout.partition("---\n")
    version = version.splitlines()[0] if version else ""
    want = "hello: 1 3 5 7 9 3.143 15\napple 4 3\n"

    if r.returncode != 0 or not version.startswith("clang version 23.") or ran != want:
        print(f"FAIL clang: exit {r.returncode}, version {version!r}, ran {ran!r}, want {want!r}\n{r.stderr[-3000:]}")
        sys.exit(1)

    print(f"clang: {version}; built hello and hello-cxx, which ran as expected")


if __name__ == "__main__":
    main()
