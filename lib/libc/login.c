/*
 * The login name is LOGNAME's: getlogin's is its value, of none where it
 * is unset; getlogin_r's a copy, ENXIO where it is unset and ERANGE where
 * it is longer than the room.
 */
#include <errno.h>
#include <stdlib.h>
#include <string.h>

char* __kernel_getlogin(void) {
    return getenv("LOGNAME");
}

int __kernel_getlogin_r(char* buf, size_t size) {
    const char* name = getenv("LOGNAME");

    if (!name) {
        return ENXIO;
    }

    size_t n = strlen(name);

    if (n >= size) {
        return ERANGE;
    }

    memcpy(buf, name, n + 1);

    return 0;
}
