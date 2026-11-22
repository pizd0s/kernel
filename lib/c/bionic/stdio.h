#pragma once

/*
 * lib/c's stdio.h for Bionic's sources, with what Bionic's includes and
 * its sources take of it: sys/types.h's types (u_char), and stdint.h's,
 * which Bionic's sys/types.h includes.
 */
#include <stdint.h>
#include <sys/types.h>

#include_next <stdio.h>
