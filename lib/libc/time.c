/*
 * The time zones and the calendar are FreeBSD's tzcode's (localtime.c,
 * asctime.c, difftime.c): TZ's zone, unset the one of /etc/localtime,
 * empty UTC, a TZif file's under /usr/share/zoneinfo or of a path, else a
 * POSIX rule's. These are the functions of a structure the kernel's
 * forwarders pass the size of, which they check.
 */
#include "sized.h"

#include <time.h>

struct tm* __kernel_localtime(const time_t* t, size_t size) {
    sized("localtime", size, sizeof(struct tm));

    return localtime(t);
}

struct tm* __kernel_localtime_r(const time_t* restrict t, struct tm* restrict tm, size_t tmSize, size_t size) {
    sized("localtime_r", tmSize, sizeof(struct tm));
    sized("localtime_r", size, sizeof(struct tm));

    return localtime_r(t, tm);
}

time_t __kernel_mktime(struct tm* tm, size_t size) {
    sized("mktime", size, sizeof(struct tm));

    return mktime(tm);
}

struct tm* __kernel_gmtime(const time_t* t, size_t size) {
    sized("gmtime", size, sizeof(struct tm));

    return gmtime(t);
}

struct tm* __kernel_gmtime_r(const time_t* restrict t, struct tm* restrict tm, size_t tmSize, size_t size) {
    sized("gmtime_r", tmSize, sizeof(struct tm));
    sized("gmtime_r", size, sizeof(struct tm));

    return gmtime_r(t, tm);
}

time_t __kernel_timegm(struct tm* tm, size_t size) {
    sized("timegm", size, sizeof(struct tm));

    return timegm(tm);
}

char* __kernel_asctime(const struct tm* tm, size_t size) {
    sized("asctime", size, sizeof(struct tm));

    return asctime(tm);
}

char* __kernel_asctime_r(const struct tm* restrict tm, size_t tmSize, char* restrict buf) {
    sized("asctime_r", tmSize, sizeof(struct tm));

    return asctime_r(tm, buf);
}
