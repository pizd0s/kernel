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
- **Kernel imports.** A libc function the kernel implements is lib/c's own
  function calling the import, declared `__KERNEL(name)` under the C name
  `__kernel_<name>` (`src/kernel.c`, or `__INLINE` in its header); the
  import name is spelled out so the linker can tell an import from a
  missing definition. No other declaration has the attribute, but libc.wasm's
  own imports of the kernel (`lib/libc/libc.h`). The forwarders are weak, so
  that a program's own function of a name is its own. The kernel implements
  each import in Go or in libc.wasm (`lib/libc`), whose C defines
  `__kernel_<name>`, the import's prototype as it is, never the C name, and
  calls the C library through its own copy of kernel.c. The kernel links
  what a program imports of libc.wasm, and what that reaches, into the
  program: the two share the imports, each has its own forwarders.
- **Third-party code.** Sources are fetched as release tarballs, a big one
  cut down to what is built (`dev/slice.py`), and never edited in place; what they need from us lives beside their build
  (lib/c's `freebsd/` and `llvm/` headers, `lib/c/src`, and `lib/libc/freebsd.h`). Code
  whose own error the system cannot live with is patched as it is built, by
  patch files beside its build script, each with its reason at its head and
  fit to send to its authors: GLib and mc (`bin/mc/*.patch`), LLVM libc and
  FreeBSD (`lib/c/llvm/patches`, `lib/c/freebsd/patches`). So is FreeBSD's
  code in libc.wasm that calls what libc.wasm does not have yet
  (`lib/libc/patches`), each patch gone when it has it.
- **Formatter.** None yet: `style.py` covers C++ only.
