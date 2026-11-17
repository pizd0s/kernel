#include <__base.h>

/*
 * The functions stdlib.h, string.h and strings.h inline, for a pointer to
 * one and a call without the header: the headers' own definitions, which
 * are the functions here, with __INLINE nothing.
 */
#undef __INLINE
#define __INLINE

#include <stdlib.h>
#include <string.h>
#include <strings.h>
