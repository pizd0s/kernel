#pragma once

#include <__base.h>

__BEGIN_DECLS

#define __USE_GNU_GETTEXT 1
#define __GNU_GETTEXT_SUPPORTED_REVISION(major) ((major) == 0 ? 1 : -1)

char* gettext(const char*);
char* dgettext(const char*, const char*);
char* dcgettext(const char*, const char*, int);
char* ngettext(const char*, const char*, unsigned long);
char* dngettext(const char*, const char*, const char*, unsigned long);
char* dcngettext(const char*, const char*, const char*, unsigned long, int);
char* gettext_l(const char*, locale_t);
char* dgettext_l(const char*, const char*, locale_t);
char* dcgettext_l(const char*, const char*, int, locale_t);
char* ngettext_l(const char*, const char*, unsigned long, locale_t);
char* dngettext_l(const char*, const char*, const char*, unsigned long, locale_t);
char* dcngettext_l(const char*, const char*, const char*, unsigned long, int, locale_t);

char* textdomain(const char*);
char* bindtextdomain(const char*, const char*);
char* bind_textdomain_codeset(const char*, const char*);

__END_DECLS
