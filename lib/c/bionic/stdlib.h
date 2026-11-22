#pragma once

/*
 * lib/c's stdlib.h for Bionic's sources, with sys/types.h's types and
 * stdint.h's, which Bionic's stdlib.h gives, through sys/types.h, and
 * NetBSD's rand48 takes (int16_t).
 */
#include <stdint.h>
#include <sys/types.h>

#include_next <stdlib.h>
