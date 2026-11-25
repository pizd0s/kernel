#pragma once

#include <__base.h>

__BEGIN_DECLS

#define STDIN_FILENO 0
#define STDOUT_FILENO 1
#define STDERR_FILENO 2

#define F_OK 0
#define X_OK 1
#define W_OK 2
#define R_OK 4

#ifndef SEEK_SET
#define SEEK_SET 0
#define SEEK_CUR 1
#define SEEK_END 2
#endif
#define SEEK_DATA 3
#define SEEK_HOLE 4

#define F_ULOCK 0
#define F_LOCK 1
#define F_TLOCK 2
#define F_TEST 3

#define _POSIX_VERSION 200809L
#define _POSIX2_VERSION _POSIX_VERSION
#define _XOPEN_VERSION 700
#define _POSIX_VDISABLE 0
#define _POSIX_JOB_CONTROL 1
#define _POSIX_SAVED_IDS 1
#define _POSIX_MONOTONIC_CLOCK _POSIX_VERSION
#define _POSIX_TIMERS _POSIX_VERSION
#define _POSIX_THREADS _POSIX_VERSION
#define _POSIX_THREAD_SAFE_FUNCTIONS _POSIX_VERSION
#define _POSIX_THREAD_ATTR_STACKADDR _POSIX_VERSION
#define _POSIX_THREAD_ATTR_STACKSIZE _POSIX_VERSION
#define _POSIX_THREAD_CPUTIME _POSIX_VERSION
#define _POSIX_SEMAPHORES _POSIX_VERSION
#define _POSIX_BARRIERS _POSIX_VERSION
#define _POSIX_SPIN_LOCKS _POSIX_VERSION
#define _POSIX_READER_WRITER_LOCKS _POSIX_VERSION
#define _POSIX_TIMEOUTS _POSIX_VERSION
#define _POSIX_MESSAGE_PASSING _POSIX_VERSION
#define _POSIX_ASYNCHRONOUS_IO _POSIX_VERSION
#define _POSIX_ADVISORY_INFO _POSIX_VERSION
#define _POSIX_SPAWN _POSIX_VERSION

#define _PC_LINK_MAX 0
#define _PC_MAX_CANON 1
#define _PC_MAX_INPUT 2
#define _PC_NAME_MAX 3
#define _PC_PATH_MAX 4
#define _PC_PIPE_BUF 5
#define _PC_CHOWN_RESTRICTED 6
#define _PC_NO_TRUNC 7
#define _PC_VDISABLE 8

#define _SC_ARG_MAX 0
#define _SC_CHILD_MAX 1
#define _SC_CLK_TCK 2
#define _SC_NGROUPS_MAX 3
#define _SC_OPEN_MAX 4
#define _SC_STREAM_MAX 5
#define _SC_TZNAME_MAX 6
#define _SC_JOB_CONTROL 7
#define _SC_SAVED_IDS 8
#define _SC_VERSION 29
#define _SC_PAGESIZE 30
#define _SC_PAGE_SIZE 30
#define _SC_LINE_MAX 43
#define _SC_IOV_MAX 60
#define _SC_GETGR_R_SIZE_MAX 69
#define _SC_GETPW_R_SIZE_MAX 70
#define _SC_LOGIN_NAME_MAX 71
#define _SC_TTY_NAME_MAX 72
#define _SC_NPROCESSORS_CONF 83
#define _SC_NPROCESSORS_ONLN 84
#define _SC_PHYS_PAGES 85
#define _SC_AVPHYS_PAGES 86
#define _SC_MONOTONIC_CLOCK 149
#define _SC_SYMLOOP_MAX 173
#define _SC_HOST_NAME_MAX 180

#define _CS_PATH 0

extern char** environ;
extern char* optarg;
extern int optind;
extern int opterr;
extern int optopt;

int getopt(int, char* const[], const char*);

ssize_t read(int, void*, size_t);
ssize_t write(int, const void*, size_t);
ssize_t pread(int, void*, size_t, off_t);
ssize_t pwrite(int, const void*, size_t, off_t);
ssize_t copy_file_range(int, off_t*, int, off_t*, size_t, unsigned);
off_t lseek(int, off_t, int);
int close(int);
#define POSIX_CLOSE_RESTART 0
int posix_close(int, int);
int issetugid(void);
void encrypt(char*, int);
int dup(int);
int dup2(int, int);
int dup3(int, int, int);
int pipe(int[2]);
int pipe2(int[2], int);
int fsync(int);
int fdatasync(int);
void sync(void);
int syncfs(int);
int ftruncate(int, off_t);
int truncate(const char*, off_t);
int lockf(int, int, off_t);

// fork runs pthread_atfork's handlers around _Fork, the kernel's fork
// without them (lib/libc's pthread.c).
pid_t fork(void);
pid_t _Fork(void);
pid_t vfork(void) __RETURNS_TWICE;
int execve(const char*, char* const[], char* const[]);
int execv(const char*, char* const[]);
int execvp(const char*, char* const[]);
int execvpe(const char*, char* const[], char* const[]);
int fexecve(int, char* const[], char* const[]);
int execl(const char*, const char*, ...);
int execlp(const char*, const char*, ...);
int execle(const char*, const char*, ...);
__NORETURN void _exit(int);

pid_t getpid(void);
pid_t getppid(void);
pid_t gettid(void);
pid_t getpgrp(void);
pid_t getpgid(pid_t);
int setpgid(pid_t, pid_t);
int setpgrp(void);
pid_t getsid(pid_t);
pid_t setsid(void);
pid_t tcgetpgrp(int);
int tcsetpgrp(int, pid_t);

uid_t getuid(void);
uid_t geteuid(void);
gid_t getgid(void);
gid_t getegid(void);
int setuid(uid_t);
int seteuid(uid_t);
int setgid(gid_t);
int setegid(gid_t);
int setreuid(uid_t, uid_t);
int setregid(gid_t, gid_t);
int getresuid(uid_t*, uid_t*, uid_t*);
int getresgid(gid_t*, gid_t*, gid_t*);
int setresuid(uid_t, uid_t, uid_t);
int setresgid(gid_t, gid_t, gid_t);
int getgroups(int, gid_t[]);

int access(const char*, int);
int faccessat(int, const char*, int, int);
int eaccess(const char*, int);
int chdir(const char*);
int fchdir(int);
int chroot(const char*);
/*
 * attach gives a directory over a connection: fd an end of a pair whose
 * other end speaks the file protocol (lib/protocol/protocol.h), the new
 * descriptor a directory whose calls by path go to who serves that end,
 * as rootfs gives its processes their root (fchdir, then chroot(".")).
 */
int attach(int);
/*
 * fdid gives the id of the kernel's object of a descriptor: the same for
 * the descriptors of one object, dup's, fork's and SCM_RIGHTS's among
 * them, and never another's, while the system runs. A pipe's two ends
 * are one pipe's, a socket pair's two ends one pair's, and an open file
 * of a server's is of its channel, the pair the server keeps an end of,
 * which so knows the open file it is given back as its own.
 */
int fdid(int, unsigned long long*);
/*
 * ctl calls the server of a descriptor's file (CTL, lib/protocol): the
 * request's bytes and descriptors go to it, with who calls, which the
 * kernel says, and the answer's come back, its bytes into out, as many as
 * room holds, room then the answer's size, and its descriptors into got,
 * as many as ngot holds, ngot then their count, a server's files of them
 * as the server says, O_CLOEXEC of flags. It fails with the server's
 * errno, ENOTTY of a file no server serves. A signal ends a call that
 * waits, EINTR, or restarts it with CTL_RESTART where its handler has
 * SA_RESTART. At most 3 descriptors go.
 */
struct ctl {
    const void* in;
    size_t size;
    const int* fds;
    size_t nfds;
    void* out;
    size_t room;
    int* got;
    size_t ngot;
};
#define CTL_RESTART 1
int ctl(int, struct ctl*, int);
char* getcwd(char*, size_t);
int unlink(const char*);
int unlinkat(int, const char*, int);
int rmdir(const char*);
int link(const char*, const char*);
int linkat(int, const char*, int, const char*, int);
int symlink(const char*, const char*);
int symlinkat(const char*, int, const char*);
ssize_t readlink(const char* __restrict, char* __restrict, size_t);
ssize_t readlinkat(int, const char* __restrict, char* __restrict, size_t);
int chown(const char*, uid_t, gid_t);
int fchown(int, uid_t, gid_t);
int lchown(const char*, uid_t, gid_t);
int fchownat(int, const char*, uid_t, gid_t, int);

int isatty(int);
char* ttyname(int);
int ttyname_r(int, char*, size_t);
char* getlogin(void);
int getlogin_r(char*, size_t);
int gethostname(char*, size_t);
int sethostname(const char*, size_t);
int getentropy(void*, size_t);
int getdomainname(char*, size_t);
int setdomainname(const char*, size_t);
long gethostid(void);
char* getusershell(void);
void setusershell(void);
void endusershell(void);
char* crypt(const char*, const char*);
char* getpass(const char*);
int daemon(int, int);
int acct(const char*);

unsigned sleep(unsigned);
int usleep(useconds_t);
unsigned alarm(unsigned);
int pause(void);
int nice(int);

long sysconf(int);
long pathconf(const char*, int);
long fpathconf(int, int);
size_t confstr(int, char*, size_t);
int getpagesize(void);
int getdtablesize(void);
void swab(const void* __restrict, void* __restrict, ssize_t);

/*
 * Inline only: vfork's child runs in its parent's memory, in the caller's
 * frame, which the parent's later return lands in. There is no definition
 * to point to.
 */
pid_t __kernel_vfork(void) __RETURNS_TWICE __KERNEL(vfork);

__INLINE pid_t vfork(void) {
    return __kernel_vfork();
}

__END_DECLS
