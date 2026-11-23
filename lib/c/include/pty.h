#pragma once

#include <termios.h>
#include <sys/ioctl.h>

__BEGIN_DECLS

int openpty(int*, int*, char*, const struct termios*, const struct winsize*);

// forkpty: a child of fork with openpty's terminal as its controlling one
// and its standard input, output and error, the leader of a session of its
// own (lib/libc's pty.c).
pid_t forkpty(int*, char*, const struct termios*, const struct winsize*);

__END_DECLS
