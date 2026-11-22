#pragma once

/*
 * lib/c's sys/file.h for Bionic's sources, with fcntl.h, which Bionic's
 * includes, as OpenBSD's does: its stdio's mode flags take open's.
 */
#include <fcntl.h>

#include_next <sys/file.h>
