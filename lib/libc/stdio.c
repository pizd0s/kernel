/*
 * fopencookie, FreeBSD's: the stream's functions come by their place and
 * with their size, as the kernel's structures do, the one layout they have
 * had.
 */
#include <stdio.h>

FILE* __kernel_fopencookie(void* cookie, const char* mode, const cookie_io_functions_t* functions, size_t size) {
    return fopencookie(cookie, mode, *functions);
}
