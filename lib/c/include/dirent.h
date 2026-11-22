#pragma once

#include <__base.h>

__BEGIN_DECLS

typedef struct __dirstream DIR;

struct dirent {
    ino_t d_ino;
    off_t d_off;
    unsigned short d_reclen;
    unsigned char d_type;
    char d_name[256];
};

#define d_fileno d_ino
#define _DIRENT_HAVE_D_RECLEN
#define _DIRENT_HAVE_D_OFF
#define _DIRENT_HAVE_D_TYPE

#define DT_UNKNOWN 0
#define DT_FIFO 1
#define DT_CHR 2
#define DT_DIR 4
#define DT_BLK 6
#define DT_REG 8
#define DT_LNK 10
#define DT_SOCK 12
#define DT_WHT 14
#define DT_MQ 16
#define DT_SEM 17
#define DT_SHM 18
#define DT_TMO 19

/*
 * What posix_getdents gives of a directory's entries: struct dirent's
 * layout, each entry's length rounded to 8. lib/c makes no file of the
 * types DT_MQ, DT_SEM, DT_SHM and DT_TMO name.
 */
struct posix_dent {
    ino_t d_ino;
    off_t d_off;
    reclen_t d_reclen;
    unsigned char d_type;
    char d_name[];
};

ssize_t posix_getdents(int, void*, size_t, int);

DIR* opendir(const char*);
DIR* fdopendir(int);
int closedir(DIR*);
struct dirent* readdir(DIR*);
int readdir_r(DIR* __restrict, struct dirent* __restrict, struct dirent** __restrict);
void rewinddir(DIR*);
void seekdir(DIR*, long);
long telldir(DIR*);
int dirfd(DIR*);
int alphasort(const struct dirent**, const struct dirent**);
int versionsort(const struct dirent**, const struct dirent**);
int scandir(const char*, struct dirent***, int (*)(const struct dirent*), int (*)(const struct dirent**, const struct dirent**));

__END_DECLS
