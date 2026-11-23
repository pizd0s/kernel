#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

/*
 * Temporary files and names in P_tmpdir, made by the kernel's mkstemp and
 * mktemp: a name of tmpnam was free when made, as POSIX allows.
 */

FILE* __kernel_tmpfile(void) {
    char path[] = P_tmpdir "/tmpfile_XXXXXX";
    int fd = mkstemp(path);

    if (fd < 0) {
        return 0;
    }

    unlink(path);

    FILE* f = fdopen(fd, "w+");

    if (!f) {
        close(fd);
    }

    return f;
}

char* __kernel_tmpnam(char* buf) {
    static char internal[L_tmpnam];
    char path[] = P_tmpdir "/tmpnam_XXXXXX";

    if (!*mktemp(path)) {
        return 0;
    }

    return strcpy(buf ? buf : internal, path);
}
