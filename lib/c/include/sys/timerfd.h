#pragma once

#include <__base.h>
#include <fcntl.h>
#include <time.h>

__BEGIN_DECLS

/*
 * timerfd, Linux's: a descriptor of a timer of CLOCK_REALTIME,
 * CLOCK_MONOTONIC or CLOCK_BOOTTIME, the kernel's. A read of 8 bytes
 * gives how often it expired since the last read or setting and waits
 * while that is never; poll finds it readable then. timerfd_settime arms
 * it for it_value, from now or, with TFD_TIMER_ABSTIME, a time of its
 * clock, past ones at once, then every it_interval; an it_value of 0
 * disarms it. TFD_TIMER_CANCEL_ON_SET is taken and never cancels, as
 * nothing in the system sets the clock.
 */
#define TFD_NONBLOCK O_NONBLOCK
#define TFD_CLOEXEC O_CLOEXEC

#define TFD_TIMER_ABSTIME 1
#define TFD_TIMER_CANCEL_ON_SET 2

int timerfd_create(int, int);
int timerfd_settime(int, int, const struct itimerspec*, struct itimerspec*);
int timerfd_gettime(int, struct itimerspec*);

__END_DECLS
