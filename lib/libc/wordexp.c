/*
 * wordexp: the words the system's shell makes of the text as a command's
 * arguments, which it prints, each ended by a NUL, after an "x" of its
 * own: no "x" is an error of the shell's. The text is checked first, as
 * the shell would take it: an unquoted |, &, ;, <, >, (, ), {, } or newline
 * is WRDE_BADCHAR, a command's substitution with WRDE_NOCMD WRDE_CMDSUB,
 * an open quote or substitution WRDE_SYNTAX. With WRDE_UNDEF the shell
 * runs with set -u, and its error is WRDE_BADVAL when the text expands
 * without. Its standard error is /dev/null but with WRDE_SHOWERR.
 */
#include "kernel.h"
#include "sized.h"

#include <errno.h>
#include <fcntl.h>
#include <spawn.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>
#include <wordexp.h>

// A quote or substitution the text is in: of a command (`` ` `` or '('),
// of arithmetic ('a'), of a parameter ('{'), or none; whether in double
// quotes, and how many parentheses of its own are open.
struct Frame {
    char kind;
    char quoted;
    int depth;
};

static int checked(const char* s, int noCommands) {
    size_t length = strlen(s);
    struct Frame* frames = calloc(length + 1, sizeof *frames);
    size_t top = 0;
    int single = 0;
    int e = 0;

    if (!frames) {
        return WRDE_NOSPACE;
    }

    for (size_t i = 0; i < length && !e; i++) {
        char c = s[i];
        struct Frame* f = &frames[top];

        if (single) {
            single = c != '\'';
        } else if (c == '\\') {
            e = ++i >= length ? WRDE_SYNTAX : 0;
        } else if (c == '\'' && !f->quoted) {
            single = 1;
        } else if (c == '"') {
            f->quoted = !f->quoted;
        } else if (c == '`') {
            if (noCommands) {
                e = WRDE_CMDSUB;
            } else if (f->kind == '`') {
                top--;
            } else {
                frames[++top] = (struct Frame){'`', 0, 0};
            }
        } else if (c == '$' && strncmp(s + i, "$((", 3) == 0) {
            frames[++top] = (struct Frame){'a', 0, 0};
            i += 2;
        } else if (c == '$' && strncmp(s + i, "$(", 2) == 0) {
            if (noCommands) {
                e = WRDE_CMDSUB;
            } else {
                frames[++top] = (struct Frame){'(', 0, 0};
                i++;
            }
        } else if (c == '$' && strncmp(s + i, "${", 2) == 0) {
            frames[++top] = (struct Frame){'{', 0, 0};
            i++;
        } else if (f->quoted || f->kind == '`') {
        } else if (f->kind == '(' || f->kind == 'a') {
            if (c == '(') {
                f->depth++;
            } else if (c == ')' && f->depth > 0) {
                f->depth--;
            } else if (c == ')' && f->kind == 'a' && s[i + 1] != ')') {
                e = WRDE_SYNTAX;
            } else if (c == ')') {
                i += f->kind == 'a';
                top--;
            }
        } else if (f->kind == '{') {
            top -= c == '}';
        } else if (strchr("\n|&;<>(){}", c)) {
            e = WRDE_BADCHAR;
        }
    }

    if (!e && (single || top > 0 || frames[0].quoted)) {
        e = WRDE_SYNTAX;
    }

    free(frames);

    return e;
}

// What the shell printed of the text, on the heap, and its length; null
// where it could not run.
static char* shell(const char* text, int flags, size_t* length) {
    static const char print[] = "eval \"printf %s\\\\\\\\0 x $1\"";
    static const char strict[] = "set -u; eval \"printf %s\\\\\\\\0 x $1\"";
    char* argv[] = {"sh", "-c", (char*)(flags & WRDE_UNDEF ? strict : print), "sh", (char*)text, NULL};
    int fds[2];

    if (pipe2(fds, O_CLOEXEC) != 0) {
        return NULL;
    }

    posix_spawn_file_actions_t actions;
    pid_t pid = 0;
    int failed = posix_spawn_file_actions_init(&actions) || posix_spawn_file_actions_adddup2(&actions, fds[1], 1) ||
                 (!(flags & WRDE_SHOWERR) && posix_spawn_file_actions_addopen(&actions, 2, "/dev/null", O_WRONLY, 0)) ||
                 posix_spawn(&pid, "/bin/sh", &actions, NULL, argv, *__program->environAt);

    posix_spawn_file_actions_destroy(&actions);
    close(fds[1]);

    char* out = NULL;
    size_t n = 0;

    for (size_t room = 0; !failed;) {
        if (n == room) {
            char* more = realloc(out, room = room ? 2 * room : 256);

            if (!more) {
                failed = 1;

                break;
            }

            out = more;
        }

        ssize_t got = read(fds[0], out + n, room - n);

        if (got < 0 && errno == EINTR) {
            continue;
        }

        if (got <= 0) {
            break;
        }

        n += got;
    }

    close(fds[0]);

    if (pid > 0) {
        while (waitpid(pid, NULL, 0) < 0 && errno == EINTR) {
        }
    }

    if (failed) {
        free(out);

        return NULL;
    }

    *length = n;

    return out;
}

// Whether the shell printed its "x": then the words follow it.
static int expanded(const char* out, size_t n) {
    return out && n >= 2 && out[0] == 'x' && out[1] == 0;
}

static int failure(wordexp_t* we, int flags, int e) {
    if (!(flags & WRDE_APPEND)) {
        we->we_wordc = 0;
        we->we_wordv = NULL;
    }

    return e;
}

void __kernel_wordfree(wordexp_t* we, size_t size) {
    sized("wordfree", size, sizeof(wordexp_t));

    if (!we->we_wordv) {
        return;
    }

    for (char** w = we->we_wordv + we->we_offs; *w; w++) {
        free(*w);
    }

    free(we->we_wordv);
    we->we_wordv = NULL;
}

int __kernel_wordexp(const char* restrict text, wordexp_t* restrict we, size_t size, int flags) {
    sized("wordexp", size, sizeof(wordexp_t));

    int e = checked(text, flags & WRDE_NOCMD);

    if (e) {
        return failure(we, flags, e);
    }

    if (flags & WRDE_REUSE) {
        __kernel_wordfree(we, size);
        flags &= ~WRDE_APPEND;
    }

    size_t n = 0;
    char* out = shell(text, flags, &n);

    if (!expanded(out, n)) {
        e = WRDE_SYNTAX;

        if (flags & WRDE_UNDEF) {
            size_t m = 0;
            char* plain = shell(text, flags & ~WRDE_UNDEF, &m);

            e = expanded(plain, m) ? WRDE_BADVAL : e;
            free(plain);
        }

        free(out);

        return failure(we, flags, e);
    }

    size_t offs = flags & WRDE_DOOFFS ? we->we_offs : 0;
    size_t old = flags & WRDE_APPEND && we->we_wordv ? we->we_wordc : 0;
    size_t count = 0;

    if (!(flags & WRDE_DOOFFS)) {
        we->we_offs = 0;
    }

    for (size_t i = 2; i < n; i++) {
        count += out[i] == 0;
    }

    char** vector = offs > 1 << 26 ? NULL : malloc((offs + old + count + 1) * sizeof *vector);

    if (!vector) {
        free(out);

        return WRDE_NOSPACE;
    }

    for (size_t i = 0; i < offs; i++) {
        vector[i] = NULL;
    }

    for (size_t i = 0; i < old; i++) {
        vector[offs + i] = we->we_wordv[offs + i];
    }

    const char* word = out + 2;

    for (size_t i = 0; i < count; i++) {
        char* copy = strdup(word);

        if (!copy) {
            for (size_t k = 0; k < i; k++) {
                free(vector[offs + old + k]);
            }

            free(vector);
            free(out);

            return WRDE_NOSPACE;
        }

        vector[offs + old + i] = copy;
        word += strlen(word) + 1;
    }

    vector[offs + old + count] = NULL;
    free(out);

    if (flags & WRDE_APPEND && we->we_wordv) {
        free(we->we_wordv);
    }

    we->we_wordc = old + count;
    we->we_wordv = vector;

    return 0;
}
