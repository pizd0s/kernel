//@ compile-flags: -g -fno-builtin
//@ aot-flags: --asan
//@ run-flags: --asan
//@ stdout: "sanitized memory: ok\n"

#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mman.h>

static volatile uint64_t global = 0x0123456789abcdefULL;
static volatile unsigned char fixed[8192];

[[gnu::noinline]] static unsigned char distant(volatile unsigned char* bytes, unsigned i) {
    bytes[i + 4096] = static_cast<unsigned char>(i * 7);
    return bytes[i + 4096];
}

[[gnu::noinline]] static uint64_t transfer(volatile uint64_t* words, unsigned i, uint64_t value) {
    words[i] = value;
    return words[i];
}

int main() {
    assert(global == 0x0123456789abcdefULL);
    global ^= 0xffff;
    assert(global == 0x0123456789ab3210ULL);
    fixed[17] = 91;
    assert(fixed[17] == 91);

    auto* bytes = static_cast<unsigned char*>(malloc(16384));
    assert(bytes);
    for (unsigned i = 0; i < 100; ++i) {
        assert(distant(bytes, i) == static_cast<unsigned char>(i * 7));
        uint64_t value = uint64_t(i) * 0xfedcba987654321ULL;
        assert(transfer(reinterpret_cast<volatile uint64_t*>(bytes), i, value) == value);
    }
    memset(bytes, 0x57, 16384);
    auto* copy = static_cast<unsigned char*>(calloc(16384, 1));
    assert(copy && copy[16383] == 0);
    memcpy(copy, bytes, 16384);
    assert(memcmp(copy, bytes, 16384) == 0);
    copy[8191] = 0x58;
    assert(memcmp(copy, bytes, 16384) > 0);
    assert(memchr(copy, 0x58, 16384) == copy + 8191);
    memmove(copy + 1, copy, 16383);
    assert(copy[8192] == 0x58 && copy[8191] == 0x57);
    memmove(copy, copy + 1, 16383);
    assert(copy[8191] == 0x58 && copy[8192] == 0x57);
    copy = static_cast<unsigned char*>(realloc(copy, 32768));
    assert(copy && copy[8191] == 0x58);
    copy[32767] = 0xa5;
    assert(copy[32767] == 0xa5);

    char text[64], other[64];
    strcpy(text, "checked");
    strcat(text, " strings");
    assert(strlen(text) == 15 && strcmp(text, "checked strings") == 0);
    strncpy(other, text, sizeof(other));
    assert(strcmp(other, text) == 0 && strchr(other, ' ') == other + 7);
    // lib/str's by a call from here, their ranges the sanitizer's too: a
    // string that ends its block is read to its NUL and written to it,
    // and no byte past the block is anyone's.
    char* exact = static_cast<char*>(malloc(16));
    char* room = static_cast<char*>(malloc(16));
    assert(exact && room && mempcpy(exact, text, 16) == exact + 16);
    assert(strnlen(exact, 16) == 15 && strnlen(exact, 4) == 4 && strnlen(exact, 100) == 15);
    assert(strstr(exact, "strings") == exact + 8 && strstr(exact, "gs") == exact + 13 && strstr(exact, "x") == nullptr);
    assert(strpbrk(exact, "sx") == exact + 8 && memmem(exact, 16, "ngs", 4) == exact + 12);
    assert(strlcpy(room, "checked", 16) == 7 && strncat(room, " strings", 8) == room && strcmp(room, exact) == 0);
    assert(strlcat(room, "", 16) == 15 && strlcat(room, "more", 16) == 19 && bcmp(room, exact, 16) == 0);
    bzero(room, 16);
    explicit_bzero(exact, 16);
    assert(bcmp(room, exact, 16) == 0);
    free(exact);
    free(room);
    assert(strncmp(other, "checked stringz", 14) == 0);
    assert(strstr(other, "string") == other + 8);

    auto* mapped = static_cast<unsigned char*>(mmap(nullptr, 16384, PROT_READ | PROT_WRITE,
                                                  MAP_PRIVATE | MAP_ANONYMOUS, -1, 0));
    assert(mapped != MAP_FAILED);
    memset(mapped, 0x33, 16384);
    assert(distant(mapped, 37) == static_cast<unsigned char>(37 * 7));
    assert(mapped[16383] == 0x33);
    assert(munmap(mapped, 16384) == 0);
    free(copy);
    free(bytes);
    puts("sanitized memory: ok");
}
