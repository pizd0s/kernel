#pragma once

/*
 * libfuse 2.9's option parsing, as the system's libfuse has it (lib/fuse).
 */

#include <__base.h>

__BEGIN_DECLS

struct fuse_opt {
    const char* templ;
    unsigned long offset;
    int value;
};

#define FUSE_OPT_KEY(templ, key) {templ, -1U, key}
#define FUSE_OPT_END {0, 0, 0}

struct fuse_args {
    int argc;
    char** argv;
    int allocated;
};

#define FUSE_ARGS_INIT(argc, argv) {argc, argv, 0}

#define FUSE_OPT_KEY_OPT -1
#define FUSE_OPT_KEY_NONOPT -2
#define FUSE_OPT_KEY_KEEP -3
#define FUSE_OPT_KEY_DISCARD -4

typedef int (*fuse_opt_proc_t)(void* data, const char* arg, int key, struct fuse_args* outargs);

int fuse_opt_parse(struct fuse_args* args, void* data, const struct fuse_opt opts[], fuse_opt_proc_t proc);
int fuse_opt_add_opt(char** opts, const char* opt);
int fuse_opt_add_opt_escaped(char** opts, const char* opt);
int fuse_opt_add_arg(struct fuse_args* args, const char* arg);
int fuse_opt_insert_arg(struct fuse_args* args, int pos, const char* arg);
void fuse_opt_free_args(struct fuse_args* args);
int fuse_opt_match(const struct fuse_opt opts[], const char* opt);

__END_DECLS
