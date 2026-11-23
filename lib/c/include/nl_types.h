#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * Message catalogs, POSIX's, the kernel's. catopen finds a catalog by
 * NLSPATH's templates, a colon between two, else by
 * /usr/share/nls/%L/%N.cat and /usr/share/nls/%N/%L: %N is the name, %L
 * the locale, LC_MESSAGES's with NL_CAT_LOCALE, else LANG's, %l, %t and
 * %c its language, territory and codeset, %% a %; an empty template is
 * the name, and one of another % is passed over. A name with a slash is
 * the catalog's path. A catalog is of the BSDs' format, musl's and
 * FreeBSD's, which gencat writes. catgets gives the message of a set, or
 * s where there is none, ENOMSG.
 */
typedef void* nl_catd;
typedef int nl_item;

#define NL_SETD 1
#define NL_CAT_LOCALE 1

int catclose(nl_catd);
char* catgets(nl_catd, int, int, const char*);
nl_catd catopen(const char*, int);

__END_DECLS
