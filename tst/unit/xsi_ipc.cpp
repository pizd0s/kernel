//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "xsi ipc: ok\n"

// The XSI message queues and semaphore sets, as POSIX.1-2024 has them, and
// semtimedop. ftok makes the same key of the same file and id. A key's
// object is made once with IPC_EXCL and found after; IPC_PRIVATE makes a
// new one each time. msgrcv takes the first message, the first of a type,
// or the lowest type up to -type; E2BIG for a longer text unless
// MSG_NOERROR cuts it; ENOMSG with IPC_NOWAIT; a queue past msg_qbytes
// makes msgsnd wait. semop does all of its operations or none, waits for a
// value to allow them or EAGAIN with IPC_NOWAIT or past semtimedop's time;
// ERANGE past semaphoreMax; SEM_UNDO's adjustments come back when the process
// ends; GETNCNT counts the waiters. IPC_RMID wakes the waiters with EIDRM,
// and the identifier is EINVAL after. A signal ends a wait with EINTR.

#include <assert.h>
#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ipc.h>
#include <sys/msg.h>
#include <sys/sem.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/wait.h>
#include <time.h>
#include <unistd.h>

union semun {
    int val;
    struct semid_ds* buf;
    unsigned short* array;
};

// The limits are not POSIX's names: Linux's values, which lib/c's are.
enum { queueBytes = 16384, semaphoreMax = 32767 };

struct message {
    long mtype;
    char mtext[32];
};

static void exited(pid_t pid, int code) {
    int status;
    assert(waitpid(pid, &status, 0) == pid && WIFEXITED(status) && WEXITSTATUS(status) == code);
}

static void sent(int id, long type, const char* text) {
    message m = {type, {}};
    strcpy(m.mtext, text);
    assert(msgsnd(id, &m, strlen(text), 0) == 0);
}

static void got(int id, long type, const char* want) {
    message m = {};
    ssize_t n = msgrcv(id, &m, sizeof m.mtext, type, IPC_NOWAIT);
    assert(n == ssize_t(strlen(want)) && memcmp(m.mtext, want, n) == 0);
}

static void alarmed(int) {}

static void interruptAfter(long ms) {
    itimerval t = {{0, 0}, {0, ms * 1000}};
    assert(setitimer(ITIMER_REAL, &t, nullptr) == 0);
}

static void messageQueues() {
    int id = msgget(IPC_PRIVATE, 0600);
    assert(id >= 0 && msgget(IPC_PRIVATE, 0600) != id);
    sent(id, 1, "one");
    sent(id, 2, "two");
    sent(id, 3, "three");
    got(id, 2, "two");
    got(id, -2, "one");
    got(id, 0, "three");
    message m = {};
    assert(msgrcv(id, &m, sizeof m.mtext, 0, IPC_NOWAIT) == -1 && errno == ENOMSG);
    sent(id, 4, "toolong");
    assert(msgrcv(id, &m, 3, 4, IPC_NOWAIT) == -1 && errno == E2BIG);
    assert(msgrcv(id, &m, 3, 4, IPC_NOWAIT | MSG_NOERROR) == 3 && memcmp(m.mtext, "too", 3) == 0);
    m.mtype = 0;
    assert(msgsnd(id, &m, 1, 0) == -1 && errno == EINVAL);

    msqid_ds ds = {};
    sent(id, 5, "five");
    assert(msgctl(id, IPC_STAT, &ds) == 0 && ds.msg_qnum == 1 && ds.msg_qbytes == queueBytes && ds.msg_lspid == getpid());
    assert((ds.msg_perm.mode & 0777) == 0600 && ds.msg_perm.uid == geteuid() && ds.msg_perm.cuid == geteuid());
    ds.msg_qbytes = 8;
    assert(msgctl(id, IPC_SET, &ds) == 0);
    m.mtype = 6;
    assert(msgsnd(id, &m, 5, IPC_NOWAIT) == -1 && errno == EAGAIN);
    got(id, 5, "five");
    assert(msgctl(id, IPC_STAT, &ds) == 0 && ds.msg_qbytes == 8 && ds.msg_lrpid == getpid() && ds.msg_qnum == 0);

    pid_t child = fork();
    if (child == 0) {
        message r = {};
        _exit(msgrcv(id, &r, sizeof r.mtext, 7, 0) == 3 && memcmp(r.mtext, "dad", 3) == 0 ? 0 : 1);
    }
    usleep(20000);
    sent(id, 7, "dad");
    exited(child, 0);

    signal(SIGALRM, alarmed);
    interruptAfter(20);
    assert(msgrcv(id, &m, sizeof m.mtext, 0, 0) == -1 && errno == EINTR);

    child = fork();
    if (child == 0) {
        message r = {};
        _exit(msgrcv(id, &r, sizeof r.mtext, 0, 0) == -1 && errno == EIDRM ? 0 : 1);
    }
    usleep(20000);
    assert(msgctl(id, IPC_RMID, nullptr) == 0);
    exited(child, 0);
    assert(msgsnd(id, &m, 1, 0) == -1 && errno == EINVAL && msgctl(id, IPC_STAT, &ds) == -1 && errno == EINVAL);

    char work[] = "ipc-XXXXXX";
    assert(mkdtemp(work) == work);
    key_t key = ftok(work, 'p');
    assert(key != -1 && ftok(work, 'p') == key && ftok(work, 'q') != key && ftok("no-such-file", 'p') == -1);
    int named = msgget(key, IPC_CREAT | IPC_EXCL | 0600);
    assert(named >= 0 && msgget(key, IPC_CREAT | IPC_EXCL | 0600) == -1 && errno == EEXIST);
    assert(msgget(key, 0600) == named && msgget(key, IPC_CREAT | 0600) == named);
    assert(msgctl(named, IPC_RMID, nullptr) == 0 && msgget(key, 0600) == -1 && errno == ENOENT);
    assert(rmdir(work) == 0);
}

static int value(int id, int num) {
    return semctl(id, num, GETVAL);
}

static void semaphoreSets() {
    int id = semget(IPC_PRIVATE, 3, 0600);
    assert(id >= 0 && value(id, 0) == 0 && value(id, 2) == 0);
    assert(semget(IPC_PRIVATE, 0, 0600) == -1 && errno == EINVAL);
    semun arg;
    unsigned short all[3] = {1, 2, 3};
    arg.array = all;
    assert(semctl(id, 0, SETALL, arg) == 0 && value(id, 0) == 1 && value(id, 1) == 2 && value(id, 2) == 3);
    arg.val = 5;
    assert(semctl(id, 1, SETVAL, arg) == 0 && value(id, 1) == 5);
    arg.val = semaphoreMax + 1;
    assert(semctl(id, 1, SETVAL, arg) == -1 && errno == ERANGE);
    arg.val = 2;
    assert(semctl(id, 1, SETVAL, arg) == 0);
    assert(semctl(id, 3, GETVAL) == -1 && errno == EINVAL);

    sembuf down = {0, -1, 0};
    assert(semop(id, &down, 1) == 0 && value(id, 0) == 0 && semctl(id, 0, GETPID) == getpid());
    down.sem_flg = IPC_NOWAIT;
    assert(semop(id, &down, 1) == -1 && errno == EAGAIN);
    down.sem_flg = 0;
    timespec soon = {0, 20000000};
    assert(semtimedop(id, &down, 1, &soon) == -1 && errno == EAGAIN);
    sembuf zero = {0, 0, 0};
    assert(semop(id, &zero, 1) == 0);
    sembuf two[2] = {{1, -1, 0}, {2, -5, IPC_NOWAIT}};
    assert(semop(id, two, 2) == -1 && errno == EAGAIN && value(id, 1) == 2 && value(id, 2) == 3);
    sembuf up = {2, semaphoreMax, 0};
    assert(semop(id, &up, 1) == -1 && errno == ERANGE && value(id, 2) == 3);
    sembuf far = {3, 1, 0};
    assert(semop(id, &far, 1) == -1 && errno == EFBIG);
    unsigned short now[3] = {};
    arg.array = now;
    assert(semctl(id, 0, GETALL, arg) == 0 && now[0] == 0 && now[1] == 2 && now[2] == 3);

    pid_t child = fork();
    if (child == 0) {
        sembuf take = {0, -1, 0};
        _exit(semop(id, &take, 1) == 0 ? 0 : 1);
    }
    while (semctl(id, 0, GETNCNT) != 1) {
        usleep(1000);
    }
    sembuf give = {0, 1, 0};
    assert(semop(id, &give, 1) == 0);
    exited(child, 0);
    assert(value(id, 0) == 0 && semctl(id, 0, GETNCNT) == 0);

    child = fork();
    if (child == 0) {
        sembuf undone = {2, 4, SEM_UNDO};
        _exit(semop(id, &undone, 1) == 0 && value(id, 2) == 7 ? 0 : 1);
    }
    exited(child, 0);
    assert(value(id, 2) == 3);

    semid_ds ds = {};
    arg.buf = &ds;
    assert(semctl(id, 0, IPC_STAT, arg) == 0 && ds.sem_nsems == 3 && (ds.sem_perm.mode & 0777) == 0600 && ds.sem_perm.uid == geteuid());
    interruptAfter(20);
    assert(semop(id, &down, 1) == -1 && errno == EINTR);

    child = fork();
    if (child == 0) {
        sembuf take = {0, -1, 0};
        _exit(semop(id, &take, 1) == -1 && errno == EIDRM ? 0 : 1);
    }
    while (semctl(id, 0, GETNCNT) != 1) {
        usleep(1000);
    }
    assert(semctl(id, 0, IPC_RMID) == 0);
    exited(child, 0);
    assert(semop(id, &give, 1) == -1 && errno == EINVAL);
}

int main() {
    messageQueues();
    semaphoreSets();
    printf("xsi ipc: ok\n");
}
