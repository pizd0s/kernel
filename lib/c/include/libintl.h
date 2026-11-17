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

char* textdomain(const char*);
char* bindtextdomain(const char*, const char*);
char* bind_textdomain_codeset(const char*, const char*);

__END_DECLS
