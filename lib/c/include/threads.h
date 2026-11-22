#pragma once

#include <__base.h>
#include <time.h>

__BEGIN_DECLS

/*
 * C11's threads, pthread.h's under other names: a thrd_t is a pthread_t,
 * mtx_t and cnd_t a mutex's and a condition variable's number in the
 * kernel's table, a tss_t a key.
 */
typedef pthread_t thrd_t;
typedef unsigned tss_t;
typedef int once_flag;
typedef int (*thrd_start_t)(void*);
typedef void (*tss_dtor_t)(void*);

typedef struct {
    int __handle;
} mtx_t;

typedef struct {
    int __handle;
} cnd_t;

#if !defined(__cplusplus) && (!defined(__STDC_VERSION__) || __STDC_VERSION__ < 202311L)
#define thread_local _Thread_local
#endif

#define ONCE_FLAG_INIT 0
#define TSS_DTOR_ITERATIONS 4

enum {
    thrd_success = 0,
    thrd_busy = 1,
    thrd_error = 2,
    thrd_nomem = 3,
    thrd_timedout = 4,
};

enum {
    mtx_plain = 0,
    mtx_recursive = 1,
    mtx_timed = 2,
};

int thrd_create(thrd_t*, thrd_start_t, void*);
__NORETURN void thrd_exit(int);
int thrd_join(thrd_t, int*);
int thrd_detach(thrd_t);
thrd_t thrd_current(void);
int thrd_equal(thrd_t, thrd_t);
int thrd_sleep(const struct timespec*, struct timespec*);
void thrd_yield(void);

int mtx_init(mtx_t*, int);
void mtx_destroy(mtx_t*);
int mtx_lock(mtx_t*);
int mtx_timedlock(mtx_t* __restrict, const struct timespec* __restrict);
int mtx_trylock(mtx_t*);
int mtx_unlock(mtx_t*);

void call_once(once_flag*, void (*)(void));

int cnd_init(cnd_t*);
void cnd_destroy(cnd_t*);
int cnd_broadcast(cnd_t*);
int cnd_signal(cnd_t*);
int cnd_timedwait(cnd_t* __restrict, mtx_t* __restrict, const struct timespec* __restrict);
int cnd_wait(cnd_t*, mtx_t*);

int tss_create(tss_t*, tss_dtor_t);
void tss_delete(tss_t);
int tss_set(tss_t, void*);
void* tss_get(tss_t);

__END_DECLS
