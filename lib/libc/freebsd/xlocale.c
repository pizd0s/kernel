/*
 * lib/c's one locale, as FreeBSD's stdio and multibyte functions see it
 * (xlocale_private.h): its ctype is FreeBSD's UTF-8, utf8.c's functions,
 * which are its own file's and so are included here; its collation is
 * none, so a scanf range is of characters' values.
 */
#include "namespace.h"

#include <wchar.h>

#include "collate.h"
#include "mblocal.h"
#include "un-namespace.h"
#include "utf8.c"

// A state of char16_t's begins with the state of char32_t's, UTF-8's.
_Static_assert(sizeof(_UTF8State) <= sizeof(((_Char16State*)0)->c16.c32), "a state of char16_t's has no room for UTF-8's");

struct xlocale_ctype __xlocale_global_ctype = {
    .__mbrtowc = _UTF8_mbrtowc,
    .__mbsinit = _UTF8_mbsinit,
    .__mbsnrtowcs = _UTF8_mbsnrtowcs,
    .__wcrtomb = _UTF8_wcrtomb,
    .__wcsnrtombs = _UTF8_wcsnrtombs,
    .__mb_cur_max = 4,
    .__mb_sb_limit = 128,
};

static struct xlocale_collate collation = {
    .__collate_load_error = 1,
};

struct _xlocale __xlocale_global_locale = {
    .components = {
        [XLC_COLLATE] = &collation.header,
        [XLC_CTYPE] = &__xlocale_global_ctype.header,
    },
};

// The order of a scanf range with no collation: the bytes'.
int __collate_range_cmp(char c1, char c2) {
    unsigned char a = c1, b = c2;

    return (a > b) - (a < b);
}
