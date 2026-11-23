#pragma once

#include <sys/ipc.h>

__BEGIN_DECLS

typedef unsigned long msgqnum_t;
typedef unsigned long msglen_t;

struct msqid_ds {
    struct ipc_perm msg_perm;
    time_t msg_stime;
    time_t msg_rtime;
    time_t msg_ctime;
    msgqnum_t msg_qnum;
    msglen_t msg_qbytes;
    pid_t msg_lspid;
    pid_t msg_lrpid;
    unsigned long __msg_cbytes;
};

#define MSG_NOERROR 010000

/* A message's text is at most MSGMAX bytes, a queue's at first MSGMNB. */
#define MSGMAX 8192
#define MSGMNB 16384

int msgctl(int, int, struct msqid_ds*);
int msgget(key_t, int);
ssize_t msgrcv(int, void*, size_t, long, int);
int msgsnd(int, const void*, size_t, int);

__END_DECLS
