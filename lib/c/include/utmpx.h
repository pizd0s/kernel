#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * The user accounting database, the kernel's functions over UTMPX_FILE:
 * a file of struct utmpx records, read from a position of the process's;
 * pututxline replaces the record getutxid would find, from the last one
 * read on and then from the start, or adds one at the end, as the file's
 * permissions let it.
 */
#define UTMPX_FILE "/run/utmp"

#define EMPTY 0
#define RUN_LVL 1
#define BOOT_TIME 2
#define NEW_TIME 3
#define OLD_TIME 4
#define INIT_PROCESS 5
#define LOGIN_PROCESS 6
#define USER_PROCESS 7
#define DEAD_PROCESS 8

struct utmpx {
    short ut_type;
    pid_t ut_pid;
    char ut_line[32];
    char ut_id[4];
    char ut_user[32];
    char ut_host[256];
    struct {
        short e_termination;
        short e_exit;
    } ut_exit;
    long ut_session;
    struct timeval ut_tv;
    int ut_addr_v6[4];
    char __unused[20];
};

void endutxent(void);
struct utmpx* getutxent(void);
struct utmpx* getutxid(const struct utmpx*);
struct utmpx* getutxline(const struct utmpx*);
struct utmpx* pututxline(const struct utmpx*);
void setutxent(void);

__END_DECLS
