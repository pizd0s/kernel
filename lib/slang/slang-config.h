/* S-Lang's config.h for the system: what its configure finds of lib/c,
   wasm32, for the part of the library bin/mc/make.py builds, its terminal
   and screen. The terminal's description is read from the terminfo
   database, S-Lang's own reader of it; there is no termcap. */
#ifndef SL_CONFIG_H
#define SL_CONFIG_H

#define HAVE_LONG_LONG 1
#define SIZEOF_SHORT 2
#define SIZEOF_INT 4
#define SIZEOF_LONG 4
#define SIZEOF_LONG_LONG 8
#define SIZEOF_FLOAT 4
#define SIZEOF_DOUBLE 8
#define SIZEOF_OFF_T 8
#define SIZEOF_SIZE_T 4

#define HAVE_LANGINFO_H 1
#define HAVE_LOCALE_H 1
#define HAVE_MALLOC_H 1
#define HAVE_STDLIB_H 1
#define HAVE_SYS_TYPES_H 1
#define HAVE_SYS_WAIT_H 1
#define HAVE_TERMIOS_H 1
#define HAVE_UNISTD_H 1

#define HAVE_ATEXIT 1
#define HAVE_CFGETOSPEED 1
#define HAVE_GETEUID 1
#define HAVE_GETGID 1
#define HAVE_GETUID 1
#define HAVE_MEMCHR 1
#define HAVE_MEMCMP 1
#define HAVE_MEMCPY 1
#define HAVE_MEMSET 1
#define HAVE_NL_LANGINFO_CODESET 1
#define HAVE_SIGACTION 1
#define HAVE_SIGADDSET 1
#define HAVE_SIGEMPTYSET 1
#define HAVE_SIGPROCMASK 1
#define HAVE_TCGETATTR 1
#define HAVE_TCSETATTR 1
#define HAVE_VSNPRINTF 1

#define SLANG_POSIX_SIGNALS

#ifndef unix
#define unix 1
#endif

#ifndef __unix__
#define __unix__ 1
#endif

#endif
