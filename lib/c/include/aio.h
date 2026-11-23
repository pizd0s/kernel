#pragma once

#include <__base.h>
#include <fcntl.h>
#include <signal.h>
#include <time.h>

__BEGIN_DECLS

/*
 * Asynchronous I/O, the kernel's: an operation is the kernel's, known by
 * its aiocb's address, done by a kernel agent of the process's on its own;
 * the process takes its result, a read's bytes into aio_buf and the
 * notification, when it next looks or is interrupted. aio_reqprio is
 * ignored: there is no prioritized I/O.
 */
struct aiocb {
    int aio_fildes;
    int aio_lio_opcode;
    int aio_reqprio;
    volatile void* aio_buf;
    size_t aio_nbytes;
    struct sigevent aio_sigevent;
    off_t aio_offset;
};

#define AIO_CANCELED 0
#define AIO_NOTCANCELED 1
#define AIO_ALLDONE 2

#define LIO_READ 0
#define LIO_WRITE 1
#define LIO_NOP 2

#define LIO_WAIT 0
#define LIO_NOWAIT 1

int aio_cancel(int, struct aiocb*);
int aio_error(const struct aiocb*);
int aio_fsync(int, struct aiocb*);
int aio_read(struct aiocb*);
ssize_t aio_return(struct aiocb*);
int aio_suspend(const struct aiocb* const[], int, const struct timespec*);
int aio_write(struct aiocb*);
int lio_listio(int, struct aiocb* __restrict const* __restrict, int, struct sigevent* __restrict);

__END_DECLS
