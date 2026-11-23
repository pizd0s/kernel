#pragma once

#include <__base.h>

__BEGIN_DECLS

struct tm {
    int tm_sec;
    int tm_min;
    int tm_hour;
    int tm_mday;
    int tm_mon;
    int tm_year;
    int tm_wday;
    int tm_yday;
    int tm_isdst;
    long tm_gmtoff;
    const char* tm_zone;
};

struct itimerspec {
    struct timespec it_interval;
    struct timespec it_value;
};

#define CLOCKS_PER_SEC 1000000L
#define TIME_UTC 1

#define CLOCK_REALTIME 0
#define CLOCK_MONOTONIC 1
#define CLOCK_PROCESS_CPUTIME_ID 2
#define CLOCK_THREAD_CPUTIME_ID 3
#define CLOCK_MONOTONIC_RAW 4
#define CLOCK_REALTIME_COARSE 5
#define CLOCK_MONOTONIC_COARSE 6
#define CLOCK_BOOTTIME 7
#define TIMER_ABSTIME 1

extern char* tzname[2];
extern long timezone;
extern int daylight;

time_t time(time_t*);
clock_t clock(void);
double difftime(time_t, time_t);
time_t mktime(struct tm*);
time_t timegm(struct tm*);
struct tm* gmtime(const time_t*);
struct tm* gmtime_r(const time_t* __restrict, struct tm* __restrict);
struct tm* localtime(const time_t*);
struct tm* localtime_r(const time_t* __restrict, struct tm* __restrict);
char* asctime(const struct tm*);
char* asctime_r(const struct tm* __restrict, char* __restrict);
char* ctime(const time_t*);
char* ctime_r(const time_t*, char*);
size_t strftime(char* __restrict, size_t, const char* __restrict, const struct tm* __restrict);
char* strptime(const char* __restrict, const char* __restrict, struct tm* __restrict);
int timespec_get(struct timespec*, int);

/*
 * Timers of a process, the kernel's, of CLOCK_REALTIME, CLOCK_MONOTONIC
 * or CLOCK_BOOTTIME; a CPU-time clock's ENOTSUP. One tells its expiry as
 * its sigevent says, SIGALRM with its id as the value without one; a
 * signal of it pending, another expiry counts in timer_getoverrun's, up
 * to DELAYTIMER_MAX. A child of fork has none; exec ends them.
 */
struct sigevent;

int timer_create(clockid_t, struct sigevent* __restrict, timer_t* __restrict);
int timer_delete(timer_t);
int timer_settime(timer_t, int, const struct itimerspec* __restrict, struct itimerspec* __restrict);
int timer_gettime(timer_t, struct itimerspec*);
int timer_getoverrun(timer_t);
// getdate_err is a macro, as POSIX lets it be: the kernel's int, which
// __getdate_err is the address of.
int* __getdate_err(void) __attribute__((__const__));
#define getdate_err (*__getdate_err())
struct tm* getdate(const char*);
void tzset(void);
int nanosleep(const struct timespec*, struct timespec*);
int clock_gettime(clockid_t, struct timespec*);
int clock_settime(clockid_t, const struct timespec*);
int clock_getres(clockid_t, struct timespec*);
int clock_getcpuclockid(pid_t, clockid_t*);
int clock_nanosleep(clockid_t, int, const struct timespec*, struct timespec*);
int stime(const time_t*);

size_t strftime_l(char* __restrict, size_t, const char* __restrict, const struct tm* __restrict, locale_t);

__END_DECLS
