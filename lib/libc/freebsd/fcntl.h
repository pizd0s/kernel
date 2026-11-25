#pragma once

/*
 * lib/c's fcntl.h for FreeBSD's sources, with what FreeBSD's adds and they
 * take: O_VERIFY, fopen's "v", and O_SHLOCK and O_EXLOCK, which mkostemp
 * takes, which ask nothing of lib/c's files, and F_DUP2FD_CLOEXEC, dup3's,
 * which namespace.h's _fcntl gives.
 */
#include_next <fcntl.h>

#define O_VERIFY 0
#define O_SHLOCK 0
#define O_EXLOCK 0
#define F_DUP2FD_CLOEXEC 0x7fff0001
