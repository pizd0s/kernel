#pragma once

#include <__base.h>
#include <sched.h>
#include <signal.h>
#include <sys/types.h>

__BEGIN_DECLS

/*
 * posix_spawn and posix_spawnp, as POSIX.1-2024 has them: a child of
 * vfork does what the attributes and the file actions ask and runs the
 * program, or ends, and posix_spawn returns its error. The file actions
 * are a list of the heap, of which the object is the address.
 */
#define POSIX_SPAWN_RESETIDS 1
#define POSIX_SPAWN_SETPGROUP 2
#define POSIX_SPAWN_SETSIGDEF 4
#define POSIX_SPAWN_SETSIGMASK 8
#define POSIX_SPAWN_SETSCHEDPARAM 16
#define POSIX_SPAWN_SETSCHEDULER 32
#define POSIX_SPAWN_SETSID 128

typedef struct {
    int __flags;
    pid_t __pgroup;
    sigset_t __sigdefault;
    sigset_t __sigmask;
    int __policy;
    struct sched_param __param;
} posix_spawnattr_t;

typedef struct {
    int __actions;
} posix_spawn_file_actions_t;

int posix_spawn(pid_t* __restrict, const char* __restrict, const posix_spawn_file_actions_t*, const posix_spawnattr_t* __restrict,
                char* const[], char* const[]);
int posix_spawnp(pid_t* __restrict, const char* __restrict, const posix_spawn_file_actions_t*, const posix_spawnattr_t* __restrict,
                 char* const[], char* const[]);

int posix_spawn_file_actions_init(posix_spawn_file_actions_t*);
int posix_spawn_file_actions_destroy(posix_spawn_file_actions_t*);
int posix_spawn_file_actions_addopen(posix_spawn_file_actions_t* __restrict, int, const char* __restrict, int, mode_t);
int posix_spawn_file_actions_addclose(posix_spawn_file_actions_t*, int);
int posix_spawn_file_actions_adddup2(posix_spawn_file_actions_t*, int, int);
int posix_spawn_file_actions_addchdir(posix_spawn_file_actions_t* __restrict, const char* __restrict);
int posix_spawn_file_actions_addfchdir(posix_spawn_file_actions_t*, int);
int posix_spawn_file_actions_addchdir_np(posix_spawn_file_actions_t* __restrict, const char* __restrict);
int posix_spawn_file_actions_addfchdir_np(posix_spawn_file_actions_t*, int);

int posix_spawnattr_init(posix_spawnattr_t*);
int posix_spawnattr_destroy(posix_spawnattr_t*);
int posix_spawnattr_getflags(const posix_spawnattr_t* __restrict, short* __restrict);
int posix_spawnattr_setflags(posix_spawnattr_t*, short);
int posix_spawnattr_getpgroup(const posix_spawnattr_t* __restrict, pid_t* __restrict);
int posix_spawnattr_setpgroup(posix_spawnattr_t*, pid_t);
int posix_spawnattr_getsigdefault(const posix_spawnattr_t* __restrict, sigset_t* __restrict);
int posix_spawnattr_setsigdefault(posix_spawnattr_t* __restrict, const sigset_t* __restrict);
int posix_spawnattr_getsigmask(const posix_spawnattr_t* __restrict, sigset_t* __restrict);
int posix_spawnattr_setsigmask(posix_spawnattr_t* __restrict, const sigset_t* __restrict);
int posix_spawnattr_getschedpolicy(const posix_spawnattr_t* __restrict, int* __restrict);
int posix_spawnattr_setschedpolicy(posix_spawnattr_t*, int);
int posix_spawnattr_getschedparam(const posix_spawnattr_t* __restrict, struct sched_param* __restrict);
int posix_spawnattr_setschedparam(posix_spawnattr_t* __restrict, const struct sched_param* __restrict);

__END_DECLS
