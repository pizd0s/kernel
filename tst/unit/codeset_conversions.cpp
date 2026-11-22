//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "codeset conversions: ok\n"
//@ host: false

// iconv as POSIX.1-2024 has it, beyond character_conversion's cases:
// - tocode's suffixes: //TRANSLIT gives a character the target has none
//   for the nearest it has, lib/c's by the letters of the character's
//   compatibility decomposition, and the implementation's conversion where
//   it has none; //IGNORE drops a byte that is no character, and such a
//   character; //NON_IDENTICAL_DISCARD only such a character; each such
//   character is counted; an unknown suffix is EINVAL;
// - a codeset of shift states, ISO-2022-JP: E2BIG keeps the state for the
//   next call; a null input writes the sequence back to the initial state,
//   E2BIG with no room for it; a shift sequence cut short is EINVAL; a
//   child of fork converts on in its parent's state;
// - UTF-16's byte order mark decides the order, big-endian with none;
// - the CJK codesets, GB18030's own U+FFFD a character, not EILSEQ;
// - names matched with no case and no punctuation; WCHAR_T is wchar_t;
// - a closed descriptor is EBADF.
//
// Not on the host: musl has none of the suffixes.

#include <assert.h>
#include <errno.h>
#include <iconv.h>
#include <stdio.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>
#include <wchar.h>

struct Result {
    size_t count;
    int error;
    size_t taken;
    size_t made;
};

// step converts what fits of input into room bytes of out, on cd's state.
static Result step(iconv_t cd, const char* input, size_t length, char* out, size_t room) {
    char* in = const_cast<char*>(input);
    char* to = out;
    size_t left = length, space = room;
    errno = 0;
    size_t count = iconv(cd, &in, &left, &to, &space);
    return {count, count == (size_t)-1 ? errno : 0, size_t(in - input), size_t(to - out)};
}

static void is(iconv_t cd, const char* input, size_t length, const char* want, size_t made, size_t count) {
    char out[64];
    Result r = step(cd, input, length, out, sizeof out);
    assert(r.error == 0 && r.count == count && r.taken == length);
    assert(r.made == made && memcmp(out, want, made) == 0);
}

static void once(const char* to, const char* from, const char* input, size_t length, const char* want, size_t made) {
    iconv_t cd = iconv_open(to, from);
    assert(cd != (iconv_t)-1);
    is(cd, input, length, want, made, 0);
    assert(iconv_close(cd) == 0);
}

int main() {
    char out[64];

    iconv_t cd = iconv_open("ASCII//TRANSLIT", "UTF-8");
    assert(cd != (iconv_t)-1);
    is(cd, "caf\xc3\xa9 \xef\xac\x81", 9, "cafe fi", 7, 2);
    Result r = step(cd, "\xe2\x82\xac", 3, out, sizeof out);
    assert(r.error == 0 && r.count == 1 && r.taken == 3 && r.made == 1);
    assert(iconv_close(cd) == 0);

    cd = iconv_open("ISO-8859-1//IGNORE", "UTF-8");
    assert(cd != (iconv_t)-1);
    is(cd, "a\xff" "b\xd0\xbc" "c", 6, "abc", 3, 1);
    assert(iconv_close(cd) == 0);

    cd = iconv_open("ISO-8859-1//NON_IDENTICAL_DISCARD", "UTF-8");
    assert(cd != (iconv_t)-1);
    is(cd, "a\xd0\xbc" "b", 4, "ab", 2, 1);
    r = step(cd, "a\xff", 2, out, sizeof out);
    assert(r.error == EILSEQ && r.taken == 1 && r.made == 1);
    assert(iconv_close(cd) == 0);

    errno = 0;
    assert(iconv_open("UTF-8//NO_SUCH_THING", "UTF-8") == (iconv_t)-1 && errno == EINVAL);

    cd = iconv_open("ISO-2022-JP", "UTF-8");
    assert(cd != (iconv_t)-1);
    r = step(cd, "a\xe3\x81\x82" "b", 5, out, 4);
    assert(r.error == E2BIG && r.taken == 1 && r.made == 1 && out[0] == 'a');
    r = step(cd, "\xe3\x81\x82" "b", 4, out, sizeof out);
    assert(r.error == 0 && r.taken == 4 && r.made == 9 && memcmp(out, "\x1b$B\x24\x22\x1b(Bb", 9) == 0);
    r = step(cd, "\xe3\x81\x82", 3, out, sizeof out);
    assert(r.error == 0 && r.made == 5 && memcmp(out, "\x1b$B\x24\x22", 5) == 0);
    char* to = out;
    size_t space = 2;
    assert(iconv(cd, nullptr, nullptr, &to, &space) == (size_t)-1 && errno == E2BIG && space == 2);
    space = sizeof out;
    assert(iconv(cd, nullptr, nullptr, &to, &space) == 0 && to == out + 3 && memcmp(out, "\x1b(B", 3) == 0);
    r = step(cd, "\xe3\x81\x82", 3, out, sizeof out);
    assert(r.error == 0 && r.made == 5);
    pid_t child = fork();
    if (child == 0) {
        r = step(cd, "\xe3\x81\x84", 3, out, sizeof out);
        _exit(r.error == 0 && r.made == 2 && memcmp(out, "\x24\x24", 2) == 0 ? 0 : 1);
    }
    int status = 0;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    r = step(cd, "\xe3\x81\x84", 3, out, sizeof out);
    assert(r.error == 0 && r.made == 2 && memcmp(out, "\x24\x24", 2) == 0);
    assert(iconv_close(cd) == 0);

    cd = iconv_open("UTF-8", "ISO-2022-JP");
    assert(cd != (iconv_t)-1);
    is(cd, "\x1b$B\x24\x22\x1b(Bb", 9, "\xe3\x81\x82" "b", 4, 0);
    r = step(cd, "a\x1b$", 3, out, sizeof out);
    assert(r.error == EINVAL && r.taken == 1 && r.made == 1);
    assert(iconv_close(cd) == 0);

    once("UTF-8", "UTF-16", "\xff\xfe" "a\x00", 4, "a", 1);
    once("UTF-8", "UTF-16", "\xfe\xff\x00" "a", 4, "a", 1);
    once("UTF-8", "UTF-16", "\x00" "a\xfe\xff", 4, "a\xef\xbb\xbf", 4);
    once("UTF-16", "UTF-8", "a", 1, "\x00" "a", 2);
    once("UTF-8", "UTF-32", "\xff\xfe\x00\x00" "a\x00\x00\x00", 8, "a", 1);

    once("SHIFT_JIS", "UTF-8", "\xe3\x81\x82", 3, "\x82\xa0", 2);
    once("Shift_JIS", "UTF-8", "\xe3\x81\x82", 3, "\x82\xa0", 2);
    once("EUC-JP", "UTF-8", "\xe3\x81\x82", 3, "\xa4\xa2", 2);
    once("BIG5", "UTF-8", "\xe4\xb8\xad", 3, "\xa4\xa4", 2);
    once("GBK", "UTF-8", "\xe4\xb8\xad", 3, "\xd6\xd0", 2);
    once("EUC-KR", "UTF-8", "\xea\xb0\x80", 3, "\xb0\xa1", 2);
    once("UTF-8", "GB18030", "\x84\x31\xa4\x37", 4, "\xef\xbf\xbd", 3);
    once("utf8", "latin1", "\xe9", 1, "\xc3\xa9", 2);

    cd = iconv_open("UTF-8", "GB18030");
    assert(cd != (iconv_t)-1);
    r = step(cd, "a\xff", 2, out, sizeof out);
    assert(r.error == EILSEQ && r.taken == 1);
    assert(iconv_close(cd) == 0);

    cd = iconv_open("WCHAR_T", "UTF-8");
    assert(cd != (iconv_t)-1);
    wchar_t wide[4] = {};
    r = step(cd, "a\xc3\xa9", 3, reinterpret_cast<char*>(wide), sizeof wide);
    assert(r.error == 0 && r.made == 2 * sizeof(wchar_t) && wide[0] == L'a' && wide[1] == L'é');
    assert(iconv_close(cd) == 0);
    errno = 0;
    assert(iconv_close(cd) == -1 && errno == EBADF);

    printf("codeset conversions: ok\n");
}
