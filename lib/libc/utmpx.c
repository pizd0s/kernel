/*
 * The user accounting database, UTMPX_FILE: its records, struct utmpx as
 * they are, read from a position of the process's, the record after the
 * last one read. pututxline replaces the record getutxid would find,
 * searching from the last one read on and then from the start, or adds
 * one at the end. Each function reads the file as it is then; a result is
 * a copy of the record, the same place every time.
 */
#include <errno.h>
#include <fcntl.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>
#include <utmpx.h>

static size_t position;
static struct utmpx result;

// The file's records, a heap's array, and how many: none where it cannot
// be read.
static struct utmpx* records(size_t* count) {
    int fd = open(UTMPX_FILE, O_RDONLY | O_CLOEXEC);
    struct stat st;
    struct utmpx* all = NULL;
    size_t n = 0;

    if (fd >= 0 && fstat(fd, &st) == 0 && st.st_size >= (off_t)sizeof *all) {
        size_t want = st.st_size / sizeof *all;

        all = malloc(want * sizeof *all);

        if (all) {
            ssize_t got = pread(fd, all, want * sizeof *all, 0);

            n = got > 0 ? (size_t)got / sizeof *all : 0;
        }
    }

    if (fd >= 0) {
        close(fd);
    }

    *count = n;

    return all;
}

static int process(short type) {
    return type == INIT_PROCESS || type == LOGIN_PROCESS || type == USER_PROCESS || type == DEAD_PROCESS;
}

static int sameId(const struct utmpx* r, const struct utmpx* id) {
    switch (id->ut_type) {
        case RUN_LVL:
        case BOOT_TIME:
        case OLD_TIME:
        case NEW_TIME:
            return r->ut_type == id->ut_type;
        case INIT_PROCESS:
        case LOGIN_PROCESS:
        case USER_PROCESS:
        case DEAD_PROCESS:
            return process(r->ut_type) && memcmp(r->ut_id, id->ut_id, sizeof r->ut_id) == 0;
    }

    return 0;
}

static int sameLine(const struct utmpx* r, const struct utmpx* line) {
    return (r->ut_type == LOGIN_PROCESS || r->ut_type == USER_PROCESS) && strncmp(r->ut_line, line->ut_line, sizeof r->ut_line) == 0;
}

enum Match { any, byId, byLine };

static struct utmpx* search(const struct utmpx* want, enum Match match) {
    struct utmpx key = want ? *want : (struct utmpx){0};
    size_t n;
    struct utmpx* all = records(&n);
    struct utmpx* found = NULL;

    for (size_t i = position; i < n && !found; i++) {
        if (match == any || (match == byId ? sameId(&all[i], &key) : sameLine(&all[i], &key))) {
            result = all[i];
            found = &result;
            position = i + 1;
        }
    }

    if (!found) {
        position = n;
    }

    free(all);

    return found;
}

void __kernel_setutxent(void) {
    position = 0;
}

void __kernel_endutxent(void) {
    position = 0;
}

struct utmpx* __kernel_getutxent(size_t size) {
    return search(NULL, any);
}

struct utmpx* __kernel_getutxid(const struct utmpx* id, size_t idSize, size_t size) {
    return search(id, byId);
}

struct utmpx* __kernel_getutxline(const struct utmpx* line, size_t lineSize, size_t size) {
    return search(line, byLine);
}

struct utmpx* __kernel_pututxline(const struct utmpx* ut, size_t utSize, size_t size) {
    struct utmpx record = *ut;
    int fd = open(UTMPX_FILE, O_RDWR | O_CREAT | O_CLOEXEC, 0644);

    if (fd < 0) {
        return NULL;
    }

    size_t n;
    struct utmpx* all = records(&n);
    size_t from = position > 0 ? position - 1 : 0;
    size_t slot = n;

    from = from < n ? from : n;

    for (size_t k = 0; k < n; k++) {
        size_t i = (from + k) % n;

        if (sameId(&all[i], &record)) {
            slot = i;

            break;
        }
    }

    free(all);

    ssize_t wrote = pwrite(fd, &record, sizeof record, (off_t)slot * sizeof record);
    int e = wrote < 0 ? errno : EIO;

    close(fd);

    if (wrote != sizeof record) {
        errno = e;

        return NULL;
    }

    position = slot + 1;
    result = record;

    return &result;
}
