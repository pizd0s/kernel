#pragma once

/*
 * libfuse 2.9's high-level API, as the system's libfuse has it (lib/fuse):
 * a file system's operations on paths, fuse_main to serve them, and the
 * context of the call being served. fuse_main registers the file system
 * at rootfs's /dev/init by the name its mount point argument gives, and
 * serves it on one thread, a file system of the VFS's by node.
 */

#ifndef FUSE_USE_VERSION
    #define FUSE_USE_VERSION 21
#endif

#include <__base.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <sys/types.h>
#include <sys/uio.h>
#include <time.h>
#include <utime.h>

#include "fuse_common.h"

__BEGIN_DECLS

struct fuse;

typedef int (*fuse_fill_dir_t)(void* buf, const char* name, const struct stat* stbuf, off_t off);
typedef struct fuse_dirhandle* fuse_dirh_t;
typedef int (*fuse_dirfil_t)(fuse_dirh_t h, const char* name, int type, ino_t ino);

struct fuse_operations {
    int (*getattr)(const char*, struct stat*);
    int (*readlink)(const char*, char*, size_t);
    int (*getdir)(const char*, fuse_dirh_t, fuse_dirfil_t);
    int (*mknod)(const char*, mode_t, dev_t);
    int (*mkdir)(const char*, mode_t);
    int (*unlink)(const char*);
    int (*rmdir)(const char*);
    int (*symlink)(const char*, const char*);
    int (*rename)(const char*, const char*);
    int (*link)(const char*, const char*);
    int (*chmod)(const char*, mode_t);
    int (*chown)(const char*, uid_t, gid_t);
    int (*truncate)(const char*, off_t);
    int (*utime)(const char*, struct utimbuf*);
    int (*open)(const char*, struct fuse_file_info*);
    int (*read)(const char*, char*, size_t, off_t, struct fuse_file_info*);
    int (*write)(const char*, const char*, size_t, off_t, struct fuse_file_info*);
    int (*statfs)(const char*, struct statvfs*);
    int (*flush)(const char*, struct fuse_file_info*);
    int (*release)(const char*, struct fuse_file_info*);
    int (*fsync)(const char*, int, struct fuse_file_info*);
    int (*setxattr)(const char*, const char*, const char*, size_t, int);
    int (*getxattr)(const char*, const char*, char*, size_t);
    int (*listxattr)(const char*, char*, size_t);
    int (*removexattr)(const char*, const char*);
    int (*opendir)(const char*, struct fuse_file_info*);
    int (*readdir)(const char*, void*, fuse_fill_dir_t, off_t, struct fuse_file_info*);
    int (*releasedir)(const char*, struct fuse_file_info*);
    int (*fsyncdir)(const char*, int, struct fuse_file_info*);
    void* (*init)(struct fuse_conn_info* conn);
    void (*destroy)(void*);
    int (*access)(const char*, int);
    int (*create)(const char*, mode_t, struct fuse_file_info*);
    int (*ftruncate)(const char*, off_t, struct fuse_file_info*);
    int (*fgetattr)(const char*, struct stat*, struct fuse_file_info*);
    int (*lock)(const char*, struct fuse_file_info*, int cmd, struct flock*);
    int (*utimens)(const char*, const struct timespec tv[2]);
    int (*bmap)(const char*, size_t blocksize, uint64_t* idx);
    unsigned int flag_nullpath_ok : 1;
    unsigned int flag_nopath : 1;
    unsigned int flag_utime_omit_ok : 1;
    unsigned int flag_reserved : 29;
    int (*ioctl)(const char*, int cmd, void* arg, struct fuse_file_info*, unsigned int flags, void* data);
    int (*poll)(const char*, struct fuse_file_info*, struct fuse_pollhandle* ph, unsigned* reventsp);
    int (*write_buf)(const char*, struct fuse_bufvec* buf, off_t off, struct fuse_file_info*);
    int (*read_buf)(const char*, struct fuse_bufvec** bufp, size_t size, off_t off, struct fuse_file_info*);
    int (*flock)(const char*, struct fuse_file_info*, int op);
    int (*fallocate)(const char*, int, off_t, off_t, struct fuse_file_info*);
};

struct fuse_context {
    struct fuse* fuse;
    uid_t uid;
    gid_t gid;
    pid_t pid;
    void* private_data;
    mode_t umask;
};

/*
 * Serves the file system: argv's first argument that is no option names it
 * in /dev, its -o options are taken and those of libfuse's own let be;
 * -o ready=fd is a descriptor closed once the file system is there. It
 * returns when rootfs lets go of the name or the mount ends: 0, or 1 for
 * arguments it cannot take, 3 for a name it cannot register.
 */
int fuse_main_real(int argc, char* argv[], const struct fuse_operations* op, size_t op_size, void* user_data);
#define fuse_main(argc, argv, op, user_data) fuse_main_real(argc, argv, op, sizeof(*(op)), user_data)

struct fuse_context* fuse_get_context(void);
int fuse_getgroups(int size, gid_t list[]);
int fuse_interrupted(void);

__END_DECLS
