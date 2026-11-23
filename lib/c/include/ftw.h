#pragma once

#include <__base.h>
#include <sys/stat.h>

__BEGIN_DECLS

/*
 * A walk of a file tree, calling the program's function for each name:
 * POSIX.1-2024's nftw and POSIX.1-2017's ftw (lib/libc's nftw.c).
 */
#define FTW_F 1
#define FTW_D 2
#define FTW_DNR 3
#define FTW_NS 4
#define FTW_SL 5
#define FTW_DP 6
#define FTW_SLN 7

#define FTW_PHYS 1
#define FTW_MOUNT 2
#define FTW_CHDIR 4
#define FTW_DEPTH 8
#define FTW_XDEV 16

struct FTW {
    int base;
    int level;
};

int ftw(const char*, int (*)(const char*, const struct stat*, int), int);
int nftw(const char*, int (*)(const char*, const struct stat*, int, struct FTW*), int, int);

__END_DECLS
