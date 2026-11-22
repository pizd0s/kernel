#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * watchdir gives a descriptor of the changes of a directory's names:
 * dirfd an open directory, flags O_CLOEXEC and O_NONBLOCK of the new
 * descriptor. A read of it gives records, as many as fit: a kind, one
 * byte, then the name and its NUL. WATCH_EXISTING is a name there as the
 * watch starts, each once, then WATCH_IDLE, with no name, once; after it
 * WATCH_ADDED and WATCH_REMOVED, of what changed since the read before:
 * a name that came and went between two reads is not told, one that
 * went and came back is another, removed and added. A read that finds
 * nothing to tell waits, or fails EAGAIN with O_NONBLOCK, and poll finds
 * the descriptor readable when there is something; a read of a watch of
 * a directory that is gone is 0. The VFS watches it, rootfs, which sees
 * every change of the names, every file system's.
 */

#define WATCH_EXISTING 1
#define WATCH_IDLE 2
#define WATCH_ADDED 3
#define WATCH_REMOVED 4

int watchdir(int dirfd, int flags);

__END_DECLS
