#include "kernel.h"

#include <stdlib.h>
#include <unistd.h>

/*
 * The entry of a program on lib/c/lite, which the kernel runs with its Go
 * alone and no libc.wasm: the compiler, which the kernel links as its
 * native build compiled it, with no --libc (dev/aot.py). The start is its
 * own: __kernel_begin tells the kernel errno's and environ's places and
 * gives the arguments, then the constructors run and main, and main's
 * end is the process's, with nothing to run or flush. getenv is of the
 * environment the start got.
 */
static int errnoValue;
char** environ;

int __main_argc_argv(int, char**);

// The module's constructors, C++'s static objects among them (wasm-ld).
void __wasm_call_ctors(void);

__attribute__((__export_name__("_start"))) void _start(void) {
    int argc = 0;
    char** argv = 0;

    __kernel_begin(&errnoValue, &environ, &argc, &argv);
    __wasm_call_ctors();
    _exit(__main_argc_argv(argc, argv));
}

char* getenv(const char* name) {
    for (char** entry = environ; entry && *entry; ++entry) {
        size_t i = 0;

        while (name[i] != 0 && name[i] != '=' && (*entry)[i] == name[i]) {
            ++i;
        }
        if (i != 0 && name[i] == 0 && (*entry)[i] == '=') {
            return *entry + i + 1;
        }
    }

    return 0;
}
