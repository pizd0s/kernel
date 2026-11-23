#include "kernel.h"

#include <math.h>
#include <netinet/in.h>
#include <signal.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>

/*
 * The program's entry, and what of the C library is a variable of the
 * program's by POSIX: environ, tzname, timezone and daylight, whose places
 * the kernel gets at the start and keeps current; getopt's and lgamma's,
 * whose places the kernel's functions get at each call (kernel.c); and the
 * two addresses of netinet/in.h. errno, a macro, is the kernel's, as the
 * standard streams and getdate_err are; its int is here, the kernel's to
 * give (__errno_location). __dso_handle is the compiler's, the object a
 * C++ destructor is registered with.
 */
static int errnoValue;
char** environ;
char* tzname[2] = {"UTC", "UTC"};
long timezone;
int daylight;
char* optarg;
int optind = 1;
int opterr = 1;
int optopt;
int signgam;
const struct in6_addr in6addr_any = IN6ADDR_ANY_INIT;
const struct in6_addr in6addr_loopback = IN6ADDR_LOOPBACK_INIT;
void* __dso_handle;

int __main_argc_argv(int, char**);

// The module's constructors, C++'s static objects among them (wasm-ld).
void __wasm_call_ctors(void);

__attribute__((__export_name__("_start"))) void _start(void) {
    int argc = 0;
    char** argv = 0;

    __kernel_start(&errnoValue, &environ, &argc, &argv, tzname, &timezone, &daylight, 0);
    __wasm_call_ctors();
    exit(__main_argc_argv(argc, argv));
}

__attribute__((__export_name__("__call_signal_handler"))) void __call_signal_handler(void (*handler)(int), int signal) {
    handler(signal);
}

__attribute__((__export_name__("__call_signal_action"))) void __call_signal_action(void (*action)(int, siginfo_t*, void*), int signal, siginfo_t* info, void* context) {
    action(signal, info, context);
}

__attribute__((__export_name__("__call_glob_error"))) int __call_glob_error(int (*error)(const char*, int), const char* path, int code) {
    return error(path, code);
}
