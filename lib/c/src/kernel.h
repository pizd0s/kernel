#pragma once

#include <__base.h>

/*
 * Imports private to lib/c. __kernel_start hands the kernel the addresses it
 * keeps current for the process (errno's, which __errno_location gives;
 * environ, tzname, timezone and daylight, which tzset sets; and one of the
 * host functions' of netdb.h, h_errno of an earlier lib/c, none of this
 * one) and receives the argument vector, which the kernel has already
 * placed in the process heap.
 */
void __kernel_start(int* errnoAt, char*** environAt, int* argc, char*** argv, char** tznameAt, long* timezoneAt,
                    int* daylightAt, int* herrnoAt) __KERNEL(__kernel_start);
void __kernel_syslog(int priority, const char* message) __KERNEL(__kernel_syslog);

// __kernel_program is getcwd of the program's file: its absolute path,
// through its links, or a failure when it is outside the root.
char* __kernel_program(char* path, size_t size) __KERNEL(__kernel_program);

// __kernel_name is the name the program was started by, the last component
// of its argv[0], or null where it has no arguments: what a message of
// lib/c's begins with.
const char* __kernel_name(void) __KERNEL(__kernel_name);
