/*
 * glob: the pattern's components, between its slashes, each matched by
 * fnmatch against the names of its directory, "." and ".." of a component
 * of a leading period only, or taken as they are where they have no *, ?
 * or [ unescaped; a directory that cannot be read, but of ENOENT or
 * ENOTDIR, is errfunc's to stop at, and GLOB_ERR's. The paths found are
 * sorted but with GLOB_NOSORT, a directory's marked by a slash with
 * GLOB_MARK, after gl_offs nulls with GLOB_DOOFFS and after those found
 * before with GLOB_APPEND; with GLOB_NOCHECK the pattern is the one found
 * where there is none. A pattern's ~, with GLOB_TILDE or GLOB_TILDE_CHECK,
 * is the home directory: HOME's for ~ alone, the user's of /etc/passwd for
 * ~user; of none it is the pattern's, or no match with GLOB_TILDE_CHECK.
 */
#include "sized.h"

#include <dirent.h>
#include <errno.h>
#include <fnmatch.h>
#include <glob.h>
#include <pwd.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>

struct Glob {
    int flags;
    int (*errfunc)(const char*, int);
    char** found;
    size_t count, room;
    int error;
};

static int compare(const void* a, const void* b) {
    return strcmp(*(char* const*)a, *(char* const*)b);
}

// A copy of n bytes of s and of t's after them, or null.
static char* joined(const char* s, size_t n, const char* t) {
    size_t m = strlen(t);
    char* out = malloc(n + m + 1);

    if (out) {
        memcpy(out, s, n);
        memcpy(out + n, t, m + 1);
    }

    return out;
}

// The list with room for one more of its count, or null, which it frees.
static char** grown(char** list, size_t count, size_t* room) {
    if (count < *room) {
        return list;
    }

    char** more = realloc(list, (*room = *room ? 2 * *room : 16) * sizeof *list);

    if (!more) {
        for (size_t i = 0; i < count; i++) {
            free(list[i]);
        }

        free(list);
    }

    return more;
}

// Takes path as one found, freeing it where there is no room.
static void found(struct Glob* g, char* path) {
    if (!path || !(g->found = grown(g->found, g->count, &g->room))) {
        free(path);
        g->error = GLOB_NOSPACE;
        g->count = g->room = 0;

        return;
    }

    g->found[g->count++] = path;
}

static int magic(const struct Glob* g, const char* s, size_t n) {
    for (size_t i = 0; i < n; i++) {
        if (s[i] == '\\' && !(g->flags & GLOB_NOESCAPE)) {
            i++;
        } else if (s[i] == '*' || s[i] == '?' || s[i] == '[') {
            return 1;
        }
    }

    return 0;
}

// The component of n bytes as a name, its escapes gone.
static char* literal(const struct Glob* g, const char* s, size_t n) {
    char* out = malloc(n + 1);
    size_t k = 0;

    for (size_t i = 0; out && i < n; i++) {
        if (s[i] == '\\' && !(g->flags & GLOB_NOESCAPE) && i + 1 < n) {
            i++;
        }

        out[k++] = s[i];
    }

    if (out) {
        out[k] = 0;
    }

    return out;
}

static int exists(const char* path) {
    struct stat st;

    return lstat(path, &st) == 0;
}

// Whether the walk goes on after a directory that cannot be read.
static int failed(struct Glob* g, const char* dir, int e) {
    size_t n = strlen(dir);
    char* path = n > 1 && dir[n - 1] == '/' ? joined(dir, n - 1, "") : NULL;

    if (g->errfunc && g->errfunc(path ? path : n ? dir : ".", e)) {
        g->error = GLOB_ABORTED;
    }

    free(path);

    if (g->flags & GLOB_ERR) {
        g->error = GLOB_ABORTED;
    }

    return !g->error;
}

// The names of a directory, "." and ".." among them, sorted, or null of
// one that cannot be read, with errno.
static char** names(const char* dir, size_t* count) {
    DIR* d = opendir(*dir ? dir : ".");
    char** list = NULL;
    size_t n = 0, room = 0;

    if (!d) {
        return NULL;
    }

    for (struct dirent* e; (e = readdir(d));) {
        char* name = strdup(e->d_name);

        if (!name || !(list = grown(list, n, &room))) {
            free(name);
            closedir(d);
            errno = ENOMEM;

            return NULL;
        }

        list[n++] = name;
    }

    closedir(d);
    qsort(list, n, sizeof *list, compare);
    *count = n;

    return list ? list : calloc(1, sizeof *list);
}

static void walk(struct Glob* g, const char* dir, const char* pattern) {
    size_t n = strcspn(pattern, "/");
    const char* rest = pattern[n] ? pattern + n + 1 : NULL;

    if (g->error) {
        return;
    }

    if (n == 0) {
        char* next = joined(dir, strlen(dir), rest ? "/" : ".");

        if (!next) {
            g->error = GLOB_NOSPACE;
        } else if (rest) {
            walk(g, next, rest);
        } else if (exists(next)) {
            found(g, strdup(dir));
        }

        free(next);

        return;
    }

    if (!magic(g, pattern, n)) {
        char* name = literal(g, pattern, n);
        char* path = name ? joined(dir, strlen(dir), name) : NULL;
        char* next = path && rest ? joined(path, strlen(path), "/") : NULL;

        if (!path || (rest && !next)) {
            g->error = GLOB_NOSPACE;
        } else if (rest) {
            walk(g, next, rest);
        } else if (exists(path)) {
            found(g, path);
            path = NULL;
        }

        free(next);
        free(path);
        free(name);

        return;
    }

    char* component = joined(pattern, n, "");
    size_t count = 0;
    char** list = component ? names(dir, &count) : NULL;
    int flags = (g->flags & GLOB_PERIOD ? 0 : FNM_PERIOD) | (g->flags & GLOB_NOESCAPE ? FNM_NOESCAPE : 0);

    if (!component || (!list && errno == ENOMEM)) {
        g->error = GLOB_NOSPACE;
    } else if (!list && errno != ENOENT && errno != ENOTDIR) {
        failed(g, dir, errno);
    }

    for (size_t i = 0; i < count; i++) {
        const char* name = list[i];

        if (!g->error && (component[0] == '.' || (strcmp(name, ".") != 0 && strcmp(name, "..") != 0)) &&
            fnmatch(component, name, flags) == 0) {
            char* path = joined(dir, strlen(dir), name);

            if (!path) {
                g->error = GLOB_NOSPACE;
            } else if (rest) {
                char* next = joined(path, strlen(path), "/");

                if (next) {
                    walk(g, next, rest);
                } else {
                    g->error = GLOB_NOSPACE;
                }

                free(next);
                free(path);
            } else {
                found(g, path);
            }
        }

        free(list[i]);
    }

    free(list);
    free(component);
}

// The pattern of its ~ the home directory, on the heap, or null: of no
// memory, with *error GLOB_NOSPACE, or of no home with GLOB_TILDE_CHECK.
static char* tilde(const char* pattern, int flags, int* error) {
    if (!(flags & (GLOB_TILDE | GLOB_TILDE_CHECK)) || pattern[0] != '~') {
        char* copy = strdup(pattern);

        *error = copy ? 0 : GLOB_NOSPACE;

        return copy;
    }

    size_t n = strcspn(pattern + 1, "/");
    const char* home = NULL;
    int saved = errno;

    if (n == 0) {
        home = getenv("HOME");
    } else {
        char* user = strndup(pattern + 1, n);
        struct passwd* pw = user ? getpwnam(user) : NULL;

        home = pw ? pw->pw_dir : NULL;
        free(user);
    }

    errno = saved;

    if (!home || !*home) {
        char* copy = flags & GLOB_TILDE_CHECK ? NULL : strdup(pattern);

        *error = flags & GLOB_TILDE_CHECK ? GLOB_NOMATCH : copy ? 0 : GLOB_NOSPACE;

        return copy;
    }

    size_t h = strlen(home);
    const char* rest = pattern + 1 + n;

    if (*rest == '/' && h && home[h - 1] == '/') {
        h--;
    }

    char* out = joined(home, h, rest);

    *error = out ? 0 : GLOB_NOSPACE;

    return out;
}

int __kernel_glob(const char* restrict pattern, int flags, int (*errfunc)(const char*, int), glob_t* restrict pglob, size_t size) {
    sized("glob", size, sizeof(glob_t));

    struct Glob g = {flags, errfunc, NULL, 0, 0, 0};
    size_t offs = flags & GLOB_DOOFFS ? pglob->gl_offs : 0;
    int error = 0;

    if (!(flags & GLOB_APPEND)) {
        pglob->gl_pathc = 0;
        pglob->gl_pathv = NULL;
        pglob->gl_offs = offs;
    }

    char* expanded = tilde(pattern, flags, &error);

    if (!expanded || !*expanded) {
        free(expanded);

        return error ? error : GLOB_NOMATCH;
    }

    walk(&g, expanded[0] == '/' ? "/" : "", expanded + (expanded[0] == '/'));
    free(expanded);

    if (!g.error && !g.count && flags & GLOB_NOCHECK) {
        found(&g, strdup(pattern));
    }

    if (!g.error && !g.count) {
        g.error = GLOB_NOMATCH;
    }

    if (!g.error && !(flags & GLOB_NOSORT)) {
        qsort(g.found, g.count, sizeof *g.found, compare);
    }

    for (size_t i = 0; !g.error && flags & GLOB_MARK && i < g.count; i++) {
        struct stat st;
        size_t n = strlen(g.found[i]);

        if (stat(g.found[i], &st) == 0 && S_ISDIR(st.st_mode) && g.found[i][n - 1] != '/') {
            char* marked = joined(g.found[i], n, "/");

            if (!marked) {
                g.error = GLOB_NOSPACE;
            } else {
                free(g.found[i]);
                g.found[i] = marked;
            }
        }
    }

    size_t count = pglob->gl_pathc;
    char** pathv = g.error ? NULL : malloc((offs + count + g.count + 1) * sizeof *pathv);

    if (!g.error && !pathv) {
        g.error = GLOB_NOSPACE;
    }

    if (g.error) {
        for (size_t i = 0; i < g.count; i++) {
            free(g.found[i]);
        }

        free(g.found);

        return g.error;
    }

    memset(pathv, 0, offs * sizeof *pathv);

    if (pglob->gl_pathv) {
        memcpy(pathv + offs, pglob->gl_pathv + offs, count * sizeof *pathv);
    }

    memcpy(pathv + offs + count, g.found, g.count * sizeof *pathv);
    pathv[offs + count + g.count] = NULL;
    free(g.found);
    free(pglob->gl_pathv);
    pglob->gl_pathc = count + g.count;
    pglob->gl_pathv = pathv;

    return 0;
}

void __kernel_globfree(glob_t* pglob, size_t size) {
    sized("globfree", size, sizeof(glob_t));

    for (size_t i = 0; pglob->gl_pathv && i < pglob->gl_pathc; i++) {
        free(pglob->gl_pathv[pglob->gl_offs + i]);
    }

    free(pglob->gl_pathv);
    pglob->gl_pathc = 0;
    pglob->gl_pathv = NULL;
}
