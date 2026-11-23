/* GLib's config.h for the system: what its meson.build finds of lib/c,
   wasm32. Threads are POSIX's, lib/c's one thread; printf is the system's;
   messages go through lib/c's gettext. */
#pragma once

#define _GNU_SOURCE 1

#define ENABLE_NLS 1
#define GETTEXT_PACKAGE "glib20"
#define GLIB_LOCALE_DIR "/usr/share/locale"
#define GLIB_BINARY_AGE 9001
#define GLIB_INTERFACE_AGE 1
#define GLIB_MAJOR_VERSION 2
#define GLIB_MINOR_VERSION 90
#define GLIB_MICRO_VERSION 1

#define THREADS_POSIX 1
#define USE_SYSTEM_PRINTF 1
#define HAVE_VASPRINTF 1

#define SIZEOF_CHAR 1
#define SIZEOF_INT 4
#define SIZEOF_VOID_P 4
#define SIZEOF_WCHAR_T 4

#define HAVE_ALIGNED_ALLOC 1
#define HAVE_BIND_TEXTDOMAIN_CODESET 1
#define HAVE_CLOCK_NANOSLEEP 1
#define HAVE_CODESET 1
#define HAVE_DIRENT_H 1
#define HAVE_FSYNC 1
#define HAVE_DECL_GETC_UNLOCKED 1
#define HAVE_GETPWUID_R 1
#define HAVE_GETRESUID 1
#define HAVE_GMTIME_R 1
#define HAVE_LANGINFO_CODESET 1
#define HAVE_LANGINFO_TIME 1
#define HAVE_LOCALTIME_R 1
#define HAVE_LSTAT 1
#define HAVE_MEMALIGN 1
#define HAVE_MEMMEM 1
#define HAVE_MKOSTEMP 1
#define HAVE_MMAP 1
#define HAVE_NEWLOCALE 1
#define HAVE_O_CLOEXEC 1
#define HAVE_PIPE2 1
#define HAVE_POLL 1
#define HAVE_POLL_H 1
#define HAVE_POSIX_MEMALIGN 1
#define HAVE_PPOLL 1
#define HAVE_PRCTL 1
#define HAVE_PTHREAD_ATTR_SETSTACKSIZE 1
#define HAVE_PTHREAD_CONDATTR_SETCLOCK 1
#define HAVE_READLINK 1
#define HAVE_SCHED_H 1
#define HAVE_SETENV 1
#define HAVE_SIG_ATOMIC_T 1
#define HAVE_SOCK_CLOEXEC 1
#define HAVE_STDINT_H 1
#define HAVE_STPCPY 1
#define HAVE_STRCASECMP 1
#define HAVE_STRERROR_R 1
#define HAVE_STRLCPY 1
#define HAVE_STRNCASECMP 1
#define HAVE_STRSIGNAL 1
#define HAVE_SYSLOG_H 1
#define HAVE_SYS_PARAM_H 1
#define HAVE_SYS_PRCTL_H 1
#define HAVE_SYS_RESOURCE_H 1
#define HAVE_SYS_SELECT_H 1
#define HAVE_SYS_TIME_H 1
#define HAVE_TIMEGM 1
#define HAVE_UNSETENV 1
#define HAVE_USELOCALE 1
#define HAVE_WCHAR_H 1
