//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "resource interfaces: ok\n"

// getrandom fills its buffer, other bytes each time, and refuses flags it
// has none of. posix_madvise takes the five advices with no change to the
// memory, POSIX_MADV_DONTNEED's too, and refuses others with EINVAL; lib/c's
// alone, as musl has it not: its ENOMEM for memory the process has not.

#include <assert.h>
#include <errno.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>
#include <sys/random.h>
#include <sys/resource.h>
#include <unistd.h>

int main() {
    unsigned char a[64] = {}, b[64] = {};
    assert(getrandom(a, sizeof a, 0) == sizeof a && getrandom(b, sizeof b, GRND_NONBLOCK) == sizeof b && memcmp(a, b, sizeof a) != 0);
    assert(getrandom(a, sizeof a, 0x80) == -1 && errno == EINVAL);
    assert(getrandom(a, 0, 0) == 0);

    long page = sysconf(_SC_PAGESIZE);
    void* memory = nullptr;
    assert(posix_memalign(&memory, page, 2 * page) == 0);
    memset(memory, 'x', 2 * page);
    int advices[] = {POSIX_MADV_NORMAL, POSIX_MADV_SEQUENTIAL, POSIX_MADV_RANDOM, POSIX_MADV_WILLNEED, POSIX_MADV_DONTNEED};
    for (int advice : advices) {
        assert(posix_madvise(memory, 2 * page, advice) == 0);
    }
    for (long i = 0; i < 2 * page; i++) {
        assert(static_cast<char*>(memory)[i] == 'x');
    }
    assert(posix_madvise(memory, page, 99) == EINVAL);
#ifdef __wasm__
    assert(posix_madvise(reinterpret_cast<void*>(uintptr_t(0xffff0000)), page, POSIX_MADV_NORMAL) == ENOMEM);
#endif
    free(memory);
    printf("resource interfaces: ok\n");
}
