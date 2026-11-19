#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * How the kernel runs the images of a process, the system's own: execmode
 * sets the modes of the caller, which its children have after it. What a
 * mode makes of an image's code, the sanitizer's checks and the trace of
 * its calls, it makes as the image starts: the image that calls runs on
 * as it was started. The rest of a trace is written from the call on.
 *
 * EXEC_ASAN compiles an image with the checks of the address sanitizer.
 * EXEC_TRACE has the kernel write what the process does to sink, a
 * descriptor the kernel keeps open for it: a line a thing, its pid first,
 * of the classes asked.
 *
 * TRACE_SYSTEM is an image's calls of the system, each as it returns,
 * with the arguments the kernel gets, a structure's size after its
 * pointer among them, and its result, the error's name after a result of
 * -1; a call that does not return, exit, writes none. TRACE_LIBRARY is
 * the calls of the functions of libc that are no system's: of strings, of
 * the heap, of characters, those of them the compiler has not put in
 * line. TRACE_PROCESS is a child's start, its parent's pid with it, the
 * execs and the end. TRACE_SIGNALS is the signals taken. TRACE_FAULTS is
 * a fault that ends the process, the sanitizer's report among them, which
 * goes to its standard error otherwise.
 *
 * A write to sink waits for room, as any does, and a sink whose reader is
 * gone ends the trace. EXEC_TRACE needs classes and a sink; without it
 * classes is 0 and sink -1. modes of 0 end both.
 */
#define EXEC_ASAN 1
#define EXEC_TRACE 2

#define TRACE_SYSTEM 1
#define TRACE_LIBRARY 2
#define TRACE_PROCESS 4
#define TRACE_SIGNALS 8
#define TRACE_FAULTS 16

int execmode(unsigned modes, unsigned classes, int sink);

__END_DECLS
