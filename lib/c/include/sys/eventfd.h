#pragma once

#include <__base.h>
#include <fcntl.h>
#include <stdint.h>

__BEGIN_DECLS

/*
 * eventfd, Linux's: a descriptor of a 64-bit counter, the kernel's. A
 * read of 8 bytes gives the counter and makes it 0, or with EFD_SEMAPHORE
 * gives 1 and takes 1 from it, and waits while it is 0; a write of
 * exactly 8 bytes adds to it and waits while the sum would pass
 * 2^64 - 2, and 2^64 - 1 is EINVAL. poll finds it readable when it is not 0, writable
 * while 1 more fits. eventfd_read and eventfd_write are a read and a
 * write of the 8 bytes, 0 or -1.
 */
typedef uint64_t eventfd_t;

#define EFD_SEMAPHORE 1
#define EFD_CLOEXEC O_CLOEXEC
#define EFD_NONBLOCK O_NONBLOCK

int eventfd(unsigned int, int);
int eventfd_read(int, eventfd_t*);
int eventfd_write(int, eventfd_t);

__END_DECLS
