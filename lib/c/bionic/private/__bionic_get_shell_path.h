#pragma once

/*
 * The shell popen and system run: the system's has one place.
 */
#include <paths.h>

static inline const char* __bionic_get_shell_path(void) {
    return _PATH_BSHELL;
}
