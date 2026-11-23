//@ host: false
//@ compile-flags: -g -fno-builtin
//@ stdout: "regex extensions: ok\n"

#include <assert.h>
#include <regex.h>
#include <stdio.h>
#include <string.h>

// regex is FreeBSD's as its libregex builds it: GNU escapes supplement
// POSIX syntax, \| \+ \? of a basic expression, \< \> \b \B \w \W \s \S
// \` \', which the host's musl regex need not implement. What POSIX leaves
// undefined FreeBSD rejects: an escape of a letter it gives no meaning,
// a repetition of a repetition, a ) with no (, a { of no interval, a
// count past RE_DUP_MAX, 255; REG_STARTEND's end before its start is
// REG_INVARG, a code of the BSDs'.
struct Case {
    const char* pattern;
    const char* text;
    int flags;
    int begin;
    int end;
};

struct Rejected {
    const char* pattern;
    int flags;
    int code;
};

// A program of the lib/c before FreeBSD's regex.h calls the kernel's regex
// with a regex_t of 32 bytes, re_nsub first, a regmatch_t of two longs, and
// REG_NEWLINE 4 and REG_NOSUB 8: the kernel's functions take them still.
struct EarlierRegex {
    size_t re_nsub;
    void* opaque;
    void* padding[4];
    size_t nsub;
    char last;
};

struct EarlierMatch {
    long rm_so;
    long rm_eo;
};

static_assert(sizeof(EarlierRegex) == 32 && sizeof(EarlierMatch) == 8);

extern "C" int earlierRegcomp(EarlierRegex*, size_t, const char*, int) __attribute__((import_module("kernel"), import_name("regcomp")));
extern "C" int earlierRegexec(const EarlierRegex*, size_t, const char*, size_t, EarlierMatch*, size_t, int)
    __attribute__((import_module("kernel"), import_name("regexec")));
extern "C" void earlierRegfree(EarlierRegex*, size_t) __attribute__((import_module("kernel"), import_name("regfree")));

int main() {
    const Case cases[] = {
        {R"(a\|bc)", "xbc", 0, 1, 3},
        {R"(a\|bc)", "xa", 0, 1, 2},
        {R"(a\+)", "xaaab", 0, 1, 4},
        {R"(a\?)", "a", 0, 0, 1},
        {R"(a\?)", "b", 0, 0, 0},
        {R"(\+)", "+", 0, 0, 1},
        {R"(\?)", "?", 0, 0, 1},
        {"*", "x*y", 0, 1, 2},
        {R"(\<word\>)", "a word!", REG_EXTENDED, 2, 6},
        {R"(\bword\b)", "sword word!", REG_EXTENDED, 6, 10},
        {R"(\Boo\B)", "food", REG_EXTENDED, 1, 3},
        {R"(\w\W\s\S)", "a! b", REG_EXTENDED, 0, 4},
        {R"(\`abc\')", "abc", REG_EXTENDED, 0, 3},
        {R"(\`abc\')", "xabc", REG_EXTENDED, -1, -1},
        {R"(\`abc\')", "abcx", REG_EXTENDED, -1, -1},
        {R"(\(ab\)\{2\})", "xababz", 0, 1, 5},
        {R"(a\{2,\})", "xaaab", 0, 1, 4},
        {R"(a\{0,2\})", "aaab", 0, 0, 2},
        {"a{2}", "xaaab", REG_EXTENDED, 1, 3},
        {"a{2,}", "xaaab", REG_EXTENDED, 1, 4},
        {"a{0,2}", "aaab", REG_EXTENDED, 0, 2},
        {"(ab){2,3}", "xabababz", REG_EXTENDED, 1, 7},
    };
    for (const auto& entry : cases) {
        regex_t expression{};
        int code = regcomp(&expression, entry.pattern, entry.flags);
        assert(code == 0);
        regmatch_t matches[4];
        code = regexec(&expression, entry.text, 4, matches, 0);
        if (entry.begin < 0) {
            assert(code == REG_NOMATCH);
        } else {
            assert(code == 0 && matches[0].rm_so == entry.begin && matches[0].rm_eo == entry.end);
        }
        regfree(&expression);
    }
    const Rejected rejected[] = {
        {"{oops}", REG_EXTENDED, REG_BADRPT},
        {R"(\n\t)", REG_EXTENDED, REG_EESCAPE},
        {R"(\q)", REG_EXTENDED, REG_EESCAPE},
        {"a**", REG_EXTENDED, REG_BADRPT},
        {"a*+", REG_EXTENDED, REG_BADRPT},
        {"a+?", REG_EXTENDED, REG_BADRPT},
        {")", REG_EXTENDED, REG_EPAREN},
        {"(a{1000}){1000}", REG_EXTENDED, REG_BADBR},
    };
    for (const auto& entry : rejected) {
        regex_t expression{};
        assert(regcomp(&expression, entry.pattern, entry.flags) == entry.code);
    }
    regex_t expression{};
    assert(regcomp(&expression, "ab", REG_EXTENDED) == 0);
    regmatch_t range{3, 2};
    assert(regexec(&expression, "zabz", 1, &range, REG_STARTEND) == REG_INVARG);
    range = {1, 3};
    assert(regexec(&expression, "zabz", 1, &range, REG_STARTEND) == 0 && range.rm_so == 1 && range.rm_eo == 3);
    regfree(&expression);
    // A stale public handle must be rejected without reading host state.
    assert(regexec(&expression, "ab", 0, nullptr, 0) == REG_BADPAT);
    char message[32];
    size_t size = regerror(999, &expression, message, sizeof(message));
    assert(size == strlen(message) + 1 && message[0] != 0);

    EarlierRegex earlier{};
    assert(earlierRegcomp(&earlier, sizeof earlier, "^(b)c", 1 | 4) == 0 && earlier.re_nsub == 1);
    EarlierMatch found[2] = {};
    assert(earlierRegexec(&earlier, sizeof earlier, "a\nbc", 2, found, sizeof found[0], 0) == 0);
    assert(found[0].rm_so == 2 && found[0].rm_eo == 4 && found[1].rm_so == 2 && found[1].rm_eo == 3);
    found[0] = {2, 4};
    assert(earlierRegexec(&earlier, sizeof earlier, "a\nbcd", 1, found, sizeof found[0], REG_STARTEND) == 0 && found[0].rm_eo == 4);
    earlierRegfree(&earlier, sizeof earlier);
    assert(earlierRegcomp(&earlier, 24, "a", 0) == REG_INVARG);
    puts("regex extensions: ok");
}
