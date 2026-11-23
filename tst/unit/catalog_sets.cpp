//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "catalog sets: ok\n"

// Message catalogs, as musl's read them, of the BSDs' format: catopen
// finds one by NLSPATH's templates, %N the name, %L LANG's locale, or
// LC_MESSAGES's with NL_CAT_LOCALE, %l, %t and %c its parts, an empty
// template the name and one of an unknown % passed over; a name with a
// slash is a path; a file that is no catalog is ENOENT. catgets gives a
// set's message, or the default and ENOMSG. lib/c's alone: catgets and
// catclose of a catalog never opened are EBADF, where musl's read it;
// gencat makes a catalog of message files, with escapes, a line that goes
// on, quotes, a message and a set deleted, and adds to one there is.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <nl_types.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static unsigned char* be(unsigned char* p, uint32_t v) {
    p[0] = uint8_t(v >> 24);
    p[1] = uint8_t(v >> 16);
    p[2] = uint8_t(v >> 8);
    p[3] = uint8_t(v);
    return p + 4;
}

// A catalog of set 1, messages 1 and 2, and set 3, message 7.
static void make(const char* path) {
    const char* texts[] = {"hello", "world", "seven"};
    const uint32_t sets[][3] = {{1, 2, 0}, {3, 1, 2}};
    const uint32_t messages[] = {1, 2, 7};
    unsigned char b[256] = {};
    unsigned char* p = b + 20;
    for (const auto& s : sets) {
        p = be(be(be(p, s[0]), s[1]), s[2]);
    }
    uint32_t offset = 0;
    for (int i = 0; i < 3; ++i) {
        p = be(be(be(p, messages[i]), uint32_t(strlen(texts[i]) + 1)), offset);
        offset += uint32_t(strlen(texts[i]) + 1);
    }
    for (const char* t : texts) {
        memcpy(p, t, strlen(t) + 1);
        p += strlen(t) + 1;
    }
    const uint32_t size = uint32_t(p - b);
    be(be(be(be(be(b, 0xff88ff89), 2), size - 20), 2 * 12), 2 * 12 + 3 * 12);
    const int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    assert(fd >= 0 && write(fd, b, size) == ssize_t(size) && close(fd) == 0);
}

static bool says(nl_catd cd, int set, int msg, const char* want) {
    errno = 0;
    const char* got = catgets(cd, set, msg, "default");
    return strcmp(got, want ? want : "default") == 0 && (want || errno == ENOMSG);
}

int main() {
    char work[] = "catalogs-XXXXXX";
    assert(mkdtemp(work) == work && chdir(work) == 0);
    assert(mkdir("nls", 0755) == 0 && mkdir("nls/C", 0755) == 0 && mkdir("nls/xx_YY.UTF-8", 0755) == 0 && mkdir("nls/xx-YY-UTF-8", 0755) == 0);
    make("nls/C/test.cat");
    make("nls/xx_YY.UTF-8/test.cat");
    make("nls/xx-YY-UTF-8/test.cat");
    make("test");

    setenv("LANG", "xx_YY.UTF-8", 1);
    setenv("NLSPATH", "/no/such/%N:nls/%L/%N.cat", 1);
    nl_catd cd = catopen("test", 0);
    assert(cd != (nl_catd)-1);
    assert(says(cd, 1, 1, "hello") && says(cd, 1, 2, "world") && says(cd, 3, 7, "seven"));
    assert(says(cd, 2, 1, nullptr) && says(cd, 1, 3, nullptr) && says(cd, 3, 1, nullptr));
    assert(catclose(cd) == 0);
    cd = catopen("test", NL_CAT_LOCALE);
    assert(cd != (nl_catd)-1 && says(cd, NL_SETD, 1, "hello") && catclose(cd) == 0);
    setenv("NLSPATH", "nls/%l-%t-%c/%N.cat", 1);
    cd = catopen("test", 0);
    assert(cd != (nl_catd)-1 && says(cd, 3, 7, "seven") && catclose(cd) == 0);
    setenv("NLSPATH", ":/no/such", 1);
    cd = catopen("test", 0);
    assert(cd != (nl_catd)-1 && says(cd, 1, 2, "world") && catclose(cd) == 0);
    setenv("NLSPATH", "nls/%X/%N.cat:nls/C/%N.cat", 1);
    cd = catopen("test", NL_CAT_LOCALE);
    assert(cd != (nl_catd)-1 && catclose(cd) == 0);
    cd = catopen("./nls/C/test.cat", 0);
    assert(cd != (nl_catd)-1 && says(cd, 1, 1, "hello") && catclose(cd) == 0);
    errno = 0;
    assert(catopen("none", 0) == (nl_catd)-1 && errno == ENOENT);
    const int fd = open("nls/C/junk.cat", O_WRONLY | O_CREAT, 0644);
    assert(fd >= 0 && write(fd, "no catalog at all, this", 23) == 23 && close(fd) == 0);
    errno = 0;
    assert(catopen("junk", NL_CAT_LOCALE) == (nl_catd)-1 && errno == ENOENT);

#ifdef __wasm__
    errno = 0;
    assert(strcmp(catgets((nl_catd)-1, 1, 1, "default"), "default") == 0 && errno == EBADF);
    errno = 0;
    assert(catclose((nl_catd)-1) == -1 && errno == EBADF);
    FILE* f = fopen("first.msg", "w");
    assert(f);
    fputs("$ a comment\n"
          "1 first message\n"
          "2 second\\tone\\nwith escapes \\101\n"
          "3 continued \\\n"
          "line\n"
          "\n"
          "$set 2\n"
          "$quote \"\n"
          "1 \"  spaced  \" after\n"
          "2 \"\"\n"
          "3 plain \"quotes\" kept\n"
          "$set 3\n"
          "1 gone\n"
          "1\n"
          "$set 4\n"
          "1 kept\n"
          "$delset 4 with its messages\n",
          f);
    assert(fclose(f) == 0 && system("gencat made.cat first.msg") == 0);
    cd = catopen("./made.cat", 0);
    assert(cd != (nl_catd)-1);
    assert(says(cd, 1, 1, "first message") && says(cd, 1, 2, "second\tone\nwith escapes A") && says(cd, 1, 3, "continued line"));
    assert(says(cd, 2, 1, "  spaced  ") && says(cd, 2, 2, "") && says(cd, 2, 3, "plain \"quotes\" kept"));
    assert(says(cd, 3, 1, nullptr) && says(cd, 4, 1, nullptr) && catclose(cd) == 0);
    f = fopen("second.msg", "w");
    assert(f && fputs("1 replaced\n$set 5\n9 new\n", f) >= 0 && fclose(f) == 0);
    assert(system("gencat made.cat second.msg") == 0);
    cd = catopen("./made.cat", 0);
    assert(cd != (nl_catd)-1 && says(cd, 1, 1, "replaced") && says(cd, 1, 2, "second\tone\nwith escapes A") && says(cd, 5, 9, "new"));
    assert(catclose(cd) == 0);
    f = fopen("bad.msg", "w");
    assert(f && fputs("x not a message\n", f) >= 0 && fclose(f) == 0);
    assert(system("gencat made.cat bad.msg 2> /dev/null") != 0);
#endif
    printf("catalog sets: ok\n");
}
