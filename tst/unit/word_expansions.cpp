//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "word expansions: ok\n"

// wordexp, as POSIX.1-2024 has it: the words a shell makes of a command's
// arguments, by quoting, field splitting, parameters, tilde, arithmetic,
// command substitution and pathnames; WRDE_NOCMD refuses command
// substitution with WRDE_CMDSUB;
// WRDE_APPEND adds to the words before, WRDE_DOOFFS puts we_offs null
// pointers first, WRDE_REUSE frees the words first; wordfree keeps errno.
// lib/c's alone, as musl checks the characters only with WRDE_NOCMD,
// ignores WRDE_UNDEF and leaves the substitutions' and the shell's errors
// on standard error: WRDE_BADCHAR for an unquoted |, &, ;, <, >, (, ), {,
// } or newline; WRDE_BADVAL for an unset variable with WRDE_UNDEF; no
// standard error of a substitution or of a syntax error, WRDE_SYNTAX.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>
#include <wordexp.h>

// The words of text, the ones after flags up to a null pointer.
static void words(const char* text, int flags, ...) {
    wordexp_t w = {};
    int r = wordexp(text, &w, flags);
    va_list ap;
    va_start(ap, flags);
    size_t i = 0;
    for (const char* s = va_arg(ap, const char*); s; s = va_arg(ap, const char*), i++) {
        if (r != 0 || i >= w.we_wordc || strcmp(w.we_wordv[i], s) != 0) {
            fprintf(stdout, "wordexp(%s): %d, %zu words, word %zu not '%s'\n", text, r, w.we_wordc, i, s);
            abort();
        }
    }
    va_end(ap);
    assert(r == 0 && w.we_wordc == i && w.we_wordv[i] == nullptr);
    wordfree(&w);
}

static int fails(const char* text, int flags = 0) {
    wordexp_t w = {};
    int r = wordexp(text, &w, flags);
    assert(r != 0);
    return r;
}

int main() {
    setenv("HOME", "/home/tester", 1);
    setenv("GREETING", "hello world", 1);
    unsetenv("UNSET_ONE");
    words("a b  c", 0, "a", "b", "c", nullptr);
    words("'a b' \"c d\" e\\ f", 0, "a b", "c d", "e f", nullptr);
    words("$GREETING \"$GREETING\"", 0, "hello", "world", "hello world", nullptr);
    words("${UNSET_ONE:-fallback} ~ ~/x", 0, "fallback", "/home/tester", "/home/tester/x", nullptr);
    words("$((2 + 3)) $(echo made by a command)", 0, "5", "made", "by", "a", "command", nullptr);
    words("", 0, nullptr);

    char work[] = "words-XXXXXX";
    assert(mkdtemp(work) == work && chdir(work) == 0);
    const char* names[] = {"b.zz", "a.zz", "c.yy"};
    for (const char* name : names) {
        int fd = open(name, O_CREAT | O_WRONLY, 0644);
        assert(fd >= 0 && close(fd) == 0);
    }
    words("*.zz", 0, "a.zz", "b.zz", nullptr);
    words("'*.zz'", 0, "*.zz", nullptr);

    assert(fails("$(echo no)", WRDE_NOCMD) == WRDE_CMDSUB && fails("`echo no`", WRDE_NOCMD) == WRDE_CMDSUB);
    words("$((1 + 1))", WRDE_NOCMD, "2", nullptr);
    assert(fails("a|b", WRDE_NOCMD) == WRDE_BADCHAR);

    wordexp_t w = {};
    assert(wordexp("one two", &w, 0) == 0 && w.we_wordc == 2);
    assert(wordexp("three", &w, WRDE_APPEND) == 0 && w.we_wordc == 3 && strcmp(w.we_wordv[2], "three") == 0 && !w.we_wordv[3]);
    assert(wordexp("again", &w, WRDE_REUSE) == 0 && w.we_wordc == 1 && strcmp(w.we_wordv[0], "again") == 0);
    errno = EBADF;
    wordfree(&w);
    assert(errno == EBADF);
    w = {};
    w.we_offs = 2;
    assert(wordexp("x y", &w, WRDE_DOOFFS) == 0 && w.we_wordc == 2 && w.we_offs == 2);
    assert(!w.we_wordv[0] && !w.we_wordv[1] && strcmp(w.we_wordv[2], "x") == 0 && strcmp(w.we_wordv[3], "y") == 0 && !w.we_wordv[4]);
    assert(wordexp("z", &w, WRDE_DOOFFS | WRDE_APPEND) == 0 && w.we_wordc == 3 && strcmp(w.we_wordv[4], "z") == 0);
    wordfree(&w);

#ifdef __wasm__
    words("$(ls /no/such/place) quiet", 0, "quiet", nullptr);
    assert(fails("'open") == WRDE_SYNTAX && fails("\"open") == WRDE_SYNTAX);
    const char* bad[] = {"a|b", "a;b", "a&b", "a<b", "a>b", "(a)", "{a}", "a\nb"};
    for (const char* text : bad) {
        assert(fails(text) == WRDE_BADCHAR);
    }
    words("'a|b' \"c;d\" $(echo e | tr e f) ${GREETING%world}x", 0, "a|b", "c;d", "f", "hello", "x", nullptr);
    assert(fails("$UNSET_ONE", WRDE_UNDEF) == WRDE_BADVAL);
    words("$UNSET_ONE set", 0, "set", nullptr);
    w = {};
    assert(wordexp("keep", &w, 0) == 0);
    assert(wordexp("a|b", &w, WRDE_APPEND) == WRDE_BADCHAR && w.we_wordc == 1 && strcmp(w.we_wordv[0], "keep") == 0);
    wordfree(&w);
#endif
    printf("word expansions: ok\n");
}
