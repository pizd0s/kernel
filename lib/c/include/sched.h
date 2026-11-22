#pragma once

#include <__base.h>

__BEGIN_DECLS

struct sched_param {
    int sched_priority;
};

#define SCHED_OTHER 0
#define SCHED_FIFO 1
#define SCHED_RR 2
#define SCHED_BATCH 3
#define SCHED_IDLE 5

int sched_yield(void);
int sched_get_priority_max(int);
int sched_get_priority_min(int);
int sched_getparam(pid_t, struct sched_param*);
int sched_setparam(pid_t, const struct sched_param*);
int sched_getscheduler(pid_t);
int sched_setscheduler(pid_t, int, const struct sched_param*);
int sched_rr_get_interval(pid_t, struct timespec*);

/*
 * A set of CPUs, a bit each. The system's CPUs are those its kernel runs
 * the process on; a thread runs on any of them.
 */
#define CPU_SETSIZE 1024

typedef struct cpu_set_t {
    unsigned long __bits[CPU_SETSIZE / (8 * sizeof(long))];
} cpu_set_t;

#define __CPU_OP_S(i, size, set, op) \
    ((size_t)(i) / 8 >= (size) ? 0 : (((unsigned long*)(set))[(size_t)(i) / 8 / sizeof(long)] op(1UL << ((size_t)(i) % (8 * sizeof(long))))))

#define CPU_SET_S(i, size, set) __CPU_OP_S(i, size, set, |=)
#define CPU_CLR_S(i, size, set) __CPU_OP_S(i, size, set, &= ~)
#define CPU_ISSET_S(i, size, set) (__CPU_OP_S(i, size, set, &) != 0)
#define CPU_ZERO_S(size, set) __builtin_memset(set, 0, size)
#define CPU_COUNT_S(size, set) __sched_cpucount(size, set)
#define CPU_EQUAL_S(size, a, b) (__builtin_memcmp(a, b, size) == 0)

#define CPU_SET(i, set) CPU_SET_S(i, sizeof(cpu_set_t), set)
#define CPU_CLR(i, set) CPU_CLR_S(i, sizeof(cpu_set_t), set)
#define CPU_ISSET(i, set) CPU_ISSET_S(i, sizeof(cpu_set_t), set)
#define CPU_ZERO(set) CPU_ZERO_S(sizeof(cpu_set_t), set)
#define CPU_COUNT(set) CPU_COUNT_S(sizeof(cpu_set_t), set)
#define CPU_EQUAL(a, b) CPU_EQUAL_S(sizeof(cpu_set_t), a, b)

#define CPU_ALLOC_SIZE(n) (sizeof(long) * (((n) + 8 * sizeof(long) - 1) / (8 * sizeof(long))))
#define CPU_ALLOC(n) ((cpu_set_t*)__builtin_calloc(1, CPU_ALLOC_SIZE(n)))
#define CPU_FREE(set) __builtin_free(set)

int __sched_cpucount(size_t, const cpu_set_t*);
int sched_getaffinity(pid_t, size_t, cpu_set_t*);
int sched_setaffinity(pid_t, size_t, const cpu_set_t*);
int sched_getcpu(void);

__END_DECLS
