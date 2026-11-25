#include "kernel.h"
#include "sized.h"

#include <ctype.h>
#include <errno.h>
#include <langinfo.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <sys/stat.h>
#include <time.h>

/*
 * getdate, POSIX's: a date and time read by the first line of the file
 * DATEMSK names that matches all of it, what it leaves out taken from now
 * by POSIX's rules. A template has POSIX's conversions, %c, %x, %X and %r
 * the locale's; the match ignores case and white space more than the
 * other side has, a number has two digits at most and a year four, %Z is
 * a name of the zone's for the time. getdate_err says why there is none: 1 no
 * DATEMSK, 2 no file to open, 3 no status of it, 4 not a regular file, 5
 * a read failed, 6 no memory, 7 no line matched, 8 the date does not
 * exist, its zone is not the one in effect then, or time_t has no room.
 * The zone's names are tzname, the program's, whose place libc.wasm has
 * from its start (kernel.h). The call comes with the size of its struct
 * tm, the one layout it has had.
 */
static int reason;

int* __kernel___getdate_err(void) {
    return &reason;
}

enum { YEAR, CENTURY, YEARS, MONTH, DAY, WEEKDAY, HOUR, HOUR12, MINUTE, SECOND, NOON, ZONE, FIELDS };

typedef struct {
    int given[FIELDS];
    int value[FIELDS];
} Fields;

static const char* skip(const char* s) {
    while (isspace((unsigned char)*s)) {
        s++;
    }

    return s;
}

static const char* number(const char* s, int digits, int low, int high, Fields* f, int field) {
    int value = 0;
    int n = 0;

    for (; n < digits && isdigit((unsigned char)s[n]); n++) {
        value = 10 * value + (s[n] - '0');
    }

    if (n == 0 || value < low || value > high) {
        return 0;
    }

    f->given[field] = 1;
    f->value[field] = value;

    return s + n;
}

// A name of the locale's, full or abbreviated, its index the field's value.
static const char* named(const char* s, nl_item full, nl_item abbreviated, int count, Fields* f, int field) {
    for (int pass = 0; pass < 2; pass++) {
        for (int i = 0; i < count; i++) {
            const char* name = nl_langinfo((pass ? abbreviated : full) + i);
            size_t n = strlen(name);

            if (n && strncasecmp(s, name, n) == 0) {
                f->given[field] = 1;
                f->value[field] = i;

                return s + n;
            }
        }
    }

    return 0;
}

// A name of the zone's: which of its standard (bit 0) and summer (bit 1)
// time it names, none for a name that is not the zone's.
static const char* zone(const char* s, Fields* f) {
    size_t n = 0;

    while (isalpha((unsigned char)s[n])) {
        n++;
    }

    if (n == 0) {
        return 0;
    }

    tzset();
    f->given[ZONE] = 1;
    f->value[ZONE] = 0;

    for (int i = 0; i < 2; i++) {
        const char* zone = __program->tznameAt[i];

        if (zone && strlen(zone) == n && strncasecmp(s, zone, n) == 0) {
            f->value[ZONE] |= 1 << i;
        }
    }

    return s + n;
}

static const char* match(const char* format, const char* s, Fields* f, int depth) {
    while (s && *format) {
        if (isspace((unsigned char)*format)) {
            format++;
            continue;
        }

        s = skip(s);

        if (*format != '%') {
            if (tolower((unsigned char)*format) != tolower((unsigned char)*s)) {
                return 0;
            }

            format++;
            s++;
            continue;
        }

        format += 2;

        switch (format[-1]) {
            case '%':
                s = *s == '%' ? s + 1 : 0;
                break;
            case 'a':
            case 'A':
                s = named(s, DAY_1, ABDAY_1, 7, f, WEEKDAY);
                break;
            case 'b':
            case 'B':
            case 'h':
                s = named(s, MON_1, ABMON_1, 12, f, MONTH);
                break;
            case 'c':
                s = depth ? 0 : match(nl_langinfo(D_T_FMT), s, f, 1);
                break;
            case 'C':
                s = number(s, 2, 0, 99, f, CENTURY);
                break;
            case 'd':
            case 'e':
                s = number(s, 2, 1, 31, f, DAY);
                break;
            case 'D':
                s = match("%m/%d/%y", s, f, depth + 1);
                break;
            case 'H':
                s = number(s, 2, 0, 23, f, HOUR);
                break;
            case 'I':
                s = number(s, 2, 1, 12, f, HOUR12);
                break;
            case 'm':
                s = number(s, 2, 1, 12, f, MONTH);
                if (s) {
                    f->value[MONTH]--;
                }
                break;
            case 'M':
                s = number(s, 2, 0, 59, f, MINUTE);
                break;
            case 'n':
            case 't':
                break;
            case 'p':
                for (int i = 0; s && i < 2; i++) {
                    const char* name = nl_langinfo(i ? PM_STR : AM_STR);
                    size_t n = strlen(name);

                    if (n && strncasecmp(s, name, n) == 0) {
                        f->given[NOON] = 1;
                        f->value[NOON] = i;
                        s += n;
                        break;
                    }

                    if (i) {
                        s = 0;
                    }
                }
                break;
            case 'r':
                s = depth ? 0 : match(nl_langinfo(T_FMT_AMPM), s, f, 1);
                break;
            case 'R':
                s = match("%H:%M", s, f, depth + 1);
                break;
            case 'S':
                s = number(s, 2, 0, 60, f, SECOND);
                break;
            case 'T':
                s = match("%H:%M:%S", s, f, depth + 1);
                break;
            case 'w':
                s = number(s, 1, 0, 6, f, WEEKDAY);
                break;
            case 'x':
                s = depth ? 0 : match(nl_langinfo(D_FMT), s, f, 1);
                break;
            case 'X':
                s = depth ? 0 : match(nl_langinfo(T_FMT), s, f, 1);
                break;
            case 'y':
                s = number(s, 2, 0, 99, f, YEARS);
                break;
            case 'Y':
                s = number(s, 4, 0, 9999, f, YEAR);
                break;
            case 'Z':
                s = zone(s, f);
                break;
            default:
                s = 0;
                break;
        }
    }

    return s;
}

static int leap(int year) {
    return year % 4 == 0 && (year % 100 != 0 || year % 400 == 0);
}

static int days(int year, int month) {
    static const char lengths[] = {31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31};

    return lengths[month] + (month == 1 && leap(year));
}

/*
 * POSIX's rules: now, local; a month without a year the first from this
 * one on, its first day if no day is given; a weekday without a day the
 * first from that day on; a time without its hour, minute or second has
 * 0 there, and without a date the first such hour from this one on.
 */
static int resolve(const Fields* f, struct tm* t) {
    time_t now = time(0);

    localtime_r(&now, t);

    int hour = t->tm_hour;
    int date = f->given[YEAR] || f->given[CENTURY] || f->given[YEARS] || f->given[MONTH] || f->given[DAY] ||
               f->given[WEEKDAY];
    int year = f->given[YEAR] || f->given[CENTURY] || f->given[YEARS];

    if (f->given[YEAR]) {
        t->tm_year = f->value[YEAR] - 1900;
    } else if (year) {
        int within = f->given[YEARS] ? f->value[YEARS] : 0;

        if (f->given[CENTURY]) {
            t->tm_year = 100 * f->value[CENTURY] + within - 1900;
        } else {
            t->tm_year = within + (within < 69 ? 100 : 0);
        }
    }

    if (f->given[MONTH]) {
        if (!year && f->value[MONTH] < t->tm_mon) {
            t->tm_year++;
        }

        t->tm_mon = f->value[MONTH];
        t->tm_mday = 1;
    }

    if (f->given[DAY]) {
        if (f->value[DAY] > days(t->tm_year + 1900, t->tm_mon)) {
            return 8;
        }

        t->tm_mday = f->value[DAY];
    }

    if (f->given[HOUR] || f->given[HOUR12] || f->given[MINUTE] || f->given[SECOND]) {
        if (f->given[HOUR]) {
            t->tm_hour = f->value[HOUR];
        } else if (f->given[HOUR12]) {
            t->tm_hour = f->value[HOUR12] % 12 + (f->given[NOON] && f->value[NOON] ? 12 : 0);
        } else {
            t->tm_hour = 0;
        }

        t->tm_min = f->given[MINUTE] ? f->value[MINUTE] : 0;
        t->tm_sec = f->given[SECOND] ? f->value[SECOND] : 0;

        if (!date && t->tm_hour < hour) {
            t->tm_mday++;
        }
    }

    t->tm_isdst = -1;

    if (f->given[WEEKDAY] && !f->given[DAY]) {
        struct tm probe = *t;

        errno = 0;

        if (mktime(&probe) == (time_t)-1 && errno) {
            return 8;
        }

        t->tm_mday += (f->value[WEEKDAY] - probe.tm_wday + 7) % 7;
    }

    errno = 0;

    if (mktime(t) == (time_t)-1 && errno) {
        return 8;
    }

    if (f->given[ZONE] && !(f->value[ZONE] >> (t->tm_isdst > 0) & 1)) {
        return 8;
    }

    return 0;
}

struct tm* __kernel_getdate(const char* string, size_t returned) {
    sized("getdate", returned, sizeof(struct tm));

    static struct tm result;
    const char* mask = getenv("DATEMSK");

    if (!mask || !*mask) {
        reason = 1;

        return 0;
    }

    FILE* file = fopen(mask, "re");

    if (!file) {
        reason = errno == ENOMEM ? 6 : 2;

        return 0;
    }

    struct stat st;
    char* line = 0;
    size_t size = 0;
    int error = 7;

    if (fstat(fileno(file), &st) < 0) {
        error = 3;
    } else if (!S_ISREG(st.st_mode)) {
        error = 4;
    } else {
        for (ssize_t n; (n = getline(&line, &size, file)) >= 0;) {
            Fields f = {{0}, {0}};
            const char* end = match(line, string, &f, 0);

            if (end && !*skip(end)) {
                error = resolve(&f, &result);
                break;
            }
        }

        if (error == 7 && ferror(file)) {
            error = errno == ENOMEM ? 6 : 5;
        }
    }

    free(line);
    fclose(file);

    if (error) {
        reason = error;

        return 0;
    }

    return &result;
}
