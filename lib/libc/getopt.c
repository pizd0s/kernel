/*
 * getopt, FreeBSD's, for a program's variables: optarg, optind, opterr and
 * optopt are the program's, whose places libc.wasm has from its start
 * (kernel.h). FreeBSD's functions keep the four as variables of their own,
 * and optreset beside, which starts a scan anew: what the program has in
 * optind, opterr and optopt goes into them before a call, and what the call
 * leaves comes back after it. An optind the program has changed since the
 * last call starts a scan anew, one of 0, GNU's way to say so, at the
 * first argument. A long option comes with its size, as the kernel's
 * structures do, the one layout it has had.
 */
#include "kernel.h"
#include "sized.h"

#include <getopt.h>
#include <unistd.h>

extern int optreset;

static void begin(void) {
    if (*__program->optindAt != optind) {
        optreset = 1;
        optind = *__program->optindAt ? *__program->optindAt : 1;
    }

    opterr = *__program->opterrAt;
    optopt = *__program->optoptAt;
}

static int end(int result) {
    *__program->optargAt = optarg;
    *__program->optindAt = optind;
    *__program->optoptAt = optopt;

    return result;
}

int __kernel_getopt(int argc, char* const* argv, const char* options) {
    begin();

    return end(getopt(argc, argv, options));
}

int __kernel_getopt_long(int argc, char* const* argv, const char* options, const struct option* longs, size_t size, int* index) {
    sized("getopt_long", size, sizeof(struct option));

    begin();

    return end(getopt_long(argc, argv, options, longs, index));
}

int __kernel_getopt_long_only(int argc, char* const* argv, const char* options, const struct option* longs, size_t size, int* index) {
    sized("getopt_long_only", size, sizeof(struct option));

    begin();

    return end(getopt_long_only(argc, argv, options, longs, index));
}
