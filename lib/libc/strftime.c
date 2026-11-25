/*
 * strftime, wcsftime and strptime of the C locale's names and formats.
 *
 * A conversion of strftime may have flags, _ padding its number with
 * spaces, - not at all, 0 with zeros, ^ its text in capitals (# is passed
 * over), a width, its least, and E or O, passed over; one of no letter
 * this knows is itself, and a % that ends the format is one. %s counts
 * from the broken-down time as UTC, less tm_gmtoff; %G, %g and %V are of
 * the ISO week of the year's day tm_yday. wcsftime formats its format as
 * UTF-8, a character of none U+FFFD, and gives the characters of what
 * comes of it.
 *
 * strptime takes the same conversions but %c, %x and %X and their kind,
 * names in any case and as abbreviations, at most the digits of a field,
 * white space in the format as any of it, and before a number or a name;
 * %C with %y is the century, %s the local time of the seconds. It changes
 * the fields up to tm_isdst it parses, and leaves the rest.
 */
#include "sized.h"

#include <ctype.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <time.h>
#include <wchar.h>

static const char* const weekdays[] = {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"};
static const char* const months[] = {"January", "February", "March", "April", "May", "June",
                                     "July", "August", "September", "October", "November", "December"};

// A text being made, on the heap; failed where there was no room for it.
struct Out {
    char* text;
    size_t length;
    size_t room;
    int failed;
};

static void put(struct Out* o, const char* s, size_t n) {
    if (o->length + n + 1 > o->room) {
        size_t room = o->room ? o->room : 64;

        while (o->length + n + 1 > room) {
            room *= 2;
        }

        char* more = o->failed ? NULL : realloc(o->text, room);

        if (!more) {
            o->failed = 1;

            return;
        }

        o->text = more;
        o->room = room;
    }

    memcpy(o->text + o->length, s, n);
    o->length += n;
    o->text[o->length] = 0;
}

static void puts1(struct Out* o, const char* s) {
    put(o, s, strlen(s));
}

// Days from 1970-01-01 to a date of the proleptic Gregorian calendar.
static long long daysFrom(long long year, int month, int day) {
    year -= month <= 2;

    long long era = (year >= 0 ? year : year - 399) / 400;
    long long ofEra = year - era * 400;
    long long ofYear = (153 * (month > 2 ? month - 3 : month + 9) + 2) / 5 + day - 1;

    return era * 146097 + ofEra * 365 + ofEra / 4 - ofEra / 100 + ofYear - 719468;
}

// The year of a day counted from 1970-01-01.
static long long yearOf(long long days) {
    days += 719468;

    long long era = (days >= 0 ? days : days - 146096) / 146097;
    long long ofEra = days - era * 146097;
    long long years = (ofEra - ofEra / 1460 + ofEra / 36524 - ofEra / 146096) / 365;
    long long ofYear = ofEra - (365 * years + years / 4 - years / 100);
    long long shifted = (5 * ofYear + 2) / 153;

    return years + era * 400 + (shifted >= 10);
}

// The ISO year and week of the year's day.
static void isoWeek(long long year, int yday, long long* isoYear, int* week) {
    long long day = daysFrom(year, 1, 1) + yday;
    long long monday = ((day + 3) % 7 + 7) % 7;
    long long thursday = day - monday + 3;

    *isoYear = yearOf(thursday);
    *week = (int)((thursday - daysFrom(*isoYear, 1, 1)) / 7 + 1);
}

static const char* calendarName(const char* const* list, int count, int i, int brief) {
    static char abbreviation[4];

    if (i < 0 || i >= count) {
        return "?";
    }

    if (!brief) {
        return list[i];
    }

    memcpy(abbreviation, list[i], 3);
    abbreviation[3] = 0;

    return abbreviation;
}

static void number(struct Out* o, long long v, int width, char fill, char pad) {
    char digits[24];
    int n = snprintf(digits, sizeof digits, "%lld", v);

    if (pad != '-') {
        fill = pad ? pad : fill;

        for (int i = n; i < width; i++) {
            put(o, &fill, 1);
        }
    }

    put(o, digits, n);
}

static void text(struct Out* o, const char* s, int width) {
    for (int i = (int)strlen(s); i < width; i++) {
        put(o, " ", 1);
    }

    puts1(o, s);
}

static void format(struct Out* o, const char* f, const struct tm* tm);

static void conversion(struct Out* o, char c, const struct tm* tm, char pad, int width) {
#define NUMBER(v, least, fill) number(o, (v), (least) > width ? (least) : width, (fill), pad)
    long long year = (long long)tm->tm_year + 1900;
    int hour12 = tm->tm_hour % 12 ? tm->tm_hour % 12 : 12;
    long long isoYear;
    int week;
    char unknown[2];

    switch (c) {
        case 'a':
            return text(o, calendarName(weekdays, 7, tm->tm_wday, 1), width);
        case 'A':
            return text(o, calendarName(weekdays, 7, tm->tm_wday, 0), width);
        case 'b':
        case 'h':
            return text(o, calendarName(months, 12, tm->tm_mon, 1), width);
        case 'B':
            return text(o, calendarName(months, 12, tm->tm_mon, 0), width);
        case 'c':
            return format(o, "%a %b %e %H:%M:%S %Y", tm);
        case 'C':
            return NUMBER(year / 100, 2, '0');
        case 'd':
            return NUMBER(tm->tm_mday, 2, '0');
        case 'D':
        case 'x':
            return format(o, "%m/%d/%y", tm);
        case 'e':
            return NUMBER(tm->tm_mday, 2, ' ');
        case 'F':
            return format(o, "%Y-%m-%d", tm);
        case 'g':
            isoWeek(year, tm->tm_yday, &isoYear, &week);
            return NUMBER(isoYear % 100, 2, '0');
        case 'G':
            isoWeek(year, tm->tm_yday, &isoYear, &week);
            return NUMBER(isoYear, 1, '0');
        case 'H':
            return NUMBER(tm->tm_hour, 2, '0');
        case 'I':
            return NUMBER(hour12, 2, '0');
        case 'j':
            return NUMBER((long long)tm->tm_yday + 1, 3, '0');
        case 'k':
            return NUMBER(tm->tm_hour, 2, ' ');
        case 'l':
            return NUMBER(hour12, 2, ' ');
        case 'm':
            return NUMBER((long long)tm->tm_mon + 1, 2, '0');
        case 'M':
            return NUMBER(tm->tm_min, 2, '0');
        case 'n':
            return puts1(o, "\n");
        case 'p':
            return puts1(o, tm->tm_hour < 12 ? "AM" : "PM");
        case 'P':
            return puts1(o, tm->tm_hour < 12 ? "am" : "pm");
        case 'r':
            return format(o, "%I:%M:%S %p", tm);
        case 'R':
            return format(o, "%H:%M", tm);
        case 's': {
            long long months = (long long)tm->tm_mon;
            long long y = year + (months >= 0 ? months / 12 : (months - 11) / 12);
            int month = (int)(months - (y - year) * 12);
            long long days = daysFrom(y, month + 1, 1) + tm->tm_mday - 1;

            return NUMBER(days * 86400 + tm->tm_hour * 3600LL + tm->tm_min * 60LL + tm->tm_sec - tm->tm_gmtoff, 1, '0');
        }
        case 'S':
            return NUMBER(tm->tm_sec, 2, '0');
        case 't':
            return puts1(o, "\t");
        case 'T':
        case 'X':
            return format(o, "%H:%M:%S", tm);
        case 'u':
            return NUMBER((tm->tm_wday + 6) % 7 + 1, 1, '0');
        case 'U':
            return NUMBER((tm->tm_yday + 7 - tm->tm_wday) / 7, 2, '0');
        case 'V':
            isoWeek(year, tm->tm_yday, &isoYear, &week);
            return NUMBER(week, 2, '0');
        case 'w':
            return NUMBER(tm->tm_wday, 1, '0');
        case 'W':
            return NUMBER((tm->tm_yday + 7 - (tm->tm_wday + 6) % 7) / 7, 2, '0');
        case 'y':
            return NUMBER(year % 100, 2, '0');
        case 'Y':
            return NUMBER(year, 1, '0');
        case 'z': {
            long off = tm->tm_gmtoff;
            char sign = off < 0 ? '-' : '+';
            char zoneText[32];

            off = off < 0 ? -off : off;
            snprintf(zoneText, sizeof zoneText, "%c%02ld%02ld", sign, off / 3600, off / 60 % 60);

            return puts1(o, zoneText);
        }
        case 'Z':
            return text(o, tm->tm_zone ? tm->tm_zone : "", width);
        case '%':
            return puts1(o, "%");
    }

    unknown[0] = '%';
    unknown[1] = c;
    put(o, unknown, 2);
#undef NUMBER
}

static void format(struct Out* o, const char* f, const struct tm* tm) {
    for (size_t i = 0; f[i]; i++) {
        if (f[i] != '%' || !f[i + 1]) {
            put(o, f + i, 1);

            continue;
        }

        char pad = 0;
        int up = 0;
        int width = 0;

        for (i++; f[i] && strchr("_-0^#", f[i]); i++) {
            pad = f[i] == '_' ? ' ' : f[i] == '-' ? '-' : f[i] == '0' ? '0' : pad;
            up |= f[i] == '^';
        }

        for (; f[i] >= '0' && f[i] <= '9'; i++) {
            width = width < 100000000 ? width * 10 + (f[i] - '0') : width;
        }

        while (f[i] == 'E' || f[i] == 'O') {
            i++;
        }

        if (!f[i]) {
            break;
        }

        size_t from = o->length;

        conversion(o, f[i], tm, pad, width);

        for (size_t k = from; up && !o->failed && k < o->length; k++) {
            o->text[k] = toupper((unsigned char)o->text[k]);
        }
    }
}

size_t __kernel_strftime(char* restrict s, size_t max, const char* restrict f, const struct tm* restrict tm, size_t size) {
    sized("strftime", size, sizeof(struct tm));

    struct Out o = {0};

    format(&o, f, tm);

    int fits = !o.failed && o.length < max;
    size_t n = fits ? o.length : 0;

    if (fits) {
        memcpy(s, o.text ? o.text : "", n + 1);
    }

    free(o.text);

    return n;
}

// The character at s, of at most n bytes: U+FFFD, one byte, where they
// are of none.
static size_t decoded(const unsigned char* s, size_t n, wchar_t* c) {
    unsigned first = s[0];
    size_t length = first < 0x80 ? 1 : first < 0xc2 ? 0 : first < 0xe0 ? 2 : first < 0xf0 ? 3 : first < 0xf5 ? 4 : 0;
    uint32_t v = length == 1 ? first : first & (0x7f >> length);

    for (size_t i = 1; i < length && length <= n; i++) {
        if ((s[i] & 0xc0) != 0x80) {
            length = 0;

            break;
        }

        v = v << 6 | (s[i] & 0x3f);
    }

    if (!length || length > n || (length == 3 && v < 0x800) || (length == 4 && (v < 0x10000 || v > 0x10ffff)) ||
        (v >= 0xd800 && v <= 0xdfff)) {
        *c = 0xfffd;

        return 1;
    }

    *c = v;

    return length;
}

static void encode(struct Out* o, uint32_t c) {
    char b[4];

    if (c > 0x10ffff || (c >= 0xd800 && c <= 0xdfff)) {
        c = 0xfffd;
    }

    if (c < 0x80) {
        b[0] = c;
        put(o, b, 1);
    } else if (c < 0x800) {
        b[0] = 0xc0 | c >> 6;
        b[1] = 0x80 | (c & 0x3f);
        put(o, b, 2);
    } else if (c < 0x10000) {
        b[0] = 0xe0 | c >> 12;
        b[1] = 0x80 | (c >> 6 & 0x3f);
        b[2] = 0x80 | (c & 0x3f);
        put(o, b, 3);
    } else {
        b[0] = 0xf0 | c >> 18;
        b[1] = 0x80 | (c >> 12 & 0x3f);
        b[2] = 0x80 | (c >> 6 & 0x3f);
        b[3] = 0x80 | (c & 0x3f);
        put(o, b, 4);
    }
}

size_t __kernel_wcsftime(wchar_t* restrict s, size_t max, const wchar_t* restrict f, const struct tm* restrict tm, size_t size) {
    sized("wcsftime", size, sizeof(struct tm));

    struct Out narrow = {0}, o = {0};

    for (const wchar_t* c = f; *c; c++) {
        encode(&narrow, (uint32_t)*c);
    }

    format(&o, narrow.failed ? "" : narrow.text ? narrow.text : "", tm);

    size_t count = 0;

    for (size_t at = 0; at < o.length; count++) {
        wchar_t c;

        at += decoded((const unsigned char*)o.text + at, o.length - at, &c);
    }

    int fits = !narrow.failed && !o.failed && count < max;
    size_t n = fits ? count : 0;

    if (fits) {
        size_t at = 0;

        for (size_t i = 0; i < n; i++) {
            at += decoded((const unsigned char*)o.text + at, o.length - at, &s[i]);
        }

        s[n] = 0;
    }

    free(narrow.text);
    free(o.text);

    return n;
}

size_t __kernel_wcsftime_l(wchar_t* restrict s, size_t max, const wchar_t* restrict f, const struct tm* restrict tm, size_t size, locale_t locale) {
    sized("wcsftime_l", size, sizeof(struct tm));

    return __kernel_wcsftime(s, max, f, tm, size);
}

static int blank(unsigned char c) {
    return c == ' ' || (c >= '\t' && c <= '\r');
}

// A field of strptime's: at most the digits, and a sign before more than
// four, within lo and hi.
static int scanned(const unsigned char* s, size_t* at, int digits, long long lo, long long hi, long long* value) {
    while (blank(s[*at])) {
        ++*at;
    }

    int negative = 0;

    if ((s[*at] == '-' || s[*at] == '+') && digits > 4) {
        negative = s[(*at)++] == '-';
    }

    size_t start = *at;
    unsigned long long v = 0;

    while (*at - start < (size_t)digits && s[*at] >= '0' && s[*at] <= '9') {
        v = v * 10 + (s[(*at)++] - '0');
    }

    *value = negative ? -(long long)v : (long long)v;

    return *at > start && *value >= lo && *value <= hi;
}

static int named(const unsigned char* s, size_t* at, const char* const* list, int count, long long* value) {
    while (blank(s[*at])) {
        ++*at;
    }

    for (int i = 0; i < count; i++) {
        for (size_t n = strlen(list[i]);; n = 3) {
            if (strncasecmp((const char*)s + *at, list[i], n) == 0) {
                *at += n;
                *value = i;

                return 1;
            }

            if (n == 3) {
                break;
            }
        }
    }

    return 0;
}

// Whether the format takes the start of s into tm, and how much of it.
static int parsed(const unsigned char* s, const char* f, struct tm* tm, size_t* taken) {
    size_t at = 0;
    long long century = -1;

    for (size_t i = 0; f[i]; i++) {
        unsigned char c = f[i];

        if (blank(c)) {
            while (blank(s[at])) {
                at++;
            }

            continue;
        }

        if (c != '%' || !f[i + 1]) {
            if (!s[at] || s[at] != c) {
                return 0;
            }

            at++;

            continue;
        }

        for (i++; f[i] == 'E' || f[i] == 'O'; i++) {
        }

        if (!f[i]) {
            return 0;
        }

        long long v = 0;
        int ok = 0;
        size_t n;

        switch (f[i]) {
            case 'Y':
                ok = scanned(s, &at, 4, 0, 9999, &v);
                tm->tm_year = v - 1900;
                century = -1;
                break;
            case 'y':
                ok = scanned(s, &at, 2, 0, 99, &v);
                tm->tm_year = v < 69 ? v + 100 : v;
                break;
            case 'C':
                ok = scanned(s, &at, 2, 0, 99, &v);
                century = v;
                break;
            case 'm':
                ok = scanned(s, &at, 2, 1, 12, &v);
                tm->tm_mon = v - 1;
                break;
            case 'd':
            case 'e':
                ok = scanned(s, &at, 2, 1, 31, &v);
                tm->tm_mday = v;
                break;
            case 'H':
            case 'k':
                ok = scanned(s, &at, 2, 0, 23, &v);
                tm->tm_hour = v;
                break;
            case 'I':
            case 'l':
                ok = scanned(s, &at, 2, 1, 12, &v);
                tm->tm_hour = v % 12;
                break;
            case 'M':
                ok = scanned(s, &at, 2, 0, 59, &v);
                tm->tm_min = v;
                break;
            case 'S':
                ok = scanned(s, &at, 2, 0, 61, &v);
                tm->tm_sec = v;
                break;
            case 'j':
                ok = scanned(s, &at, 3, 1, 366, &v);
                tm->tm_yday = v - 1;
                break;
            case 'b':
            case 'B':
            case 'h':
                ok = named(s, &at, months, 12, &v);
                tm->tm_mon = v;
                break;
            case 'a':
            case 'A':
                ok = named(s, &at, weekdays, 7, &v);
                tm->tm_wday = v;
                break;
            case 'p':
                while (blank(s[at])) {
                    at++;
                }

                ok = s[at] && s[at + 1];

                if (ok) {
                    char half[3] = {toupper(s[at]), toupper(s[at + 1]), 0};

                    ok = strcmp(half, "AM") == 0 || strcmp(half, "PM") == 0;
                    tm->tm_hour += ok && half[0] == 'P' ? 12 : 0;
                    at += 2;
                }

                break;
            case 's':
                ok = scanned(s, &at, 20, -(1LL << 62), 1LL << 62, &v);

                if (ok) {
                    time_t t = v;

                    ok = localtime_r(&t, tm) != NULL;
                }

                break;
            case 'n':
            case 't':
                while (blank(s[at])) {
                    at++;
                }

                ok = 1;
                break;
            case 'D':
            case 'T':
            case 'R':
            case 'F':
                n = 0;
                ok = parsed(s + at, f[i] == 'D' ? "%m/%d/%y" : f[i] == 'T' ? "%H:%M:%S" : f[i] == 'R' ? "%H:%M" : "%Y-%m-%d", tm, &n);
                at += n;
                break;
            case '%':
                ok = s[at] == '%';
                at++;
                break;
        }

        if (!ok) {
            return 0;
        }
    }

    if (century >= 0) {
        tm->tm_year = century * 100 - 1900 + (tm->tm_year % 100 + 100) % 100;
    }

    *taken = at;

    return 1;
}

char* __kernel_strptime(const char* restrict s, const char* restrict f, struct tm* restrict tm, size_t size) {
    sized("strptime", size, sizeof(struct tm));

    struct tm fields = *tm;
    size_t at = 0;

    if (!parsed((const unsigned char*)s, f, &fields, &at)) {
        return NULL;
    }

    tm->tm_sec = fields.tm_sec;
    tm->tm_min = fields.tm_min;
    tm->tm_hour = fields.tm_hour;
    tm->tm_mday = fields.tm_mday;
    tm->tm_mon = fields.tm_mon;
    tm->tm_year = fields.tm_year;
    tm->tm_wday = fields.tm_wday;
    tm->tm_yday = fields.tm_yday;
    tm->tm_isdst = fields.tm_isdst;

    return (char*)s + at;
}
