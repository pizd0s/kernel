/*
 * A program's start, which its entry calls (lib/c's crt.c, kernel.h), and
 * the functions the kernel calls the program's through: a signal's handler
 * or action. Exported, they are the program's exports once the kernel has
 * linked libc.wasm into it. A start of another size than this lib/c's is
 * no program's that can run; nor is any structure of another (sized.h).
 */
#include "kernel.h"
#include "sized.h"

#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

const struct __start* __program;

void __kernel_start(const struct __start* start, size_t size) {
    int argc = 0;
    char** argv = 0;

    sized("start", size, sizeof *start);

    __program = start;
    __kernel_begin(start->errnoAt, start->environAt, &argc, &argv, start->tznameAt, start->timezoneAt, start->daylightAt);
    start->constructors();
    exit(start->main(argc, argv));
}

// The kernel's message of a structure of a size it knows not, and SIGSYS,
// of its default action, which nothing blocks.
_Noreturn void unsized(const char* call, size_t size, size_t known) {
    const char* name = __kernel_name();
    char text[256];
    int n = snprintf(text, sizeof text, "%s[%d]: %s: a structure of %zu bytes, which this kernel knows of %zu\n", name ? name : "?",
                     (int)getpid(), call, size, known);
    sigset_t blocked;

    write(STDERR_FILENO, text, n < (int)sizeof text ? n : (int)sizeof text - 1);
    signal(SIGSYS, SIG_DFL);
    sigemptyset(&blocked);
    sigaddset(&blocked, SIGSYS);
    sigprocmask(SIG_UNBLOCK, &blocked, NULL);
    raise(SIGSYS);
    _Exit(128 + SIGSYS);
}

__attribute__((__export_name__("__call_signal_handler"))) void __call_signal_handler(void (*handler)(int), int signal) {
    handler(signal);
}

__attribute__((__export_name__("__call_signal_action"))) void __call_signal_action(void (*action)(int, siginfo_t*, void*), int signal,
                                                                                   siginfo_t* info, void* context) {
    action(signal, info, context);
}
