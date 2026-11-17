#include <__base.h>

/*
 * The functions ctype.h inlines, for a pointer to one and a call without
 * the header: the header's own definitions, which are the functions here,
 * with __INLINE nothing.
 */
#undef __INLINE
#define __INLINE

#include <ctype.h>
