# Compiled objects

Where the kernel keeps the objects it compiles programs to, and what it
keeps of them in memory: what is decided, what is proposed and what is
open.

## Today

- An exec reads the executable whole into the kernel (`programData`),
  hashes it, unpacks its module whole (`unpack`) and gives the compiler
  the module on a memfd. The object comes back whole into a `Capture`.
- `readObject` slices `.text` and `.rodata` out of that buffer, and
  `Code.obj` keeps it for strace's relinks (`traced`). `k.codes` keeps
  every `Code` until the kernel ends.
- So a program executed once costs, to the end, its object in Go's heap
  and its linked code: python's 17.5 MB and 13.4 MB. Its compile adds the
  executable and the module: python's 2.4 and 9.4 MB; llvm's module is
  230 MB, its object 437 MB. Every boot compiles every program again.

## Decided

- An object is a file of the system's own file systems: the system has no
  storage outside itself.
- The objects are kept in `/run` first, on a file system of their own
  later.
- Root is trusted: it can change the system anyway. No one else may
  change an object or what the kernel takes for one.
- `/bin/aot`, the compiler as a file of the image, comes after this.

## Proposed

### The store

- A directory of objects, a file `<key>.o` each. Root hands the kernel
  the directory's descriptor with a call of the kernel's. The kernel
  takes a directory that root owns and no one else may write.
- rootfs hands over `/run/aot` once tmpfs serves `/run`. Later it hands
  over a directory of a disk's file system: the kernel does not change,
  and the objects outlive a boot.
- The key is a sha256 of the executable's module (its PZD1 frame), of
  the compiler, of libc.wasm, which every object links, and of the
  compile's options. The kernel reads the executable in pieces to hash
  it.
- What starts before the store, rootfs, erofs and tmpfs, is compiled
  into memory as now.

### A compile

- The kernel runs the compiler with the module as descriptor 0, an
  answer of the kernel's as 1, libc.wasm as 3 and the store as 4, and
  the key and the options as its arguments.
- Descriptor 0 is a file of the kernel's that unpacks the executable as
  the compiler reads it, reading the executable in pieces. The kernel
  holds zstd's window, 8 MiB at most at exe.py's level 19, and no module.
  zstd stays in the kernel: the compiler allocates no block over 64 KiB.
  A wasm module is its own descriptor.
- The compiler opens `<key>.o`. If it is there, the compiler answers
  with its descriptor and reads nothing. If not, it compiles, writes the
  object into a new file of the store, renames it to `<key>.o` and
  answers with its descriptor.
- The kernel holds no whole executable, module or object. The compiler's
  own memory is as now: the module, and the whole object until it is
  written.
- Two execs of a program at once wait for one compile, as now. A store
  that fails, full or gone, means a compile into memory as now.

### Linking

- The kernel reads `.text` and `.rodata` from the object's descriptor
  straight into the mapping it executes, and the relocations, symbols and
  metadata in pieces.
- It keeps the metadata with the code, and the descriptor instead of the
  bytes. strace's relinks read the file again.
- Code that no process executes is unmapped and its descriptor closed.
  The next exec asks the store again: a lookup and a link, no compile.

### What that leaves

- In Go's heap: no executable, module or object. A compile adds zstd's
  window.
- Mapped: the code of the programs that run, python's 13.4 MB.
- In the store: every object once. While the store is `/run`, that is
  tmpfs's memory (python's 17.5 MB); on a disk it is no memory, and the
  next boot compiles nothing again.

## Open

- The call that hands the kernel the store.
- What bounds the store and what goes first: tmpfs's size, a size of the
  store's own, the objects used longest ago.
- When code that no process executes goes: at once, or past a bound.
- How the compiler answers with a descriptor: as a server answers, with
  lib/protocol in the compiler, or something smaller.
- The temporaries a crash leaves in a store on a disk.
- `vm -o` the same way, with descriptors 0 and 1 on the host's files.
