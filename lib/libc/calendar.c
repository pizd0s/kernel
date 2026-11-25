/*
 * The calendar of UTC, the proleptic Gregorian one: gmtime and gmtime_r
 * break a time into its fields, timegm makes the time of fields, which
 * it normalizes as it gives them back, and a time whose year is beyond
 * an int's is EOVERFLOW; asctime and asctime_r write fields as POSIX's
 * text, of "?" for a day or month of none, and one longer than its 26
 * bytes is EOVERFLOW; difftime is the difference of two times.
 */
#include "sized.h"

#include <errno.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <time.h>

static int64_t floorDiv(int64_t a, int64_t b) {
    return a / b - (a % b < 0);
}

// The days since 1970-01-01 of a date, its month of 1 to 12.
static int64_t daysOf(int64_t year, int64_t month, int64_t day) {
    year -= month <= 2;

    int64_t era = floorDiv(year, 400), years = year - 400 * era;
    int64_t yearDay = (153 * (month > 2 ? month - 3 : month + 9) + 2) / 5 + day - 1;

    return 146097 * era + 365 * years + years / 4 - years / 100 + yearDay - 719468;
}

struct tm* __kernel_gmtime_r(const time_t* restrict t, struct tm* restrict tm, size_t tmSize, size_t size) {
    sized("gmtime_r", tmSize, sizeof(struct tm));
    sized("gmtime_r", size, sizeof(struct tm));

    int64_t days = floorDiv(*t, 86400), second = *t - 86400 * days;
    int64_t z = days + 719468, era = floorDiv(z, 146097), dayOfEra = z - 146097 * era;
    int64_t years = (dayOfEra - dayOfEra / 1460 + dayOfEra / 36524 - dayOfEra / 146096) / 365;
    int64_t yearDay = dayOfEra - (365 * years + years / 4 - years / 100), m = (5 * yearDay + 2) / 153;
    int64_t month = m < 10 ? m + 3 : m - 9, year = 400 * era + years + (month <= 2);

    if (year - 1900 > INT_MAX || year - 1900 < INT_MIN) {
        errno = EOVERFLOW;

        return NULL;
    }

    *tm = (struct tm){
        .tm_sec = second % 60,
        .tm_min = second / 60 % 60,
        .tm_hour = second / 3600,
        .tm_mday = yearDay - (153 * m + 2) / 5 + 1,
        .tm_mon = month - 1,
        .tm_year = year - 1900,
        .tm_wday = days - 7 * floorDiv(days + 4, 7) + 4,
        .tm_yday = days - daysOf(year, 1, 1),
        .tm_zone = "UTC",
    };

    return tm;
}

struct tm* __kernel_gmtime(const time_t* t, size_t size) {
    sized("gmtime", size, sizeof(struct tm));

    static struct tm tm;

    return __kernel_gmtime_r(t, &tm, sizeof tm, size);
}

time_t __kernel_timegm(struct tm* tm, size_t size) {
    sized("timegm", size, sizeof(struct tm));

    int64_t year = (int64_t)tm->tm_year + 1900 + floorDiv(tm->tm_mon, 12), month = tm->tm_mon - 12 * floorDiv(tm->tm_mon, 12);
    time_t t = 86400 * (daysOf(year, month + 1, 1) + tm->tm_mday - 1) + 3600 * (int64_t)tm->tm_hour + 60 * (int64_t)tm->tm_min + tm->tm_sec;
    struct tm out;

    if (!__kernel_gmtime_r(&t, &out, sizeof out, sizeof out)) {
        return -1;
    }

    *tm = out;

    return t;
}

char* __kernel_asctime_r(const struct tm* restrict tm, size_t tmSize, char* restrict buf) {
    sized("asctime_r", tmSize, sizeof(struct tm));

    static const char weekdays[][4] = {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"};
    static const char months[][4] = {"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"};
    char text[96];
    int n = snprintf(text, sizeof text, "%.3s %.3s%3d %.2d:%.2d:%.2d %lld\n",
                     (unsigned)tm->tm_wday < 7 ? weekdays[tm->tm_wday] : "?", (unsigned)tm->tm_mon < 12 ? months[tm->tm_mon] : "?",
                     tm->tm_mday, tm->tm_hour, tm->tm_min, tm->tm_sec, (long long)tm->tm_year + 1900);

    if (n >= 26) {
        errno = EOVERFLOW;

        return NULL;
    }

    return memcpy(buf, text, n + 1);
}

char* __kernel_asctime(const struct tm* tm, size_t size) {
    sized("asctime", size, sizeof(struct tm));

    static char text[26];

    return __kernel_asctime_r(tm, size, text);
}

double __kernel_difftime(time_t a, time_t b) {
    return (double)a - (double)b;
}
