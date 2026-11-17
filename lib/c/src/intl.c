#include <errno.h>
#include <libintl.h>
#include <limits.h>
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

char* textdomain(const char* name) {
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

char* bindtextdomain(const char* name, const char* directory) {
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

char* bind_textdomain_codeset(const char* name, const char* codeset) {
    if (codeset && strcasecmp(codeset, "UTF-8")) {
        errno = EINVAL;
    }

    return 0;
}

char* dcngettext(const char* domain, const char* one, const char* many, unsigned long n, int category) {
    return (char*)(n == 1 ? one : many);
}

char* dcgettext(const char* domain, const char* message, int category) {
    return (char*)message;
}

char* dngettext(const char* domain, const char* one, const char* many, unsigned long n) {
    return (char*)(n == 1 ? one : many);
}

char* dgettext(const char* domain, const char* message) {
    return (char*)message;
}

char* ngettext(const char* one, const char* many, unsigned long n) {
    return (char*)(n == 1 ? one : many);
}

char* gettext(const char* message) {
    return (char*)message;
}
