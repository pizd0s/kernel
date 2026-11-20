//@ compile-flags: -O2
//@ stdout: "heap pages: ok\n"

// The heap grows the memory by the pages it hands out, not by its
// segments of 2 MiB: after a program's first allocations, a page for each
// of 16 size classes, the memory ends less than a segment past the heap's
// base. Blocks of a page each grow it as far as they need, and once freed
// their pages serve again with no growth.

#include <assert.h>
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

int main() {
    void* small[16];
    for (int i = 0; i < 16; ++i) {
        small[i] = malloc(16 + 16 * i);
        assert(small[i]);
        memset(small[i], i, 16 + 16 * i);
    }
#ifdef __wasm__
    const size_t base = reinterpret_cast<uintptr_t>(&__heap_base);
    assert(memoryEnd() < base + 2 * 1024 * 1024);
#endif
    enum { runs = 64 };
    void* big[runs];
    for (int i = 0; i < runs; ++i) {
        big[i] = malloc(20000);
        assert(big[i]);
        memset(big[i], 0x5a, 20000);
    }
#ifdef __wasm__
    const size_t grown = memoryEnd();
    assert(grown >= base + runs * 65536);
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
    assert(memoryEnd() == grown);
#endif
    for (int i = 0; i < runs; ++i) {
        free(big[i]);
    }
    for (int i = 0; i < 16; ++i) {
        const unsigned char* bytes = static_cast<const unsigned char*>(small[i]);
        for (int k = 0; k < 16 + 16 * i; ++k) {
            assert(bytes[k] == i);
        }
        free(small[i]);
    }
    puts("heap pages: ok");
}
