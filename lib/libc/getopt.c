/*
 * getopt, FreeBSD's, for a program's variables: optarg, optind, opterr and
 * optopt are the program's, and a call comes with their places after its
 * own arguments. FreeBSD's functions keep the four as variables of their
 * own, and optreset beside, which starts a scan anew: what the program has
 * in optind, opterr and optopt goes into them before a call, and what the
 * call leaves comes back after it. An optind the program has changed since
 * the last call starts a scan anew, one of 0, GNU's way to say so, at the
 * first argument. A long option comes with its size, as the kernel's
 * structures do, the one layout it has had.
 */
#include <getopt.h>
#include <unistd.h>

extern int optreset;

static void begin(const int* ind, const int* err, const int* opt) {
    if (*ind != optind) {
        optreset = 1;
        optind = *ind ? *ind : 1;
    }

    opterr = *err;
    optopt = *opt;
}

static int end(int result, char** arg, int* ind, int* opt) {
    *arg = optarg;
    *ind = optind;
    *opt = optopt;

    return result;
}

int __kernel_getopt(int argc, char* const* argv, const char* options, char** arg, int* ind, int* err, int* opt) {
    begin(ind, err, opt);

    return end(getopt(argc, argv, options), arg, ind, opt);
}

int __kernel_getopt_long(int argc, char* const* argv, const char* options, const struct option* longs, size_t size, int* index,
                         char** arg, int* ind, int* err, int* opt) {
    begin(ind, err, opt);

    return end(getopt_long(argc, argv, options, longs, index), arg, ind, opt);
}

int __kernel_getopt_long_only(int argc, char* const* argv, const char* options, const struct option* longs, size_t size, int* index,
                              char** arg, int* ind, int* err, int* opt) {
    begin(ind, err, opt);

    return end(getopt_long_only(argc, argv, options, longs, index), arg, ind, opt);
}
