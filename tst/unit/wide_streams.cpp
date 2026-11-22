//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "wide streams: ok\n"

// Streams of wide characters and of words: the _unlocked kinds of
// fgetwc, getwc, fgetws, fputwc, putwc and fputws as the locked ones, in
// UTF-8; open_wmemstream's wide buffer and its length in wide characters,
// NUL after them, at each fflush and at fclose; getw and putw's ints,
// EOF at the end; fgetln's line with its newline and length, not ended
// by NUL, the last line without one, NULL at the end.

#include <assert.h>
#include <locale.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <wchar.h>

int main() {
    assert(setlocale(LC_ALL, "C.UTF-8") != nullptr);

    FILE* f = fopen("wide", "w+");
    assert(f);
    assert(fputwc_unlocked(L'é', f) == L'é' && putwc_unlocked(L'x', f) == L'x');
    assert(fputws_unlocked(L"ab€\n", f) >= 0 && fputws(L"line two\n", f) >= 0);
    rewind(f);
    assert(fgetwc_unlocked(f) == L'é' && getwc_unlocked(f) == L'x');
    wchar_t line[16];
    assert(fgetws_unlocked(line, 16, f) == line && wcscmp(line, L"ab€\n") == 0);
    assert(fgetws(line, 16, f) == line && wcscmp(line, L"line two\n") == 0);
    assert(fgetwc_unlocked(f) == WEOF && feof(f));
    fclose(f);
    f = fopen("wide", "r");
    char bytes[32];
    assert(fread(bytes, 1, sizeof bytes, f) == 18 && memcmp(bytes, "\xC3\xA9x" "ab\xE2\x82\xAC\n", 9) == 0);
    fclose(f);

    wchar_t* buffer = nullptr;
    size_t size = 1;
    FILE* m = open_wmemstream(&buffer, &size);
    assert(m);
    assert(fputws(L"héllo", m) >= 0 && fflush(m) == 0);
    assert(buffer && size == 5 && wcscmp(buffer, L"héllo") == 0);
    assert(fwprintf(m, L" %d %ls", 42, L"€") > 0 && fflush(m) == 0);
    assert(size == 10 && wcscmp(buffer, L"héllo 42 €") == 0);
    assert(fclose(m) == 0 && size == 10 && buffer[10] == 0);
    free(buffer);

    f = fopen("words", "w+");
    assert(f);
    const int words[] = {0, 1, -1, 0x12345678, -0x7fffffff};
    for (int w : words) {
        assert(putw(w, f) == 0);
    }
    rewind(f);
    for (int w : words) {
        assert(getw(f) == w);
    }
    assert(getw(f) == EOF && feof(f));
    fclose(f);

    f = fopen("lines", "w+");
    assert(f && fputs("first\nsecond line\nlast", f) >= 0);
    rewind(f);
    size_t n = 0;
    char* l = fgetln(f, &n);
    assert(l && n == 6 && memcmp(l, "first\n", 6) == 0);
    l = fgetln(f, &n);
    assert(l && n == 12 && memcmp(l, "second line\n", 12) == 0);
    l = fgetln(f, &n);
    assert(l && n == 4 && memcmp(l, "last", 4) == 0);
    assert(fgetln(f, &n) == nullptr);
    fclose(f);

    printf("wide streams: ok\n");
}
