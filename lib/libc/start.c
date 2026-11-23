/*
 * A program's start, which its entry calls (lib/c's crt.c, kernel.h), and
 * the functions the kernel calls the program's through: a signal's handler
 * or action, and glob's function of errors. Exported, they are the
 * program's exports once the kernel has linked libc.wasm into it. A start
 * of another size than this lib/c's is no program's that can run.
 */
#include "kernel.h"

#include <signal.h>
#include <stdlib.h>

const struct __start* __program;

void __kernel_start(const struct __start* start, size_t size) {
    int argc = 0;
    char** argv = 0;

    if (size != sizeof *start) {
        abort();
    }

    __program = start;
    __kernel_begin(start->errnoAt, start->environAt, &argc, &argv, start->tznameAt, start->timezoneAt, start->daylightAt);
    start->constructors();
    exit(start->main(argc, argv));
}

__attribute__((__export_name__("__call_signal_handler"))) void __call_signal_handler(void (*handler)(int), int signal) {
    handler(signal);
}

__attribute__((__export_name__("__call_signal_action"))) void __call_signal_action(void (*action)(int, siginfo_t*, void*), int signal,
                                                                                   siginfo_t* info, void* context) {
    action(signal, info, context);
}

__attribute__((__export_name__("__call_glob_error"))) int __call_glob_error(int (*error)(const char*, int), const char* path, int code) {
    return error(path, code);
}
