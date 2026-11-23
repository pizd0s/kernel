#pragma once

/*
 * lib/c's sys/file.h for FreeBSD's sources, with fcntl.h, which FreeBSD's
 * includes: its stdio's mode flags take open's.
 */
#include <fcntl.h>

#include_next <sys/file.h>
