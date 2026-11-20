//@ compile-flags: -O2
//@ stdout: "stack blocks: ok\n"

// The memory's stack grows in blocks of 64 KiB: a frame that does not fit
// below the stack pointer in its block goes to the top of another block,
// which the kernel takes from the heap and keeps for the next time. Frames
// holding arrays of their own recurse far past the first block, in frames
// of constant and of variable size and one larger than a block, and find
// their arrays as they left them; a call at the edge of a block repeats
// without taking memory; longjmp leaves the blocks to the next descent; a
// handler recurses past its alternate stack; a child of fork returns
// through every block it inherited.

#include <alloca.h>
#include <assert.h>
#include <setjmp.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/wait.h>
#include <unistd.h>

static unsigned mix(unsigned x) {
    return x * 2654435761u ^ x >> 13;
}

static size_t pages() {
#ifdef __wasm__
    return __builtin_wasm_memory_size(0);
#else
    return 0;
#endif
}

__attribute__((noinline)) static unsigned descend(int depth, unsigned seed) {
    volatile unsigned a[64];
    for (int i = 0; i < 64; ++i) {
        a[i] = mix(seed + i);
    }
    unsigned r = depth > 0 ? descend(depth - 1, a[depth % 64]) : seed;
    for (int i = 0; i < 64; ++i) {
        if (a[i] != mix(seed + i)) {
            abort();
        }
    }
    return r + a[63];
}

__attribute__((noinline)) static unsigned vary(int depth, unsigned seed) {
    const unsigned n = 1 + seed % 997;
    volatile unsigned char* v = static_cast<unsigned char*>(alloca(n));
    for (unsigned i = 0; i < n; ++i) {
        v[i] = static_cast<unsigned char>(seed + i);
    }
    unsigned r = depth > 0 ? vary(depth - 1, mix(seed)) : seed;
    for (unsigned i = 0; i < n; ++i) {
        if (v[i] != static_cast<unsigned char>(seed + i)) {
            abort();
        }
    }
    return r ^ n;
}

__attribute__((noinline)) static unsigned large(unsigned seed) {
    volatile unsigned char big[200000];
    for (unsigned i = 0; i < sizeof big; i += 4096) {
        big[i] = static_cast<unsigned char>(seed + i / 4096);
    }
    unsigned r = descend(100, seed);
    for (unsigned i = 0; i < sizeof big; i += 4096) {
        if (big[i] != static_cast<unsigned char>(seed + i / 4096)) {
            abort();
        }
    }
    return r ^ big[0];
}

__attribute__((noinline)) static unsigned page(unsigned seed) {
    volatile unsigned buf[1024];
    buf[seed % 1024] = seed;
    return buf[seed % 1024] + 1;
}

__attribute__((noinline)) static unsigned edge(int depth, unsigned seed) {
    volatile unsigned pad[16];
    pad[depth % 16] = seed;
    if (depth > 0) {
        return edge(depth - 1, seed) + pad[depth % 16];
    }
    unsigned r = 0;
    for (unsigned i = 0; i < 200; ++i) {
        r += page(seed + i);
    }
    return r;
}

static unsigned edges() {
    unsigned r = 0;
    for (int depth = 0; depth < 1200; ++depth) {
        r += edge(depth, depth);
    }
    return r;
}

static jmp_buf jump;

__attribute__((noinline)) static void leap(int depth, unsigned seed) {
    volatile unsigned a[512];
    a[depth % 512] = seed;
    if (depth == 0) {
        longjmp(jump, 1);
    }
    leap(depth - 1, mix(seed));
    if (a[depth % 512] != seed) {
        abort();
    }
}

static unsigned leaps() {
    unsigned r = 0;
    for (unsigned k = 0; k < 4; ++k) {
        if (setjmp(jump) == 0) {
            leap(300, k);
        }
        r += descend(50, k);
    }
    return r;
}

static volatile unsigned handled;

static void handler(int) {
    handled = descend(600, 7);
}

static void alternate() {
    stack_t alt{};
    alt.ss_size = 1 << 20;
    alt.ss_sp = malloc(alt.ss_size);
    assert(alt.ss_sp && sigaltstack(&alt, nullptr) == 0);
    struct sigaction action {};
    action.sa_handler = handler;
    action.sa_flags = SA_ONSTACK;
    assert(sigaction(SIGUSR1, &action, nullptr) == 0);
    assert(raise(SIGUSR1) == 0);
    assert(handled == descend(600, 7));
    alt.ss_flags = SS_DISABLE;
    assert(sigaltstack(&alt, nullptr) == 0);
}

__attribute__((noinline)) static unsigned forked(int depth, unsigned seed) {
    volatile unsigned a[256];
    for (int i = 0; i < 256; ++i) {
        a[i] = mix(seed + i);
    }
    unsigned r = 0;
    if (depth > 0) {
        r = forked(depth - 1, a[7]);
    } else {
        pid_t child = fork();
        assert(child >= 0);
        if (child == 0) {
            r = 1;
        } else {
            int status = 0;
            assert(waitpid(child, &status, 0) == child);
            r = WIFEXITED(status) && WEXITSTATUS(status) == 42 ? 2 : 3;
        }
    }
    for (int i = 0; i < 256; ++i) {
        if (a[i] != mix(seed + i)) {
            abort();
        }
    }
    return r;
}

int main() {
    const unsigned deep = descend(6000, 1);
    assert(deep == descend(6000, 1));
    const unsigned varied = vary(3000, 5);
    assert(varied == vary(3000, 5));
    const unsigned big = large(3);
    assert(big == large(3));
    const unsigned first = edges();
    size_t before = pages();
    assert(edges() == first && pages() == before);
    const unsigned leapt = leaps();
    before = pages();
    assert(leaps() == leapt && pages() == before);
    alternate();
    const unsigned r = forked(400, 9);
    if (r == 1) {
        _exit(42);
    }
    assert(r == 2);
    puts("stack blocks: ok");
}
