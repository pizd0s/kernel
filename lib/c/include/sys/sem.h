#pragma once

#include <sys/ipc.h>

__BEGIN_DECLS

#define SEM_UNDO 0x1000

#define GETPID 11
#define GETVAL 12
#define GETALL 13
#define GETNCNT 14
#define GETZCNT 15
#define SETVAL 16
#define SETALL 17

/* A set has at most SEMMSL semaphores, a value at most SEMVMX, a semop SEMOPM operations. */
#define SEMMSL 32000
#define SEMVMX 32767
#define SEMOPM 500

struct semid_ds {
    struct ipc_perm sem_perm;
    time_t sem_otime;
    time_t sem_ctime;
    unsigned short sem_nsems;
};

struct sembuf {
    unsigned short sem_num;
    short sem_op;
    short sem_flg;
};

/*
 * semctl's fourth argument, for SETVAL, GETALL, SETALL, IPC_STAT and
 * IPC_SET, is the program's union semun, as POSIX has it; wasm32 passes a
 * union by its address, which the kernel reads it at.
 */
int semctl(int, int, int, ...);
int semget(key_t, int, int);
int semop(int, struct sembuf*, size_t);
int semtimedop(int, struct sembuf*, size_t, const struct timespec*);

__END_DECLS
