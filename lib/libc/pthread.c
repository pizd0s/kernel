#include <errno.h>
#include <limits.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <threads.h>
#include <unistd.h>

/*
 * What of threads calls the program's functions, which the kernel's Go
 * cannot: a once's routine, the cleanup handlers and the destructors of
 * keys of a thread that ends, and fork's handlers. Everything else of
 * pthread.h and threads.h is the kernel's Go, its state too: a once is its
 * object, which says whether its routine is to run.
 */
int __pthread_once_begin(pthread_once_t*);
void __pthread_once_end(pthread_once_t*);
int __pthread_cancelled(int);
void* __pthread_key_take(pthread_key_t*, void (**)(void*));

// The thread's cleanup handlers, the last pushed first, each in the frame
// that pushed it, which comes with its size, the one layout it has had.
static struct __ptcb* cleanups;

void __kernel__pthread_cleanup_push(struct __ptcb* cb, size_t size, void (*f)(void*), void* x) {
    cb->__f = f;
    cb->__x = x;
    cb->__next = cleanups;
    cleanups = cb;
}

void __kernel__pthread_cleanup_pop(struct __ptcb* cb, size_t size, int run) {
    cleanups = cb->__next;

    if (run) {
        cb->__f(cb->__x);
    }
}

int __kernel_pthread_once(pthread_once_t* once, void (*init)(void)) {
    if (__pthread_once_begin(once)) {
        init();
        __pthread_once_end(once);
    }

    return 0;
}

void __kernel_call_once(once_flag* flag, void (*fn)(void)) {
    pthread_once(flag, fn);
}

/*
 * A thread's end: its cleanup handlers, then the destructors of the
 * values its keys hold, each value cleared before its destructor runs,
 * in rounds while one leaves a value again; the last thread's end is the
 * process's exit, with status 0.
 */
void __kernel_pthread_exit(void* result) {
    while (cleanups) {
        struct __ptcb* cb = cleanups;
        cleanups = cb->__next;
        cb->__f(cb->__x);
    }

    for (int round = 0; round < PTHREAD_DESTRUCTOR_ITERATIONS; round++) {
        int called = 0;
        pthread_key_t key = 0;
        void (*destructor)(void*);

        for (void* value; (value = __pthread_key_take(&key, &destructor)); called = 1) {
            destructor(value);
        }

        if (!called) {
            break;
        }
    }

    exit(0);
}

void __kernel_thrd_exit(int result) {
    pthread_exit((void*)(intptr_t)result);
}

void __kernel_pthread_testcancel(void) {
    if (__pthread_cancelled(0)) {
        pthread_exit(PTHREAD_CANCELED);
    }
}

/*
 * fork's handlers, in the order registered: the prepare handlers run
 * last registered first, before the kernel's fork, _Fork; the parent's,
 * even where it failed, and the child's first registered first, after it.
 */
typedef struct {
    void (*prepare)(void);
    void (*parent)(void);
    void (*child)(void);
} ForkHandlers;

static ForkHandlers* forks;
static int forkCount;
static int forkRoom;

int __kernel_pthread_atfork(void (*prepare)(void), void (*parent)(void), void (*child)(void)) {
    if (forkCount == forkRoom) {
        int grown = forkRoom ? 2 * forkRoom : 8;
        ForkHandlers* moved = realloc(forks, grown * sizeof *forks);

        if (!moved) {
            return ENOMEM;
        }

        forks = moved;
        forkRoom = grown;
    }

    forks[forkCount++] = (ForkHandlers){prepare, parent, child};

    return 0;
}

pid_t __kernel_fork(void) {
    for (int i = forkCount - 1; i >= 0; i--) {
        if (forks[i].prepare) {
            forks[i].prepare();
        }
    }

    pid_t pid = _Fork();

    for (int i = 0; i < forkCount; i++) {
        void (*handler)(void) = pid == 0 ? forks[i].child : forks[i].parent;

        if (handler) {
            handler();
        }
    }

    return pid;
}
