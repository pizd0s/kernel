#pragma once

/*
 * What libfuse 2.9's two APIs share, as the system's libfuse has it
 * (lib/fuse): the version, an open file's information, the connection's.
 */

#include <__base.h>
#include <stdint.h>
#include <sys/types.h>

#include "fuse_opt.h"

#define FUSE_MAJOR_VERSION 2
#define FUSE_MINOR_VERSION 9
#define FUSE_MAKE_VERSION(maj, min) ((maj) * 10 + (min))
#define FUSE_VERSION FUSE_MAKE_VERSION(FUSE_MAJOR_VERSION, FUSE_MINOR_VERSION)

__BEGIN_DECLS

struct fuse_file_info {
    int flags;
    unsigned long fh_old;
    int writepage;
    unsigned int direct_io : 1;
    unsigned int keep_cache : 1;
    unsigned int flush : 1;
    unsigned int nonseekable : 1;
    unsigned int flock_release : 1;
    unsigned int padding : 27;
    uint64_t fh;
    uint64_t lock_owner;
};

#define FUSE_CAP_ASYNC_READ (1 << 0)
#define FUSE_CAP_POSIX_LOCKS (1 << 1)
#define FUSE_CAP_ATOMIC_O_TRUNC (1 << 3)
#define FUSE_CAP_EXPORT_SUPPORT (1 << 4)
#define FUSE_CAP_BIG_WRITES (1 << 5)
#define FUSE_CAP_DONT_MASK (1 << 6)
#define FUSE_CAP_SPLICE_WRITE (1 << 7)
#define FUSE_CAP_SPLICE_MOVE (1 << 8)
#define FUSE_CAP_SPLICE_READ (1 << 9)
#define FUSE_CAP_FLOCK_LOCKS (1 << 10)
#define FUSE_CAP_IOCTL_DIR (1 << 11)

#define FUSE_IOCTL_COMPAT (1 << 0)
#define FUSE_IOCTL_UNRESTRICTED (1 << 1)
#define FUSE_IOCTL_RETRY (1 << 2)
#define FUSE_IOCTL_DIR (1 << 4)
#define FUSE_IOCTL_MAX_IOV 256

struct fuse_conn_info {
    unsigned proto_major;
    unsigned proto_minor;
    unsigned async_read;
    unsigned max_write;
    unsigned max_readahead;
    unsigned capable;
    unsigned want;
    unsigned max_background;
    unsigned congestion_threshold;
    unsigned reserved[23];
};

struct fuse_pollhandle;
struct fuse_bufvec;

__END_DECLS
