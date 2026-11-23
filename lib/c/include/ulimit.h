#pragma once

#include <__base.h>

__BEGIN_DECLS

/* ulimit, of RLIMIT_FSIZE in blocks of 512 bytes: UL_SETFSIZE sets its soft and hard limits. */
#define UL_GETFSIZE 1
#define UL_SETFSIZE 2

long ulimit(int, ...);

__END_DECLS
