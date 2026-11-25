#pragma once

/*
 * A terminal's calls of its server's (termios.c): a call of size bytes,
 * its answer into out of room, as ctl makes it, the answer's size or -1
 * and errno; the path of a terminal's device, or of a master's slave's
 * with peer, into name of n bytes, 0 or the errno.
 */
#include <stddef.h>
#include <sys/types.h>

ssize_t __terminal_call(int fd, const void* in, size_t size, void* out, size_t room, int flags);
int __terminal_name(int fd, int peer, char* name, size_t n);
