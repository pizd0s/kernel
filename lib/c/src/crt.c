#include "kernel.h"

#include <math.h>
#include <netinet/in.h>
#include <time.h>
#include <unistd.h>

/*
 * The program's entry, and what of the C library is a variable of the
 * program's by POSIX: environ, tzname, timezone and daylight, getopt's
 * and signgam, whose places the kernel gets at the start, and the two
 * addresses of netinet/in.h. errno, a macro, is the kernel's, as the
 * standard streams and getdate_err are; its int is here, the kernel's to
 * give (__errno_location). __dso_handle is the compiler's, the object a
 * C++ destructor is registered with. The start is libc.wasm's
 * (kernel.h): the entry gives it the places, the constructors and main.
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

static const struct __start program = {
    &errnoValue, &environ, tzname, &timezone, &daylight, &optarg, &optind, &opterr, &optopt, &signgam, __wasm_call_ctors, __main_argc_argv,
};

__attribute__((__export_name__("_start"))) void _start(void) {
    __kernel_start(&program, sizeof program);
}
