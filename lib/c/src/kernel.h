#pragma once

#include <__base.h>

/*
 * Imports private to lib/c, and what of them is shared with libc.wasm.
 *
 * A program's start (crt.c) is a call of __kernel_start, libc.wasm's
 * (start.c), with the places of the program's variables of POSIX's:
 * errno's and environ's, which the kernel keeps current for the process
 * (errno's, which __errno_location gives), tzname, timezone and daylight,
 * which tzset sets, getopt's variables and signgam; the module's
 * constructors and main, and the structure's size. __kernel_begin hands
 * the kernel's Go errno's and environ's places and receives the argument
 * vector, which the kernel has already placed in the process heap; then
 * the constructors run, then main, and exit ends the process with what
 * main returns. libc.wasm keeps the structure, __program, for its
 * functions of the program's variables: environ's, getopt's, lgamma's,
 * tzcode's and getdate's.
 */
struct __start {
    int* errnoAt;
    char*** environAt;
    char** tznameAt;
    long* timezoneAt;
    int* daylightAt;
    char** optargAt;
    int* optindAt;
    int* opterrAt;
    int* optoptAt;
    int* signgamAt;
    void (*constructors)(void);
    int (*main)(int, char**);
};

__NORETURN void __kernel_start(const struct __start* start, size_t size) __KERNEL(start);
extern const struct __start* __program;
void __kernel_begin(int* errnoAt, char*** environAt, int* argc, char*** argv) __KERNEL(__kernel_begin);

// __kernel_program is getcwd of the program's file: its absolute path,
// through its links, or a failure when it is outside the root.
char* __kernel_program(char* path, size_t size) __KERNEL(__kernel_program);

// __kernel_fdioctl is the kernel's of ioctl's requests, of a descriptor of
// any kind: FIONBIO, FIOCLEX and FIONCLEX; FIONREAD of what the kernel
// keeps, a pipe's or a socket's; TIOCSCTTY and TIOCNOTTY, of the
// controlling terminal. Another is ENOTTY: a terminal's are its server's,
// which lib/c's ioctl calls (ctl).
int __kernel_fdioctl(int fd, unsigned long request, void* arg) __KERNEL(__kernel_fdioctl);

// The kernel's of what lib/c's functions of paths and of sockets make of
// them (vfs.c, sockets.c, exec.c): the kernel knows no path. __kernel_exec
// runs the program of a descriptor, path the name it was asked by and
// program its path from the root, __kernel_program's, "" for none; a
// file of "#!" is ENOEXEC. __kernel_sockrecv and __kernel_socksend are
// a socket's read and write: the bytes of the vector, flags as recv's
// and send's, and with x an address, descriptors, the rights, and what
// a receive says of what it took.
struct sockaddr;
struct msghdr;
int __kernel_exec(int fd, const char* path, const char* program, char* const argv[], char* const envp[]) __KERNEL(__kernel_exec);
struct iovec;
struct __kernel_sockio {
    void* addr;     // where a send goes; where a receive puts its sender
    unsigned alen;  // its size; of a receive the room, then the sender's size
    int* fds;       // the rights
    unsigned nfds;  // how many; of a receive the room, then how many came
    unsigned flags; // what a receive says: MSG_TRUNC, MSG_CTRUNC
};
long __kernel_sockrecv(int fd, const struct iovec* iov, unsigned count, int flags, struct __kernel_sockio* x) __KERNEL(__kernel_sockrecv);
long __kernel_socksend(int fd, const struct iovec* iov, unsigned count, int flags, const struct __kernel_sockio* x) __KERNEL(__kernel_socksend);

// __kernel_name is the name the program was started by, the last component
// of its argv[0], or null where it has no arguments: what a message of
// lib/c's begins with.
const char* __kernel_name(void) __KERNEL(__kernel_name);

