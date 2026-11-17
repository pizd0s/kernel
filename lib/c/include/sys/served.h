#pragma once

#include <__base.h>
#include <stdint.h>

/*
 * A pair a server makes for what it serves (lib/protocol/protocol.h):
 * the first end is the server's, a plain one, which the requests of the
 * second come on; the second is born the open file struct served
 * describes, whose calls the kernel makes requests on the first, and goes
 * to who asked, as an OPEN's OK brings it. kind is SERVED_FILE, an open
 * file; SERVED_TERMINAL, a terminal's, which takes a terminal's calls
 * too; SERVED_DIR, a handle of a directory of the VFS's, which calls with
 * a path go to; or SERVED_PATH, a handle of what O_PATH opened that is no
 * directory; with SERVED_TELLS the server tells its readiness (READY).
 * flags are the open's: O_ACCMODE, O_APPEND and O_NONBLOCK, and O_PATH
 * of a directory's handle, which takes no locks then. dev and ino
 * are the file's, as its stat has them, or a terminal's device's: the
 * kernel keeps the locks of flock and fcntl by them and by the process
 * that made the pair, so the open files one process gave one dev and ino
 * are one file to lock, and no other process's are that file; a file of
 * ino 0 has no locks. id is a handle's, which calls with a path name the
 * root by. Both ends close on exec.
 */
#define SERVED_FILE 1
#define SERVED_TERMINAL 2
#define SERVED_DIR 3
#define SERVED_PATH 4
#define SERVED_TELLS 16

__BEGIN_DECLS

struct served {
    uint32_t kind;
    uint32_t flags;
    uint64_t dev;
    uint64_t ino;
    uint64_t id;
};

int servedpair(const struct served*, int[2]);

__END_DECLS
