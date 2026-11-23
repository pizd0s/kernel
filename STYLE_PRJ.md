# Project style settings

Per-project settings that the shared [STYLE.md](STYLE.md) delegates here.

- **Languages.** `lib/c` is C. Go code (`bin/vm`, `tst/wazero`, later
  `bin/kernel`, `bin/rootfs`) follows the monorepo Go style and carries its
  own `STYLE.md` next to its `go.mod`, as `trustme/bin/cargo` does. C++ for the system's own programs will follow
  STYLE.md as written once it appears.
- **Headers.** Every header except `assert.h` starts with `#pragma once`.
- **Names.** Functions, types and macros of the libc ABI keep their external
  spelling (`__stdio_write`, `struct stat`, `O_CLOEXEC`). Our own helpers and
  locals follow STYLE.md: `lowerCamelCase`, `UpperCamelCase` types.
- **Anonymous namespaces.** Unlike STYLE.md, an anonymous namespace holds
  types only, as declarations, so that types of one name in a program's
  several files do not collide; their methods are defined out of line after
  it closes. A file's own functions and variables are `static` at file
  scope, outside any namespace. An operator or a hash of such a type that a
  template finds by argument-dependent lookup is the type's method: at file
  scope that lookup does not find it. What a program's files share may live
  in a namespace of its own, as bin/tools/lib.h's `tools`: the header
  declares in it, its .cpp defines the members qualified, and the files
  that use it say `using namespace tools;`.
- **Kernel imports.** Every libc function is the kernel's: lib/c's own
  function of the name calls the import, declared `__KERNEL(name)` under
  the C name `__kernel_<name>` (`src/kernel.c`, or `__INLINE` in its
  header); the import name is spelled out so the linker can tell an import
  from a missing definition. A program keeps only these forwarders, its
  entry with the variables POSIX has it define (`src/crt.c`) and the
  compiler's runtime. The forwarders are weak, so that a program's own
  function of a name is its own. The kernel implements each import in Go or
  in libc.wasm (`lib/libc`), whose C defines `__kernel_<name>`, the
  import's prototype as it is, never the C name, and calls the C library
  through its own copy of kernel.c; a function of FreeBSD's, LLVM's or
  Arm's sources there gets the name beside its own (`symbols.py`). The
  kernel links what a program imports of libc.wasm, and what that reaches,
  into the program: the two share the imports, each has its own
  forwarders. A function that takes a structure takes its size after it,
  and serves each size it has known.
- **POSIX's variables.** What POSIX has as a variable is the program's
  variable (`src/crt.c`). The program's entry gives the kernel all their
  places in one structure, `struct __start` (`src/kernel.h`), and does
  nothing else: libc.wasm's start keeps it, tells the kernel's Go its
  places, runs the constructors and main, and libc.wasm's exports are the
  functions the kernel calls the program's through (`lib/libc/start.c`).
  What POSIX has as a macro is a macro over a kernel function (`errno`,
  `stdin`). A variable POSIX has not is not in lib/c.
- **Third-party code.** Sources are fetched as release tarballs, a big one
  cut down to what is built (`dev/slice.py`), and never edited in place; what they need from us lives beside their build
  (lib/libc's `freebsd/` and `llvm/` headers). Code
  whose own error the system cannot live with is patched as it is built, by
  patch files beside its build script, each with its reason at its head and
  fit to send to its authors: GLib and mc (`bin/mc/*.patch`), LLVM libc and
  FreeBSD (`lib/libc/llvm/patches`, `lib/libc/freebsd/patches`). So is code
  that uses what POSIX has not and lib/c has dropped (`bin/busybox/patches`).
- **Formatter.** None yet: `style.py` covers C++ only.
