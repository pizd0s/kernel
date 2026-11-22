//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "random sequences: ok\n"

// The drand48 family against POSIX's definition of it: X(n+1) =
// (a X(n) + c) mod 2^48, a = 0x5DEECE66D and c = 0xB until lcong48 sets
// them and seed48 puts them back; drand48 and erand48 are X / 2^48,
// lrand48 and nrand48 its high 31 bits, mrand48 and jrand48 its high 32 as
// a signed number; srand48 makes X its argument's 32 bits above 0x330E;
// seed48 returns the X it replaces; the e, n and j kinds use the state
// they are given, not the family's. host: false: the host musl's srand48
// and seed48 keep the a and c lcong48 set.

#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>

namespace {
    uint64_t x;
    uint64_t a = 0x5DEECE66DULL;
    uint64_t c = 0xB;

    uint64_t step() {
        x = (a * x + c) & ((1ULL << 48) - 1);
        return x;
    }

    uint64_t of(const unsigned short s[3]) {
        return s[0] | (uint64_t)s[1] << 16 | (uint64_t)s[2] << 32;
    }
}

int main() {
    srand48(12345);
    x = (uint64_t)(uint32_t)12345 << 16 | 0x330E;
    for (int i = 0; i < 20; i++) {
        assert(drand48() == (double)step() / (1ULL << 48));
        assert(lrand48() == (long)(step() >> 17));
        assert(mrand48() == (long)(int32_t)(uint32_t)(step() >> 16));
    }

    srand48(-1);
    x = (uint64_t)0xFFFFFFFFu << 16 | 0x330E;
    assert(lrand48() == (long)(step() >> 17));

    unsigned short next[3] = {0x1234, 0x5678, 0x9ABC};
    uint64_t before = x;
    unsigned short* old = seed48(next);
    assert(of(old) == before);
    x = of(next);
    assert(drand48() == (double)step() / (1ULL << 48));

    unsigned short param[7] = {1, 2, 3, 0x0005, 0x0000, 0x0000, 7};
    lcong48(param);
    x = of(param);
    a = 5;
    c = 7;
    for (int i = 0; i < 5; i++) {
        assert(lrand48() == (long)(step() >> 17));
    }

    unsigned short back[3] = {9, 8, 7};
    seed48(back);
    x = of(back);
    a = 0x5DEECE66DULL;
    c = 0xB;
    assert(mrand48() == (long)(int32_t)(uint32_t)(step() >> 16));

    unsigned short own[3] = {0xE66D, 0xDEEC, 0x0005};
    uint64_t family = x;
    uint64_t mine = of(own);
    for (int i = 0; i < 10; i++) {
        x = mine;
        double e = (double)step() / (1ULL << 48);
        mine = x;
        assert(erand48(own) == e && of(own) == mine);
        x = mine;
        long n = (long)(step() >> 17);
        mine = x;
        assert(nrand48(own) == n && of(own) == mine);
        x = mine;
        long j = (long)(int32_t)(uint32_t)(step() >> 16);
        mine = x;
        assert(jrand48(own) == j && of(own) == mine);
    }
    x = family;
    assert(lrand48() == (long)(step() >> 17));

    for (int i = 0; i < 1000; i++) {
        double d = drand48();
        long l = lrand48();
        long m = mrand48();
        assert(d >= 0 && d < 1 && l >= 0 && (long long)l < (1LL << 31) && (long long)m >= -(1LL << 31));
        assert((long long)m < (1LL << 31));
    }

    printf("random sequences: ok\n");
}
