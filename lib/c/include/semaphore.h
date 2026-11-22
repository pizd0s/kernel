#pragma once

#include <__base.h>
#include <fcntl.h>
#include <time.h>

__BEGIN_DECLS

/*
 * A semaphore is the kernel's: a sem_t holds its number in the process's
 * table of them, as a mutex's structure does. A named one, sem_open's, is
 * the system's, every process's that opens its name, and its sem_t is
 * the kernel's memory in the process, the same for each open of the name.
 */
typedef struct {
    int __handle;
} sem_t;

#define SEM_FAILED ((sem_t*)0)

int sem_init(sem_t*, int, unsigned);
int sem_destroy(sem_t*);
int sem_wait(sem_t*);
int sem_trywait(sem_t*);
int sem_timedwait(sem_t* __restrict, const struct timespec* __restrict);
int sem_post(sem_t*);
int sem_getvalue(sem_t* __restrict, int* __restrict);
sem_t* sem_open(const char*, int, ...);
int sem_close(sem_t*);
int sem_unlink(const char*);

__END_DECLS
