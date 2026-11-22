//@ compile-flags: -g
//@ stdout: "child: prepared, child; cleanup 2 1; destructor 7\nparent: prepared, parent; exit 0\ncancelled: cleanup\ncancel: exit 0\nnamed: posted by the child\nthreads: ok\n"

// What a process of one thread has of pthread.h, threads.h and
// semaphore.h, the kernel's objects in the system's: mutexes of each
// type, condition variables, read-write locks, barriers, spin locks,
// once, keys and their destructors, cleanup handlers, fork's handlers,
// a thread's cancellation of itself, attributes, a thread's name and its
// CPUs, unnamed and named semaphores, a fork with mutexes held, and C11's
// names of them. Nothing waits for a thread that is not there: a wait
// with a deadline ends at it.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <pthread.h>
#include <sched.h>
#include <semaphore.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <threads.h>
#include <time.h>
#include <unistd.h>

namespace {
    // A time of a clock a little after now, or before it.
    timespec at(clockid_t clock, long ms) {
        timespec t;
        assert(clock_gettime(clock, &t) == 0);
        t.tv_nsec += ms * 1000000;
        while (t.tv_nsec >= 1000000000) {
            t.tv_nsec -= 1000000000;
            t.tv_sec++;
        }
        while (t.tv_nsec < 0) {
            t.tv_nsec += 1000000000;
            t.tv_sec--;
        }
        return t;
    }

    pthread_mutex_t staticMutex = PTHREAD_MUTEX_INITIALIZER;
    pthread_cond_t staticCond = PTHREAD_COND_INITIALIZER;
    pthread_rwlock_t staticLock = PTHREAD_RWLOCK_INITIALIZER;
    pthread_once_t once = PTHREAD_ONCE_INIT;
    once_flag flag = ONCE_FLAG_INIT;
    int onceRuns;
    char said[256];

    void say(const char* what) {
        strcat(said, what);
    }

    void initOnce() {
        onceRuns++;
    }

    void mutexes() {
        assert(pthread_mutex_lock(&staticMutex) == 0);
        assert(pthread_mutex_trylock(&staticMutex) == EBUSY);
        timespec soon = at(CLOCK_REALTIME, 20);
        assert(pthread_mutex_timedlock(&staticMutex, &soon) == ETIMEDOUT);
        assert(pthread_mutex_unlock(&staticMutex) == 0);

        pthread_mutexattr_t attr;
        assert(pthread_mutexattr_init(&attr) == 0);
        int value = -1;
        assert(pthread_mutexattr_gettype(&attr, &value) == 0 && value == PTHREAD_MUTEX_DEFAULT);
        assert(pthread_mutexattr_settype(&attr, 3) == EINVAL);
        assert(pthread_mutexattr_setprotocol(&attr, PTHREAD_PRIO_PROTECT) == ENOTSUP);
        assert(pthread_mutexattr_setprotocol(&attr, 7) == EINVAL);
        assert(pthread_mutexattr_setrobust(&attr, 2) == EINVAL);
        assert(pthread_mutexattr_setpshared(&attr, 2) == EINVAL);
        assert(pthread_mutexattr_setpshared(&attr, PTHREAD_PROCESS_PRIVATE) == 0);

        assert(pthread_mutexattr_settype(&attr, PTHREAD_MUTEX_ERRORCHECK) == 0);
        pthread_mutex_t checked;
        assert(pthread_mutex_init(&checked, &attr) == 0);
        assert(pthread_mutex_unlock(&checked) == EPERM);
        assert(pthread_mutex_lock(&checked) == 0);
        assert(pthread_mutex_lock(&checked) == EDEADLK);
        assert(pthread_mutex_trylock(&checked) == EBUSY);
        soon = at(CLOCK_REALTIME, 20);
        assert(pthread_mutex_timedlock(&checked, &soon) == EDEADLK);
        assert(pthread_mutex_unlock(&checked) == 0);
        assert(pthread_mutex_unlock(&checked) == EPERM);
        assert(pthread_mutex_destroy(&checked) == 0);

        assert(pthread_mutexattr_settype(&attr, PTHREAD_MUTEX_RECURSIVE) == 0);
        pthread_mutex_t recursive;
        assert(pthread_mutex_init(&recursive, &attr) == 0);
        assert(pthread_mutex_lock(&recursive) == 0 && pthread_mutex_lock(&recursive) == 0 && pthread_mutex_trylock(&recursive) == 0);
        assert(pthread_mutex_unlock(&recursive) == 0 && pthread_mutex_unlock(&recursive) == 0 && pthread_mutex_unlock(&recursive) == 0);
        assert(pthread_mutex_unlock(&recursive) == EPERM);
        assert(pthread_mutex_consistent(&recursive) == EINVAL);
        int ceiling;
        assert(pthread_mutex_getprioceiling(&recursive, &ceiling) == EINVAL);
        assert(pthread_mutex_destroy(&recursive) == 0);
        assert(pthread_mutexattr_destroy(&attr) == 0);
    }

    void conditions() {
        pthread_condattr_t attr;
        assert(pthread_condattr_init(&attr) == 0);
        clockid_t clock = -1;
        assert(pthread_condattr_getclock(&attr, &clock) == 0 && clock == CLOCK_REALTIME);
        assert(pthread_condattr_setclock(&attr, CLOCK_PROCESS_CPUTIME_ID) == EINVAL);
        assert(pthread_condattr_setclock(&attr, CLOCK_MONOTONIC) == 0);
        assert(pthread_condattr_setpshared(&attr, 2) == EINVAL);
        pthread_cond_t cond;
        assert(pthread_cond_init(&cond, &attr) == 0);
        assert(pthread_cond_signal(&cond) == 0 && pthread_cond_broadcast(&cond) == 0);

        pthread_mutex_t mutex;
        pthread_mutexattr_t checked;
        assert(pthread_mutexattr_init(&checked) == 0 && pthread_mutexattr_settype(&checked, PTHREAD_MUTEX_ERRORCHECK) == 0);
        assert(pthread_mutex_init(&mutex, &checked) == 0);
        assert(pthread_mutex_lock(&mutex) == 0);
        timespec soon = at(CLOCK_MONOTONIC, 20);
        assert(pthread_cond_timedwait(&cond, &mutex, &soon) == ETIMEDOUT);
        assert(pthread_mutex_lock(&mutex) == EDEADLK);
        assert(pthread_mutex_unlock(&mutex) == 0);
        assert(pthread_cond_destroy(&cond) == 0 && pthread_condattr_destroy(&attr) == 0);

        assert(pthread_mutex_lock(&staticMutex) == 0);
        soon = at(CLOCK_REALTIME, -1000);
        assert(pthread_cond_timedwait(&staticCond, &staticMutex, &soon) == ETIMEDOUT);
        assert(pthread_mutex_unlock(&staticMutex) == 0);
    }

    void locks() {
        assert(pthread_rwlock_rdlock(&staticLock) == 0 && pthread_rwlock_tryrdlock(&staticLock) == 0);
        assert(pthread_rwlock_trywrlock(&staticLock) == EBUSY);
        timespec soon = at(CLOCK_REALTIME, 20);
        assert(pthread_rwlock_timedwrlock(&staticLock, &soon) == ETIMEDOUT);
        assert(pthread_rwlock_unlock(&staticLock) == 0 && pthread_rwlock_unlock(&staticLock) == 0);
        assert(pthread_rwlock_wrlock(&staticLock) == 0);
        assert(pthread_rwlock_tryrdlock(&staticLock) == EBUSY);
        soon = at(CLOCK_REALTIME, 20);
        assert(pthread_rwlock_timedrdlock(&staticLock, &soon) == ETIMEDOUT);
        assert(pthread_rwlock_unlock(&staticLock) == 0);

        pthread_rwlockattr_t attr;
        int shared = -1;
        assert(pthread_rwlockattr_init(&attr) == 0 && pthread_rwlockattr_getpshared(&attr, &shared) == 0 && shared == 0);
        assert(pthread_rwlockattr_setpshared(&attr, 2) == EINVAL);
        pthread_rwlock_t lock;
        assert(pthread_rwlock_init(&lock, &attr) == 0 && pthread_rwlock_destroy(&lock) == 0);

        pthread_barrier_t barrier;
        assert(pthread_barrier_init(&barrier, nullptr, 0) == EINVAL);
        assert(pthread_barrier_init(&barrier, nullptr, 1) == 0);
        assert(pthread_barrier_wait(&barrier) == PTHREAD_BARRIER_SERIAL_THREAD);
        assert(pthread_barrier_destroy(&barrier) == 0);

        pthread_spinlock_t spin;
        assert(pthread_spin_init(&spin, PTHREAD_PROCESS_PRIVATE) == 0);
        assert(pthread_spin_lock(&spin) == 0 && pthread_spin_trylock(&spin) == EBUSY);
        assert(pthread_spin_unlock(&spin) == 0 && pthread_spin_trylock(&spin) == 0 && pthread_spin_unlock(&spin) == 0);
        assert(pthread_spin_destroy(&spin) == 0);
    }

    void attributes() {
        pthread_attr_t attr;
        assert(pthread_attr_init(&attr) == 0);
        size_t size = 0;
        int value = -1;
        assert(pthread_attr_getstacksize(&attr, &size) == 0 && size >= PTHREAD_STACK_MIN);
        assert(pthread_attr_setstacksize(&attr, PTHREAD_STACK_MIN - 1) == EINVAL);
        assert(pthread_attr_setstacksize(&attr, 65536) == 0 && pthread_attr_getstacksize(&attr, &size) == 0 && size == 65536);
        assert(pthread_attr_setguardsize(&attr, 4096) == 0 && pthread_attr_getguardsize(&attr, &size) == 0 && size == 4096);
        assert(pthread_attr_getdetachstate(&attr, &value) == 0 && value == PTHREAD_CREATE_JOINABLE);
        assert(pthread_attr_setdetachstate(&attr, 2) == EINVAL);
        assert(pthread_attr_setdetachstate(&attr, PTHREAD_CREATE_DETACHED) == 0);
        assert(pthread_attr_setscope(&attr, PTHREAD_SCOPE_PROCESS) == ENOTSUP);
        assert(pthread_attr_getscope(&attr, &value) == 0 && value == PTHREAD_SCOPE_SYSTEM);
        assert(pthread_attr_setinheritsched(&attr, 2) == EINVAL);
        void* stack = nullptr;
        assert(pthread_attr_getstack(&attr, &stack, &size) == EINVAL);
        static char room[65536];
        assert(pthread_attr_setstack(&attr, room, sizeof room) == 0);
        assert(pthread_attr_getstack(&attr, &stack, &size) == 0 && stack == room && size == sizeof room);
        sched_param param{};
        assert(pthread_attr_getschedparam(&attr, &param) == 0 && param.sched_priority == 0);
        assert(pthread_attr_destroy(&attr) == 0);

        assert(pthread_getattr_np(pthread_self(), &attr) == 0);
        assert(pthread_attr_getdetachstate(&attr, &value) == 0 && value == PTHREAD_CREATE_JOINABLE);
        assert(pthread_attr_destroy(&attr) == 0);
    }

    void self() {
        const pthread_t me = pthread_self();
        assert(pthread_equal(me, pthread_self()) && gettid() == getpid());
        assert(pthread_kill(me, 0) == 0);
        int policy = -1;
        sched_param param{};
        assert(pthread_getschedparam(me, &policy, &param) == 0 && policy == SCHED_OTHER && param.sched_priority == 0);
        assert(pthread_setschedprio(me, 0) == 0);
        assert(pthread_getconcurrency() == 0 && pthread_setconcurrency(-1) == EINVAL && pthread_setconcurrency(0) == 0);
        clockid_t clock;
        timespec spent;
        assert(pthread_getcpuclockid(me, &clock) == 0 && clock != CLOCK_THREAD_CPUTIME_ID && clock_gettime(clock, &spent) == 0);
        assert(clock_getres(clock, &spent) == 0);
        assert(clock_getcpuclockid(0, &clock) == 0 && clock_gettime(clock, &spent) == 0);
        assert(clock_getcpuclockid(0x7ffffff0, &clock) == ESRCH);
        char name[16];
        assert(pthread_setname_np(me, "a name too long to be one") == ERANGE);
        assert(pthread_setname_np(me, "worker") == 0);
        assert(pthread_getname_np(me, name, 4) == ERANGE && pthread_getname_np(me, name, sizeof name) == 0 && strcmp(name, "worker") == 0);
        cpu_set_t cpus;
        CPU_ZERO(&cpus);
        assert(pthread_getaffinity_np(me, sizeof cpus, &cpus) == 0 && CPU_COUNT(&cpus) >= 1);
        assert(sched_getaffinity(0, sizeof cpus, &cpus) == 0 && CPU_COUNT(&cpus) >= 1);
        assert(pthread_setaffinity_np(me, sizeof cpus, &cpus) == 0);
        int old = -1;
        assert(pthread_setcancelstate(3, &old) == EINVAL);
        assert(pthread_setcancelstate(PTHREAD_CANCEL_DISABLE, &old) == 0 && old == PTHREAD_CANCEL_ENABLE);
        assert(pthread_setcancelstate(PTHREAD_CANCEL_ENABLE, &old) == 0 && old == PTHREAD_CANCEL_DISABLE);
        assert(pthread_setcanceltype(PTHREAD_CANCEL_DEFERRED, &old) == 0 && old == PTHREAD_CANCEL_DEFERRED);
        pthread_testcancel();
        assert(pthread_detach(me) == 0);

        assert(pthread_once(&once, initOnce) == 0 && pthread_once(&once, initOnce) == 0);
        call_once(&flag, initOnce);
        call_once(&flag, initOnce);
        assert(onceRuns == 2);
    }

    void c11() {
        mtx_t mutex;
        assert(mtx_init(&mutex, mtx_plain) == thrd_success);
        assert(mtx_lock(&mutex) == thrd_success && mtx_trylock(&mutex) == thrd_busy);
        timespec soon = at(CLOCK_REALTIME, 20);
        assert(mtx_timedlock(&mutex, &soon) == thrd_timedout);
        cnd_t cond;
        assert(cnd_init(&cond) == thrd_success && cnd_signal(&cond) == thrd_success && cnd_broadcast(&cond) == thrd_success);
        soon = at(CLOCK_REALTIME, 20);
        assert(cnd_timedwait(&cond, &mutex, &soon) == thrd_timedout);
        assert(mtx_unlock(&mutex) == thrd_success);
        cnd_destroy(&cond);
        mtx_destroy(&mutex);
        assert(mtx_init(&mutex, mtx_plain | mtx_recursive) == thrd_success);
        assert(mtx_lock(&mutex) == thrd_success && mtx_lock(&mutex) == thrd_success);
        assert(mtx_unlock(&mutex) == thrd_success && mtx_unlock(&mutex) == thrd_success);
        mtx_destroy(&mutex);
        tss_t key;
        int value = 5;
        assert(tss_create(&key, nullptr) == thrd_success && tss_get(key) == nullptr);
        assert(tss_set(key, &value) == thrd_success && tss_get(key) == &value);
        tss_delete(key);
        assert(thrd_equal(thrd_current(), thrd_current()));
        timespec nap = {0, 1000000};
        assert(thrd_sleep(&nap, nullptr) == 0);
        thrd_yield();
    }

    void semaphores() {
        fflush(stdout);
        sem_t sem;
        assert(sem_init(&sem, 0, (unsigned)SEM_VALUE_MAX + 1) == -1 && errno == EINVAL);
        assert(sem_init(&sem, 0, 2) == 0);
        assert(sem_trywait(&sem) == 0 && sem_wait(&sem) == 0);
        assert(sem_trywait(&sem) == -1 && errno == EAGAIN);
        timespec soon = at(CLOCK_REALTIME, 20);
        assert(sem_timedwait(&sem, &soon) == -1 && errno == ETIMEDOUT);
        int value = -1;
        assert(sem_post(&sem) == 0 && sem_getvalue(&sem, &value) == 0 && value == 1);
        assert(sem_destroy(&sem) == 0);

        char name[64];
        snprintf(name, sizeof name, "/pizdos-threads-%d", (int)getpid());
        sem_unlink(name);
        assert(sem_open(name, 0) == SEM_FAILED && errno == ENOENT);
        sem_t* named = sem_open(name, O_CREAT | O_EXCL, 0600, 0);
        assert(named != SEM_FAILED);
        assert(sem_open(name, O_CREAT | O_EXCL, 0600, 0) == SEM_FAILED && errno == EEXIST);
        sem_t* again = sem_open(name, 0);
        assert(again == named);
        const pid_t child = fork();
        if (child == 0) {
            assert(sem_post(named) == 0);
            _exit(0);
        }
        int status;
        assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
        assert(sem_trywait(named) == 0);
        printf("named: posted by the child\n");
        assert(sem_close(again) == 0 && sem_close(named) == 0);
        assert(sem_unlink(name) == 0 && sem_unlink(name) == -1 && errno == ENOENT);
    }

    void prepare() {
        say("prepared, ");
    }

    void parent() {
        say("parent");
    }

    void child() {
        say("child");
    }

    void destroy(void* value) {
        char line[32];
        snprintf(line, sizeof line, "; destructor %d", *static_cast<int*>(value));
        say(line);
    }

    void cleanup(void* what) {
        say(static_cast<const char*>(what));
    }

    // A fork's handlers around it, and in the child mutexes the parent
    // held, its keys' destructors and its cleanup handlers as it ends by
    // pthread_exit.
    void forks() {
        pthread_mutexattr_t attr;
        pthread_mutex_t checked, normal;
        assert(pthread_mutexattr_init(&attr) == 0 && pthread_mutexattr_settype(&attr, PTHREAD_MUTEX_ERRORCHECK) == 0);
        assert(pthread_mutex_init(&checked, &attr) == 0 && pthread_mutex_init(&normal, nullptr) == 0);
        assert(pthread_mutex_lock(&checked) == 0 && pthread_mutex_lock(&normal) == 0);
        pthread_key_t key;
        static int seven = 7;
        assert(pthread_key_create(&key, destroy) == 0 && pthread_setspecific(key, &seven) == 0);
        assert(pthread_atfork(prepare, parent, child) == 0);
        const pid_t pid = fork();
        if (pid == 0) {
            assert(pthread_mutex_trylock(&checked) == EBUSY && pthread_mutex_unlock(&checked) == EPERM);
            assert(pthread_mutex_unlock(&normal) == 0);
            assert(pthread_getspecific(key) == &seven);
            pthread_cleanup_push(cleanup, (void*)" 1");
            pthread_cleanup_push(cleanup, (void*)"; cleanup 2");
            fputs("child: ", stdout);
            fputs(said, stdout);
            said[0] = 0;
            pthread_exit(nullptr);
            pthread_cleanup_pop(0);
            pthread_cleanup_pop(0);
        }
        int status;
        assert(waitpid(pid, &status, 0) == pid);
        printf("parent: %s; exit %d\n", said, WIFEXITED(status) ? WEXITSTATUS(status) : -1);
        assert(pthread_mutex_unlock(&checked) == 0 && pthread_mutex_unlock(&normal) == 0);
        assert(pthread_key_delete(key) == 0);
    }

    // A thread that cancels itself, the cancellation acting at the next
    // point that takes it, its cleanup handlers run.
    void cancels() {
        fflush(stdout);
        const pid_t pid = fork();
        if (pid == 0) {
            said[0] = 0;
            fputs("cancelled: ", stdout);
            pthread_cleanup_push(cleanup, (void*)"cleanup");
            assert(pthread_cancel(pthread_self()) == 0);
            pthread_testcancel();
            pthread_cleanup_pop(0);
            _exit(3);
        }
        int status;
        assert(waitpid(pid, &status, 0) == pid);
        printf("cancel: exit %d\n", WIFEXITED(status) ? WEXITSTATUS(status) : -1);
    }
}

// The child's lines are written as it ends: pthread_exit's exit flushes
// stdout after its destructors and cleanup handlers said theirs.
static void childLine() {
    if (said[0]) {
        fputs(said, stdout);
        fputs("\n", stdout);
    }
}

int main() {
    setvbuf(stdout, nullptr, _IOFBF, 4096);
    mutexes();
    conditions();
    locks();
    attributes();
    self();
    c11();
    atexit(childLine);
    fflush(stdout);
    forks();
    said[0] = 0;
    cancels();
    semaphores();
    puts("threads: ok");
    said[0] = 0;
    return 0;
}
