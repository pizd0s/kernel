#pragma once

/*
 * tzcode's configuration, FreeBSD's but of POSIX's variables: tzset keeps
 * timezone and daylight as it keeps tzname, and none of the three is its
 * own: they are the program's (lib/c's crt.c), where lib/c's __program
 * says, and are named so after time.h has declared them and sys/time.h
 * its struct timezone.
 */
#define TM_GMTOFF tm_gmtoff
#define TM_ZONE tm_zone

#define HAVE_GETTEXT false
#define HAVE_SYS_STAT_H true
#define HAVE_UNISTD_H true
#define HAVE_STDINT_H true

#define PCTS 1
#define NETBSD_INSPIRED 0
#define STD_INSPIRED 1
#define HAVE_TZNAME 1
#define USG_COMPAT 1
#define ALTZONE 0

#include <sys/time.h>
#include <time.h>

#include "../../c/src/kernel.h"

#define tzname (__program->tznameAt)
#define timezone (*__program->timezoneAt)
#define daylight (*__program->daylightAt)
