//@ compile-flags: -O2
//@ stdout: "heap pages: ok\n"

// The heap grows the memory by the pages of 4 KiB it hands out, and a size
// class takes a slab of the fewest pages that waste at most an eighth:
// after a block of each of the 40 classes the memory ends less than 1 MiB
// past the heap's base, where a page of 64 KiB for each class, or growth
// by segments of 2 MiB, took more. Blocks of a run each grow it as far as
// they need, and once freed their pages serve again: as many blocks again
// take less than half their bytes more.

#include <assert.h>
#include <malloc.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#ifdef __wasm__
extern "C" unsigned char __heap_base;

static size_t memoryEnd() {
    return __builtin_wasm_memory_size(0) * 65536;
}
#endif

static size_t classSize(int i) {
    if (i < 16) {
        return size_t(i + 1) * 16;
    }
    const size_t base = size_t(1) << (8 + (i - 16) / 4);
    return base + base / 4 * size_t((i - 16) % 4 + 1);
}

int main() {
    enum { classes = 40 };
    void* small[classes];
    for (int i = 0; i < classes; ++i) {
        small[i] = malloc(classSize(i));
        assert(small[i]);
        memset(small[i], i, classSize(i));
    }
#ifdef __wasm__
    const size_t base = reinterpret_cast<uintptr_t>(&__heap_base);
    assert(memoryEnd() < base + 1024 * 1024);
#endif
    // The blocks of a class whose slab takes more than a page lie in its
    // later pages too, blocks as its first one is.
    for (int i = 32; i < classes; ++i) {
        void* blocks[8];
        for (int k = 0; k < 8; ++k) {
            blocks[k] = malloc(classSize(i));
            assert(blocks[k] && malloc_usable_size(blocks[k]) >= classSize(i));
            memset(blocks[k], k, classSize(i));
        }
        for (int k = 7; k >= 0; --k) {
            const unsigned char* bytes = static_cast<const unsigned char*>(blocks[k]);
            assert(bytes[0] == k && bytes[classSize(i) - 1] == k);
            free(blocks[k]);
        }
    }
    enum { runs = 64 };
    void* big[runs];
    for (int i = 0; i < runs; ++i) {
        big[i] = malloc(20000);
        assert(big[i]);
        memset(big[i], 0x5a, 20000);
    }
#ifdef __wasm__
    const size_t grown = memoryEnd();
    assert(grown >= base + runs * 20000);
#endif
    for (int i = 0; i < runs; ++i) {
        free(big[i]);
    }
    for (int i = 0; i < runs; ++i) {
        big[i] = malloc(20000);
        assert(big[i]);
        memset(big[i], 0xa5, 20000);
    }
#ifdef __wasm__
    assert(memoryEnd() < grown + runs * 20000 / 2);
#endif
    for (int i = 0; i < runs; ++i) {
        free(big[i]);
    }
    for (int i = 0; i < classes; ++i) {
        const unsigned char* bytes = static_cast<const unsigned char*>(small[i]);
        for (size_t k = 0; k < classSize(i); ++k) {
            assert(bytes[k] == i);
        }
        free(small[i]);
    }
    puts("heap pages: ok");
}
