//@ compile-flags: -g
//@ stdout: "split characters: ok\n"
//@ host: false

// A character the end of mbsnrtowcs's input cuts (POSIX.1-2024): the
// conversion stops at the end of the input, past the character's first
// bytes, which the state keeps, and the call with the rest completes it;
// counting, with no place to convert to, stops there too.
//
// Not on the host: musl's stops before the character, its bytes not
// taken, as POSIX.1-2017 left open.

#include <assert.h>
#include <locale.h>
#include <stdio.h>
#include <string.h>
#include <wchar.h>

int main() {
    assert(setlocale(LC_ALL, "C.UTF-8") != nullptr);

    const char text[] = "a\xE2\x82\xAC" "b";
    wchar_t out[8] = {};
    mbstate_t state = {};
    const char* at = text;
    assert(mbsnrtowcs(out, &at, 3, 8, &state) == 1 && out[0] == L'a');
    assert(at == text + 3 && !mbsinit(&state));
    assert(mbsnrtowcs(out + 1, &at, 3, 7, &state) == 2 && at == nullptr);
    assert(wcscmp(out, L"a€b") == 0 && mbsinit(&state));

    state = {};
    at = text;
    assert(mbsnrtowcs(nullptr, &at, 2, 0, &state) == 1 && at == text);

    printf("split characters: ok\n");
}
