/*
 * A directory's stream, DIR, over posix_getdents: its descriptor, of
 * FD_CLOEXEC, and a buffer of entries, which readdir gives one by one, in
 * posix_dent's layout, struct dirent's; telldir's place is the count of
 * entries read, which seekdir reads again up to from the start. scandir:
 * readdir's entries copied, kept by the program's filter, sorted by its
 * comparison through qsort. The entries come with their size, as the
 * kernel's structures do, the one layout they have had.
 */
#include "sized.h"

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#define ROOM 4096

struct __dirstream {
    int fd;
    size_t at, end;
    long index;
    _Alignas(8) char buf[ROOM];
};

DIR* __kernel_fdopendir(int fd) {
    struct stat st;

    if (fstat(fd, &st) != 0) {
        return NULL;
    }

    if (!S_ISDIR(st.st_mode)) {
        errno = ENOTDIR;

        return NULL;
    }

    DIR* dir = malloc(sizeof *dir);

    if (!dir) {
        errno = ENOMEM;

        return NULL;
    }

    *dir = (DIR){.fd = fd};
    fcntl(fd, F_SETFD, FD_CLOEXEC);

    return dir;
}

DIR* __kernel_opendir(const char* path) {
    int fd = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    DIR* dir = fd < 0 ? NULL : malloc(sizeof *dir);

    if (fd >= 0 && !dir) {
        close(fd);
        errno = ENOMEM;
    }

    if (dir) {
        *dir = (DIR){.fd = fd};
    }

    return dir;
}

int __kernel_closedir(DIR* dir) {
    int fd = dir->fd;

    free(dir);

    return close(fd);
}

struct dirent* __kernel_readdir(DIR* dir, size_t size) {
    sized("readdir", size, sizeof(struct dirent));

    if (dir->at >= dir->end) {
        ssize_t n = posix_getdents(dir->fd, dir->buf, ROOM, 0);

        if (n <= 0) {
            return NULL;
        }

        dir->at = 0;
        dir->end = n;
    }

    struct dirent* entry = (struct dirent*)(dir->buf + dir->at);

    dir->at += entry->d_reclen;
    dir->index++;

    return entry;
}

int __kernel_readdir_r(DIR* restrict dir, struct dirent* restrict entry, size_t entrySize, struct dirent** restrict result,
                       size_t resultSize) {
    sized("readdir_r", entrySize, sizeof(struct dirent));
    sized("readdir_r", resultSize, sizeof(struct dirent));

    int saved = errno;

    errno = 0;

    struct dirent* e = readdir(dir);
    int error = errno;

    errno = saved;
    *result = NULL;

    if (!e) {
        return error;
    }

    memcpy(entry, e, offsetof(struct dirent, d_name) + strlen(e->d_name) + 1);
    *result = entry;

    return 0;
}

void __kernel_rewinddir(DIR* dir) {
    lseek(dir->fd, 0, SEEK_SET);
    dir->at = dir->end = 0;
    dir->index = 0;
}

void __kernel_seekdir(DIR* dir, long at) {
    rewinddir(dir);

    while (dir->index < at && readdir(dir)) {
    }
}

long __kernel_telldir(DIR* dir) {
    return dir->index;
}

int __kernel_dirfd(DIR* dir) {
    return dir->fd;
}

int __kernel_scandir(const char* path, struct dirent*** list, size_t size, int (*filter)(const struct dirent*),
                     int (*compare)(const struct dirent**, const struct dirent**)) {
    sized("scandir", size, sizeof(struct dirent));

    DIR* dir = opendir(path);

    if (!dir) {
        return -1;
    }

    struct dirent** entries = 0;
    size_t count = 0;
    size_t capacity = 0;
    int saved = errno;
    struct dirent* entry;

    errno = 0;

    while ((entry = readdir(dir))) {
        if (filter && !filter(entry)) {
            continue;
        }

        if (count == capacity) {
            capacity = capacity ? 2 * capacity : 16;
            struct dirent** grown = realloc(entries, capacity * sizeof *entries);

            if (!grown) {
                break;
            }

            entries = grown;
        }

        size_t size = offsetof(struct dirent, d_name) + strlen(entry->d_name) + 1;
        struct dirent* copy = malloc(size);

        if (!copy) {
            break;
        }

        memcpy(copy, entry, size);
        entries[count++] = copy;
    }

    closedir(dir);

    if (errno) {
        while (count) {
            free(entries[--count]);
        }

        free(entries);

        return -1;
    }

    errno = saved;

    if (compare) {
        qsort(entries, count, sizeof *entries, (int (*)(const void*, const void*))compare);
    }

    *list = entries;

    return (int)count;
}
