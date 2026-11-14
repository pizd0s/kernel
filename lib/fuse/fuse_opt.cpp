// libfuse 2.9's option parsing (fuse_opt.h): a file system's options
// matched against its templates, "-o a,b" lists split, what no template
// takes handed to its function or kept for fuse_main.

#include "fuse_opt.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

namespace {
    struct Context {
        void* data;
        const fuse_opt* opt;
        fuse_opt_proc_t proc;
        int argctr;
        int argc;
        char** argv;
        fuse_args outargs;
        char* opts;
        int nonopt;
    };

    static int failed() {
        fprintf(stderr, "fuse: out of memory\n");
        return -1;
    }

    static int addOptCommon(char** opts, const char* opt, bool escape) {
        const size_t old = *opts ? strlen(*opts) : 0;
        auto* d = static_cast<char*>(realloc(*opts, old + 1 + strlen(opt) * 2 + 1));
        if (!d) {
            return failed();
        }
        *opts = d;
        if (old) {
            d += old;
            *d++ = ',';
        }
        for (; *opt; ++opt) {
            if (escape && (*opt == ',' || *opt == '\\')) {
                *d++ = '\\';
            }
            *d++ = *opt;
        }
        *d = 0;
        return 0;
    }

    static int nextArg(Context& c, const char* opt) {
        if (c.argctr + 1 >= c.argc) {
            fprintf(stderr, "fuse: missing argument after `%s'\n", opt);
            return -1;
        }
        ++c.argctr;
        return 0;
    }

    static int callProc(Context& c, const char* arg, int key, bool iso) {
        if (key == FUSE_OPT_KEY_DISCARD) {
            return 0;
        }
        if (key != FUSE_OPT_KEY_KEEP && c.proc) {
            const int res = c.proc(c.data, arg, key, &c.outargs);
            if (res == -1 || res == 0) {
                return res;
            }
        }
        return iso ? addOptCommon(&c.opts, arg, true) : fuse_opt_add_arg(&c.outargs, arg);
    }

    // Whether a template takes an argument: "name" alone, or "name=%..." and
    // "name %..." by the name; sep is where the parameter starts.
    static bool matches(const char* t, const char* arg, unsigned& sep) {
        const size_t length = strlen(arg);
        const char* at = strchr(t, '=');
        at = at ? at : strchr(t, ' ');
        if (at && (!at[1] || at[1] == '%')) {
            size_t n = size_t(at - t);
            if (at[0] == '=') {
                ++n;
            }
            if (length >= n && strncmp(arg, t, n) == 0) {
                sep = unsigned(at - t);
                return true;
            }
        }
        if (strcmp(t, arg) == 0) {
            sep = 0;
            return true;
        }
        return false;
    }

    static const fuse_opt* find(const fuse_opt* opt, const char* arg, unsigned& sep) {
        for (; opt && opt->templ; ++opt) {
            if (matches(opt->templ, arg, sep)) {
                return opt;
            }
        }
        return nullptr;
    }

    static int param(void* var, const char* format, const char* value, const char* arg) {
        if (format[1] == 's') {
            char* copy = strdup(value);
            if (!copy) {
                return failed();
            }
            *static_cast<char**>(var) = copy;
            return 0;
        }
        if (sscanf(value, format, var) != 1) {
            fprintf(stderr, "fuse: invalid parameter in option `%s'\n", arg);
            return -1;
        }
        return 0;
    }

    static int process(Context& c, const fuse_opt* opt, unsigned sep, const char* arg, bool iso) {
        if (opt->offset == -1U) {
            return callProc(c, arg, opt->value, iso) == -1 ? -1 : 0;
        }
        void* var = static_cast<char*>(c.data) + opt->offset;
        if (sep && opt->templ[sep + 1]) {
            const char* value = arg + sep + (opt->templ[sep] == '=' ? 1 : 0);
            return param(var, opt->templ + sep + 1, value, arg);
        }
        *static_cast<int*>(var) = opt->value;
        return 0;
    }

    static int processSeparate(Context& c, const fuse_opt* opt, unsigned sep, const char* arg, bool iso) {
        if (nextArg(c, arg) == -1) {
            return -1;
        }
        const char* value = c.argv[c.argctr];
        auto* joined = static_cast<char*>(malloc(sep + strlen(value) + 1));
        if (!joined) {
            return failed();
        }
        memcpy(joined, arg, sep);
        strcpy(joined + sep, value);
        const int res = process(c, opt, sep, joined, iso);
        free(joined);
        return res;
    }

    static int processGeneral(Context& c, const char* arg, bool iso) {
        unsigned sep;
        const fuse_opt* opt = find(c.opt, arg, sep);
        if (!opt) {
            return callProc(c, arg, FUSE_OPT_KEY_OPT, iso);
        }
        for (; opt; opt = find(opt + 1, arg, sep)) {
            const int res = sep && opt->templ[sep] == ' ' && !arg[sep] ? processSeparate(c, opt, sep, arg, iso) : process(c, opt, sep, arg, iso);
            if (res == -1) {
                return -1;
            }
        }
        return 0;
    }

    // An "-o" list: options by commas, "\," a comma of one.
    static int processGroup(Context& c, const char* list) {
        char* copy = strdup(list);
        if (!copy) {
            return failed();
        }
        char* opt = copy;
        char* d = copy;
        for (const char* s = copy;; ++s) {
            if (*s == ',' || *s == 0) {
                *d = 0;
                if (processGeneral(c, opt, true) == -1) {
                    free(copy);
                    return -1;
                }
                if (*s == 0) {
                    break;
                }
                d = opt;
                continue;
            }
            if (s[0] == '\\' && s[1] != 0) {
                ++s;
            }
            *d++ = *s;
        }
        free(copy);
        return 0;
    }

    static int processOne(Context& c, const char* arg) {
        if (c.nonopt || arg[0] != '-') {
            return callProc(c, arg, FUSE_OPT_KEY_NONOPT, false);
        }
        if (arg[1] == 'o') {
            if (arg[2]) {
                return processGroup(c, arg + 2);
            }
            if (nextArg(c, arg) == -1) {
                return -1;
            }
            return processGroup(c, c.argv[c.argctr]);
        }
        if (arg[1] == '-' && !arg[2]) {
            if (fuse_opt_add_arg(&c.outargs, arg) == -1) {
                return -1;
            }
            c.nonopt = c.outargs.argc;
            return 0;
        }
        return processGeneral(c, arg, false);
    }
}

void fuse_opt_free_args(fuse_args* args) {
    if (args && args->argv && args->allocated) {
        for (int i = 0; i < args->argc; ++i) {
            free(args->argv[i]);
        }
        free(args->argv);
    }
    if (args) {
        *args = {0, nullptr, 0};
    }
}

int fuse_opt_add_arg(fuse_args* args, const char* arg) {
    if (args->argv && !args->allocated) {
        // The caller's arguments, copied before they grow.
        fuse_args own = {0, nullptr, 1};
        for (int i = 0; i < args->argc; ++i) {
            if (fuse_opt_add_arg(&own, args->argv[i]) == -1) {
                fuse_opt_free_args(&own);
                return -1;
            }
        }
        *args = own;
    }
    char* copy = strdup(arg);
    auto* argv = static_cast<char**>(realloc(args->argv, size_t(args->argc + 2) * sizeof(char*)));
    if (!copy || !argv) {
        free(copy);
        if (argv) {
            args->argv = argv;
        }
        return failed();
    }
    args->argv = argv;
    args->allocated = 1;
    args->argv[args->argc++] = copy;
    args->argv[args->argc] = nullptr;
    return 0;
}

int fuse_opt_insert_arg(fuse_args* args, int pos, const char* arg) {
    if (fuse_opt_add_arg(args, arg) == -1) {
        return -1;
    }
    if (pos != args->argc - 1) {
        char* added = args->argv[args->argc - 1];
        memmove(&args->argv[pos + 1], &args->argv[pos], sizeof(char*) * size_t(args->argc - pos - 1));
        args->argv[pos] = added;
    }
    return 0;
}

int fuse_opt_add_opt(char** opts, const char* opt) {
    return addOptCommon(opts, opt, false);
}

int fuse_opt_add_opt_escaped(char** opts, const char* opt) {
    return addOptCommon(opts, opt, true);
}

int fuse_opt_match(const fuse_opt opts[], const char* opt) {
    unsigned sep;
    return find(opts, opt, sep) ? 1 : 0;
}

int fuse_opt_parse(fuse_args* args, void* data, const fuse_opt opts[], fuse_opt_proc_t proc) {
    if (!args || !args->argv || !args->argc) {
        return 0;
    }
    Context c{data, opts, proc, 0, args->argc, args->argv, {0, nullptr, 0}, nullptr, 0};
    int res = fuse_opt_add_arg(&c.outargs, c.argv[0]);
    for (c.argctr = 1; res != -1 && c.argctr < c.argc; ++c.argctr) {
        res = processOne(c, c.argv[c.argctr]);
    }
    if (res != -1 && c.opts && (fuse_opt_insert_arg(&c.outargs, 1, "-o") == -1 || fuse_opt_insert_arg(&c.outargs, 2, c.opts) == -1)) {
        res = -1;
    }
    if (res != -1 && c.nonopt && c.nonopt == c.outargs.argc && strcmp(c.outargs.argv[c.outargs.argc - 1], "--") == 0) {
        free(c.outargs.argv[c.outargs.argc - 1]);
        c.outargs.argv[--c.outargs.argc] = nullptr;
    }
    if (res != -1) {
        const fuse_args old = *args;
        *args = c.outargs;
        c.outargs = old;
    }
    free(c.opts);
    fuse_opt_free_args(&c.outargs);
    return res == -1 ? -1 : 0;
}
