/*
 * The local time: the zone is TZ's, read again when TZ changes, and the
 * kernel's Go converts by it (kernel.h). TZ unset is /etc/localtime, empty
 * UTC; one of a colon, or of a slash before any comma, is a TZif file, a
 * path where it begins with / or ., else a name under /usr/share/zoneinfo,
 * /share/zoneinfo or /etc/zoneinfo; anything else is a POSIX rule. A zone
 * of no file that can be read is UTC.
 */
#include "kernel.h"

#include <errno.h>
#include <fcntl.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

#define LARGEST (1 << 20)

// TZ as the zone was last set from, and whether it was set.
static char* last;
static int loaded;
static int unset;

// Hands the kernel the file's zone: whether it is one.
static int fromFile(const char* path, const char* name) {
    int fd = open(path, O_RDONLY | O_CLOEXEC);

    if (fd < 0) {
        return 0;
    }

    struct stat st;
    size_t room = fstat(fd, &st) == 0 && st.st_size > 0 && st.st_size < LARGEST ? st.st_size : LARGEST;
    char* data = malloc(room);
    size_t n = 0;

    for (ssize_t got; data && n < room && (got = read(fd, data + n, room - n)) != 0;) {
        if (got < 0 && errno != EINTR) {
            break;
        }

        n += got > 0 ? got : 0;
    }

    close(fd);

    int ok = data && n >= 44 && memcmp(data, "TZif", 4) == 0 && __kernel_zone(name, data, n) == 0;

    free(data);

    return ok;
}

static void set(const char* tz) {
    static const char* const directories[] = {"/usr/share/zoneinfo/", "/share/zoneinfo/", "/etc/zoneinfo/"};
    const char* slash = tz ? strchr(tz, '/') : NULL;
    const char* comma = tz ? strchr(tz, ',') : NULL;
    int saved = errno;

    if (!tz) {
        if (!fromFile("/etc/localtime", "/etc/localtime")) {
            __kernel_zone("", NULL, 0);
        }
    } else if (*tz && (tz[0] == ':' || (slash && (!comma || comma > slash)))) {
        const char* name = tz + (tz[0] == ':');
        size_t n = strlen(name);
        int ok = 0;

        if (name[0] == '/' || name[0] == '.') {
            ok = fromFile(name, name);
        } else if (n <= 255 && !strchr(name, '.')) {
            char path[300];

            for (size_t i = 0; i < 3 && !ok; i++) {
                strcpy(path, directories[i]);
                strcat(path, name);
                ok = fromFile(path, name);
            }
        }

        if (!ok) {
            __kernel_zone("", NULL, 0);
        }
    } else {
        __kernel_zone(tz, NULL, 0);
    }

    errno = saved;
}

// The zone of TZ now, set where it is not yet, or always.
static void zone(int always) {
    const char* tz = getenv("TZ");

    if (!always && loaded && (tz ? !unset && strcmp(tz, last) == 0 : unset)) {
        return;
    }

    set(tz);
    free(last);
    last = tz ? strdup(tz) : NULL;
    unset = !tz;
    loaded = !tz || last;
}

void __kernel_tzset(void) {
    zone(1);
}

struct tm* __kernel_localtime_r(const time_t* restrict t, struct tm* restrict tm, size_t tmSize, size_t size) {
    zone(0);

    return __kernel_tolocal(t, tm, size);
}

struct tm* __kernel_localtime(const time_t* t, size_t size) {
    static struct tm tm;

    return __kernel_localtime_r(t, &tm, sizeof tm, size);
}

time_t __kernel_mktime(struct tm* tm, size_t size) {
    zone(0);

    return __kernel_fromlocal(tm, size);
}

char* __kernel_ctime_r(const time_t* t, char* buf) {
    struct tm tm;

    return __kernel_localtime_r(t, &tm, sizeof tm, sizeof tm) ? asctime_r(&tm, buf) : NULL;
}

char* __kernel_ctime(const time_t* t) {
    static char text[26];

    return __kernel_ctime_r(t, text);
}
