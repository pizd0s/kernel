//@ root: own
//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "user accounting: ok\n"

// The user accounting database, as POSIX.1-2024 has it, over UTMPX_FILE:
// none to read before the first record; pututxline adds records at the
// end and replaces the one getutxid finds; getutxent reads them in order
// from setutxent's start; getutxid matches a time's type, or a process's
// types and ut_id; getutxline a login's or user's ut_line. A process that
// may not write the file gets no record put. musl's functions are stubs,
// and Linux keeps its database under /var, root's.

#include <assert.h>
#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>
#include <utmpx.h>

static utmpx record(short type, const char* id, const char* line, const char* user, pid_t pid) {
    utmpx u = {};
    u.ut_type = type;
    u.ut_pid = pid;
    strncpy(u.ut_id, id, sizeof u.ut_id);
    strncpy(u.ut_line, line, sizeof u.ut_line);
    strncpy(u.ut_user, user, sizeof u.ut_user);
    u.ut_tv.tv_sec = 1000 + pid;
    return u;
}

static int entries() {
    int n = 0;
    setutxent();
    while (getutxent()) {
        n++;
    }
    return n;
}

int main() {
    setutxent();
    assert(getutxent() == nullptr && entries() == 0);

    utmpx user = record(USER_PROCESS, "p1", "pts/1", "alice", 100);
    utmpx* put = pututxline(&user);
    assert(put && put != &user && strcmp(put->ut_user, "alice") == 0 && put->ut_pid == 100);
    utmpx login = record(LOGIN_PROCESS, "p2", "pts/2", "LOGIN", 200);
    assert(pututxline(&login));
    utmpx boot = record(BOOT_TIME, "", "", "", 0);
    assert(pututxline(&boot));
    struct stat st;
    assert(stat(UTMPX_FILE, &st) == 0 && st.st_size == 3 * sizeof(utmpx));

    setutxent();
    utmpx* u = getutxent();
    assert(u && u->ut_type == USER_PROCESS && strcmp(u->ut_line, "pts/1") == 0 && u->ut_tv.tv_sec == 1100);
    u = getutxent();
    assert(u && u->ut_type == LOGIN_PROCESS && u->ut_pid == 200);
    u = getutxent();
    assert(u && u->ut_type == BOOT_TIME && getutxent() == nullptr);

    setutxent();
    utmpx line = record(EMPTY, "", "pts/2", "", 0);
    u = getutxline(&line);
    assert(u && strncmp(u->ut_id, "p2", 4) == 0);
    setutxent();
    utmpx id = record(DEAD_PROCESS, "p1", "", "", 0);
    u = getutxid(&id);
    assert(u && strcmp(u->ut_user, "alice") == 0);
    u->ut_type = DEAD_PROCESS;
    assert(pututxline(u) && entries() == 3);

    setutxent();
    line = record(EMPTY, "", "pts/1", "", 0);
    assert(getutxline(&line) == nullptr);
    setutxent();
    u = getutxid(&id);
    assert(u && u->ut_type == DEAD_PROCESS && u->ut_pid == 100);
    setutxent();
    utmpx time = record(BOOT_TIME, "", "", "", 0);
    u = getutxid(&time);
    assert(u && u->ut_type == BOOT_TIME);
    setutxent();
    time = record(NEW_TIME, "", "", "", 0);
    assert(getutxid(&time) == nullptr);
    endutxent();

    pid_t child = fork();
    if (child == 0) {
        utmpx other = record(USER_PROCESS, "p9", "pts/9", "bob", 900);
        _exit(setuid(1000) == 0 && pututxline(&other) == nullptr && errno == EACCES ? 0 : 1);
    }
    int status;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0 && entries() == 3);
    printf("user accounting: ok\n");
}
