/*
 * glob, the kernel's, but for the home directory a pattern's ~ names with
 * GLOB_TILDE or GLOB_TILDE_CHECK: HOME's for ~ alone, the user's of
 * /etc/passwd for ~user, none for a user there is not.
 */
#include "sized.h"

#include <errno.h>
#include <glob.h>
#include <pwd.h>
#include <stdlib.h>
#include <string.h>

int __glob_home(const char* restrict, int, int (*)(const char*, int), glob_t* restrict, const char*);

int __kernel_glob(const char* restrict pattern, int flags, int (*errfunc)(const char*, int), glob_t* restrict g, size_t size) {
    sized("glob", size, sizeof(glob_t));

    const char* home = NULL;
    char* user = NULL;
    int saved = errno;

    if (flags & (GLOB_TILDE | GLOB_TILDE_CHECK) && pattern[0] == '~') {
        size_t n = strcspn(pattern + 1, "/");

        if (n == 0) {
            home = getenv("HOME");
        } else if ((user = strndup(pattern + 1, n))) {
            struct passwd* pw = getpwnam(user);

            home = pw ? pw->pw_dir : NULL;
        }
    }

    errno = saved;

    int result = __glob_home(pattern, flags, errfunc, g, home);

    free(user);

    return result;
}
