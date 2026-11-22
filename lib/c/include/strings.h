#pragma once

#include <__base.h>

__BEGIN_DECLS

int bcmp(const void*, const void*, size_t);
void bcopy(const void*, void*, size_t);
void bzero(void*, size_t);
char* index(const char*, int);
char* rindex(const char*, int);
int ffs(int);
int ffsl(long);
int ffsll(long long);
int strcasecmp(const char*, const char*);
int strncasecmp(const char*, const char*, size_t);
int strcasecmp_l(const char*, const char*, locale_t);
int strncasecmp_l(const char*, const char*, size_t, locale_t);

__END_DECLS
