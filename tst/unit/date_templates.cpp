//@ host: false
//@ env: TZ=UTC0
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "date templates: ok\n"

// getdate, POSIX.1-2024's: the first line of the DATEMSK file that
// matches all of the input, ignoring case and extra white space, a
// number of two digits at most; what the input leaves out from now by
// POSIX's rules, its examples among them: a weekday alone the first such
// day from today, a month without a year the first from this one and its
// first day, a weekday with a month the first such day in it, a time
// with 0 for what it leaves out and, without a date, the first such hour
// from now, nothing of a time the current one; %y's century; %Z one of
// the zone's names; getdate_err's 1 to 8 for no DATEMSK, no file, not a
// regular file, no line matching, a day the month has not and a zone
// that is not the zone's. host: false: the host musl's getdate fills
// nothing in, keeps the fields of the last call and reads 99 bytes of a
// line.

#include <assert.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

namespace {
    const char* templates =
        "%m/%d/%y %I %p\n"
        "%m/%d/%y\n"
        "%A %B %d, %Y, %H:%M:%S\n"
        "%d,%m,%Y %H:%M\n"
        "at %A the %dst of %B in %Y\n"
        "run job at %I %p,%B %dnd\n"
        "%H:%M %Z\n"
        "%b %a %Y\n"
        "%b %a\n"
        "%b %H:%S\n"
        "%a %H\n"
        "%H:%M\n"
        "%A\n"
        "%B\n";

    struct tm now;

    struct tm* date(const char* input) {
        getdate_err = 0;
        return getdate(input);
    }

    int failure(const char* input) {
        assert(date(input) == nullptr);
        return getdate_err;
    }

    // The result's date and time, and, where the input gives no time,
    // the current one within the seconds the call took.
    void expect(const char* input, int year, int month, int day, int hour, int minute, int second) {
        struct tm* t = date(input);
        if (!t) {
            printf("%s: getdate_err %d\n", input, getdate_err);
            assert(false);
        }
        if (hour < 0) {
            hour = now.tm_hour;
            minute = now.tm_min;
            second = now.tm_sec;
        }
        struct tm want = {};
        want.tm_year = year - 1900;
        want.tm_mon = month - 1;
        want.tm_mday = day;
        want.tm_hour = hour;
        want.tm_min = minute;
        want.tm_sec = second;
        struct tm got = *t;
        time_t a = timegm(&want);
        time_t b = timegm(&got);
        if (b < a || b > a + 2) {
            printf("%s: %04d-%02d-%02d %02d:%02d:%02d, expected %04d-%02d-%02d %02d:%02d:%02d\n", input,
                   t->tm_year + 1900, t->tm_mon + 1, t->tm_mday, t->tm_hour, t->tm_min, t->tm_sec, year, month, day,
                   hour, minute, second);
            assert(false);
        }
    }

    // A day from now on, as year, month and day.
    void ahead(int days, int* year, int* month, int* day) {
        struct tm t = now;
        t.tm_mday += days;
        t.tm_hour = 12;
        timegm(&t);
        *year = t.tm_year + 1900;
        *month = t.tm_mon + 1;
        *day = t.tm_mday;
    }

    // The first day on or after year-month-1 that is weekday.
    int first(int year, int month, int weekday) {
        struct tm t = {};
        t.tm_year = year - 1900;
        t.tm_mon = month - 1;
        t.tm_mday = 1;
        t.tm_hour = 12;
        timegm(&t);
        return 1 + (weekday - t.tm_wday + 7) % 7;
    }

    void relative() {
        int y;
        int m;
        int d;
        int thisYear = now.tm_year + 1900;
        ahead((1 - now.tm_wday + 7) % 7, &y, &m, &d);
        expect("Mon", y, m, d, -1, 0, 0);
        ahead((5 - now.tm_wday + 7) % 7, &y, &m, &d);
        expect("Fri 9", y, m, d, 9, 0, 0);
        expect("friday 9", y, m, d, 9, 0, 0);
        int september = thisYear + (now.tm_mon > 8);
        expect("September", september, 9, 1, -1, 0, 0);
        expect("Sep Mon", september, 9, first(september, 9, 1), -1, 0, 0);
        int february = thisYear + (now.tm_mon > 1);
        expect("Feb 10:30", february, 2, 1, 10, 0, 30);
        ahead(now.tm_hour > 10, &y, &m, &d);
        expect("10:30", y, m, d, 10, 30, 0);
        expect("10:30 UTC", y, m, d, 10, 30, 0);
        ahead(0, &y, &m, &d);
        expect("23:59", y, m, d, 23, 59, 0);
        expect("run job at 3 PM, december 2nd", thisYear, 12, 2, 15, 0, 0);
        expect("Jan Wed 1989", 1989, 1, 4, -1, 0, 0);
        expect("at monday the 1st of december in 1986", 1986, 12, 1, -1, 0, 0);
    }
}

int main() {
    unsetenv("DATEMSK");
    assert(failure("10:30") == 1);
    setenv("DATEMSK", "", 1);
    assert(failure("10:30") == 1);
    setenv("DATEMSK", "none", 1);
    assert(failure("10:30") == 2);
    setenv("DATEMSK", ".", 1);
    assert(failure("10:30") == 4);

    int fd = open("masks", O_CREAT | O_WRONLY | O_TRUNC, 0644);
    assert(fd >= 0 && write(fd, templates, strlen(templates)) == (ssize_t)strlen(templates) && close(fd) == 0);
    setenv("DATEMSK", "masks", 1);
    time_t start = time(nullptr);
    gmtime_r(&start, &now);

    expect("10/1/87 4 PM", 1987, 10, 1, 16, 0, 0);
    expect("10/1/87 12 am", 1987, 10, 1, 0, 0, 0);
    expect("1/2/68", 2068, 1, 2, -1, 0, 0);
    expect("1/2/69", 1969, 1, 2, -1, 0, 0);
    expect("Friday September 18, 1987, 10:30:30", 1987, 9, 18, 10, 30, 30);
    expect("  friday   SEPTEMBER 18,1987,10:30:30 ", 1987, 9, 18, 10, 30, 30);
    assert(date("Friday September 18, 1987, 10:30:30")->tm_wday == 5);
    expect("24,9,1986 10:30", 1986, 9, 24, 10, 30, 0);

    // The rules that take now: again if a clock tick moved the day.
    for (int round = 0;; round++) {
        time_t t = time(nullptr);
        gmtime_r(&t, &now);
        relative();
        struct tm later;
        time_t u = time(nullptr);
        gmtime_r(&u, &later);
        if (later.tm_yday == now.tm_yday && later.tm_hour == now.tm_hour) {
            break;
        }
        assert(round < 3);
    }

    assert(failure("nonsense") == 7);
    assert(failure("10/123/87 4 PM") == 7);
    assert(failure("2/31/87 4 PM") == 8);
    assert(failure("2/29/87") == 8);
    assert(date("2/29/88") && date("2/29/88")->tm_mday == 29);
    assert(failure("10:30 EST") == 8);
    assert(failure("25:00") == 7);

    printf("date templates: ok\n");
}
