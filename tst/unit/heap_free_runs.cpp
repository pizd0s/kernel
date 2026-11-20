//@ compile-flags: -O2
//@ stdout: "heap free runs: ok\n"

// A block of 16 pages takes 16 free pages a segment of 32 has in a row,
// wherever the segment is among those with free pages. A block of 4096
// bytes is a slab of one page, so the pages of such blocks fill segments
// whole: one of them gets 16 pages in a row freed, and then sixteen others
// every other page, freed after it, so it is the last segment that got free
// pages. A block of 64 KiB then lies in those 16 pages, the one place it
// fits, and the heap adds no segment for it.

#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#ifdef __wasm__
extern "C" unsigned char __heap_base;
#endif

int main() {
    enum { page = 4096, perSegment = 32, segment = perSegment * page, spread = 16, count = 64 * perSegment };
    static char* pages[count];
    for (int i = 0; i < count; ++i) {
        pages[i] = static_cast<char*>(malloc(page));
        assert(pages[i]);
        memset(pages[i], i & 0xff, page);
    }
#ifdef __wasm__
    const uintptr_t base = (reinterpret_cast<uintptr_t>(&__heap_base) + page - 1) & ~uintptr_t(page - 1);
    static int owned[count];
    for (int i = 0; i < count; ++i) {
        const uintptr_t at = reinterpret_cast<uintptr_t>(pages[i]);
        assert(at % page == 0 && at >= base && (at - base) / segment < count);
        ++owned[(at - base) / segment];
    }
    // Segments all 32 pages of which are blocks of this program, the last
    // block's own segment aside: its slab stays the class's current one.
    const uintptr_t last = (reinterpret_cast<uintptr_t>(pages[count - 1]) - base) / segment;
    int whole[spread + 1];
    int found = 0;
    for (int s = 0; s < count && found <= spread; ++s) {
        if (owned[s] == perSegment && uintptr_t(s) != last) {
            whole[found++] = s;
        }
    }
    assert(found == spread + 1);
    auto slot = [&](int i) {
        const uintptr_t at = reinterpret_cast<uintptr_t>(pages[i]) - base;
        return int(at / segment) == whole[0] ? int(at % segment / page) : -1;
    };
    for (int i = 0; i < count; ++i) {
        if (slot(i) >= 8 && slot(i) < 24) {
            free(pages[i]);
            pages[i] = nullptr;
        }
    }
    for (int k = 1; k <= spread; ++k) {
        for (int i = 0; i < count; ++i) {
            const uintptr_t at = pages[i] ? reinterpret_cast<uintptr_t>(pages[i]) - base : 0;
            if (pages[i] && int(at / segment) == whole[k] && at % segment / page % 2) {
                free(pages[i]);
                pages[i] = nullptr;
            }
        }
    }
    char* block = static_cast<char*>(malloc(16 * page));
    assert(block);
    assert(reinterpret_cast<uintptr_t>(block) == base + uintptr_t(whole[0]) * segment + 8 * page);
    memset(block, 0x5a, 16 * page);
    free(block);
#endif
    for (int i = 0; i < count; ++i) {
        if (pages[i]) {
            assert(pages[i][0] == char(i & 0xff) && pages[i][page - 1] == char(i & 0xff));
            free(pages[i]);
        }
    }
    puts("heap free runs: ok");
}
