#include <errno.h>
#include <libintl.h>
#include <limits.h>
#include <locale.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>

/*
 * Message catalogs, as musl has them where no catalog is found. lib/c has
 * the one locale, C with UTF-8 strings (locale.c), and in it no message is
 * translated: every gettext gives the message it was asked for, a plural
 * one by n. The domains keep what a program says of them, the current one
 * and each one's directory, for the program to ask back; the one codeset a
 * domain's messages can be given in is UTF-8.
 */
struct domain {
    struct domain* next;
    char* directory;
    char name[];
};

static struct domain* domains;
static char* current;

char* __kernel_textdomain(const char* name) {
    if (!name) {
        return current ? current : "messages";
    }

    size_t n = strlen(name);

    if (n > NAME_MAX) {
        errno = EINVAL;

        return 0;
    }

    if (!current && !(current = malloc(NAME_MAX + 1))) {
        return 0;
    }

    memcpy(current, name, n + 1);

    return current;
}

char* __kernel_bindtextdomain(const char* name, const char* directory) {
    if (!name) {
        return 0;
    }

    struct domain* d = domains;

    while (d && strcmp(d->name, name)) {
        d = d->next;
    }

    if (!directory) {
        return d ? d->directory : 0;
    }

    size_t n = strlen(name);

    if (n > NAME_MAX || strlen(directory) >= PATH_MAX) {
        errno = EINVAL;

        return 0;
    }

    char* copy = strdup(directory);

    if (!copy) {
        return 0;
    }

    if (!d) {
        d = calloc(1, sizeof *d + n + 1);

        if (!d) {
            free(copy);

            return 0;
        }

        memcpy(d->name, name, n + 1);
        d->next = domains;
        domains = d;
    }

    free(d->directory);
    d->directory = copy;

    return copy;
}

char* __kernel_bind_textdomain_codeset(const char* name, const char* codeset) {
    if (codeset && strcasecmp(codeset, "UTF-8")) {
        errno = EINVAL;
    }

    return 0;
}

char* __kernel_dcngettext(const char* domain, const char* one, const char* many, unsigned long n, int category) {
    return (char*)(n == 1 ? one : many);
}

char* __kernel_dcgettext(const char* domain, const char* message, int category) {
    return (char*)message;
}

char* __kernel_dngettext(const char* domain, const char* one, const char* many, unsigned long n) {
    return (char*)(n == 1 ? one : many);
}

char* __kernel_dgettext(const char* domain, const char* message) {
    return (char*)message;
}

char* __kernel_ngettext(const char* one, const char* many, unsigned long n) {
    return (char*)(n == 1 ? one : many);
}

char* __kernel_gettext(const char* message) {
    return (char*)message;
}

// The _l functions: as those without the locale, of which lib/c has one.
char* __kernel_dcngettext_l(const char* domain, const char* one, const char* many, unsigned long n, int category, locale_t locale) {
    return (char*)(n == 1 ? one : many);
}

char* __kernel_dcgettext_l(const char* domain, const char* message, int category, locale_t locale) {
    return (char*)message;
}

char* __kernel_dngettext_l(const char* domain, const char* one, const char* many, unsigned long n, locale_t locale) {
    return (char*)(n == 1 ? one : many);
}

char* __kernel_dgettext_l(const char* domain, const char* message, locale_t locale) {
    return (char*)message;
}

char* __kernel_ngettext_l(const char* one, const char* many, unsigned long n, locale_t locale) {
    return (char*)(n == 1 ? one : many);
}

char* __kernel_gettext_l(const char* message, locale_t locale) {
    return (char*)message;
}
