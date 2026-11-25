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

// __kernel_name is the name the program was started by, the last component
// of its argv[0], or null where it has no arguments: what a message of
// lib/c's begins with.
const char* __kernel_name(void) __KERNEL(__kernel_name);

