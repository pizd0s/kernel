/*
 * The user and group databases, /etc/passwd and /etc/group, and the
 * shells of /etc/shells, read whole at every call. A line of the
 * databases has its fields between colons, seven of a user's, four of a
 * group's, and its ids are decimal; one of another count of fields or of
 * no ids is passed over. A group's members are between commas. A line of
 * /etc/shells is a shell but for the empty ones and comments; where it
 * cannot be read, the shells are /bin/sh and /bin/csh.
 *
 * getpwnam and the rest give the first entry that matches; getpwent and
 * getgrent go on from the last one they gave, set and end start again. A
 * result is of the heap, the one before it freed: ENOMEM where there is
 * no room; none found leaves errno and the one before as they were. The _r functions write it into the buffer, ERANGE where it is
 * too small, and pass on an error of reading the file.
 */
#include <ctype.h>
#include <errno.h>
#include <fcntl.h>
#include <grp.h>
#include <pwd.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#define LARGEST (16 << 20)

// A file's text on the heap, NUL-ended, what of it can be read; null and
// errno where it cannot be opened, or with no room for it.
static char* readAll(const char* path) {
    int fd = open(path, O_RDONLY | O_CLOEXEC);

    if (fd < 0) {
        return NULL;
    }

    char* text = NULL;
    size_t size = 0, room = 0;
    int e = 0;

    while (size < LARGEST) {
        if (size + 1 >= room) {
            char* more = realloc(text, room = room ? 2 * room : 4096);

            if (!more) {
                e = ENOMEM;

                break;
            }

            text = more;
        }

        ssize_t n = read(fd, text + size, room - size - 1);

        if (n < 0 && errno == EINTR) {
            continue;
        }

        if (n <= 0) {
            break;
        }

        size += n;
    }

    close(fd);

    if (e) {
        free(text);
        errno = e;

        return NULL;
    }

    text[size] = 0;

    return text;
}

// The line at *at, ended, and *at past it; null after the last.
static char* nextLine(char** at) {
    char* line = *at;

    if (!line) {
        return NULL;
    }

    char* end = strchr(line, '\n');

    *at = end ? end + 1 : NULL;

    if (end) {
        *end = 0;
    }

    return line;
}

// Whether the line has n fields between colons, each ended in place.
static int fields(char* line, char** field, int n) {
    for (int i = 0; i < n; i++) {
        field[i] = line;
        line = strchr(line, ':');

        if (!line) {
            return i == n - 1;
        }

        *line++ = 0;
    }

    return 0;
}

static int decimal(const char* s, uint32_t* id) {
    uint64_t v = 0;

    if (!*s) {
        return 0;
    }

    for (; *s; s++) {
        if (*s < '0' || *s > '9' || (v = v * 10 + (*s - '0')) > UINT32_MAX) {
            return 0;
        }
    }

    *id = v;

    return 1;
}

struct User {
    char* field[7];
    uint32_t uid;
    uint32_t gid;
};

struct Group {
    char* field[4];
    uint32_t gid;
};

// The next user of the text, or null.
static int nextUser(char** at, struct User* u) {
    for (char* line; (line = nextLine(at));) {
        if (fields(line, u->field, 7) && decimal(u->field[2], &u->uid) && decimal(u->field[3], &u->gid)) {
            return 1;
        }
    }

    return 0;
}

static int nextGroup(char** at, struct Group* g) {
    for (char* line; (line = nextLine(at));) {
        if (fields(line, g->field, 4) && decimal(g->field[2], &g->gid)) {
            return 1;
        }
    }

    return 0;
}

static int members(const struct Group* g) {
    int n = *g->field[3] != 0;

    for (const char* c = g->field[3]; *c; c++) {
        n += *c == ',';
    }

    return n;
}

// Whether the name is one of the group's members.
static int member(const struct Group* g, const char* name) {
    size_t n = strlen(name);

    for (const char* m = g->field[3]; *g->field[3];) {
        size_t length = strcspn(m, ",");

        if (length == n && memcmp(m, name, n) == 0) {
            return 1;
        }

        if (!m[length]) {
            return 0;
        }

        m += length + 1;
    }

    return 0;
}

enum Match { byName, byId, next };

// The text of a database and its entry that matches, or the one after
// those before; 0, or ENOENT where there is none, or the error of reading.
struct Found {
    char* text;
    int e;
};

static struct Found findUser(enum Match match, const char* name, uint32_t id, size_t* index, struct User* u) {
    int saved = errno;
    struct Found f = {readAll("/etc/passwd"), 0};
    char* at = f.text;
    size_t i = 0;

    if (!f.text) {
        f.e = errno == ENOMEM ? ENOMEM : ENOENT;
        errno = saved;

        return f;
    }

    while (nextUser(&at, u)) {
        if (match == byName ? strcmp(u->field[0], name) == 0 : match == byId ? u->uid == id : i++ == *index) {
            if (match == next) {
                ++*index;
            }

            return f;
        }
    }

    f.e = ENOENT;

    return f;
}

static struct Found findGroup(enum Match match, const char* name, uint32_t id, size_t* index, struct Group* g) {
    int saved = errno;
    struct Found f = {readAll("/etc/group"), 0};
    char* at = f.text;
    size_t i = 0;

    if (!f.text) {
        f.e = errno == ENOMEM ? ENOMEM : ENOENT;
        errno = saved;

        return f;
    }

    while (nextGroup(&at, g)) {
        if (match == byName ? strcmp(g->field[0], name) == 0 : match == byId ? g->gid == id : i++ == *index) {
            if (match == next) {
                ++*index;
            }

            return f;
        }
    }

    f.e = ENOENT;

    return f;
}

static size_t userSize(const struct User* u) {
    size_t n = 0;

    for (int i = 0; i < 7; i++) {
        n += i == 2 || i == 3 ? 0 : strlen(u->field[i]) + 1;
    }

    return n;
}

static void putUser(struct passwd* pw, char* buf, const struct User* u) {
    char* s[7];

    for (int i = 0; i < 7; i++) {
        if (i != 2 && i != 3) {
            s[i] = buf;
            buf = stpcpy(buf, u->field[i]) + 1;
        }
    }

    *pw = (struct passwd){s[0], s[1], u->uid, u->gid, s[4], s[5], s[6]};
}

static size_t padding(uintptr_t at) {
    return (4 - at % 4) % 4;
}

static size_t groupSize(const struct Group* g, uintptr_t align) {
    return padding(align) + sizeof(char*) * (members(g) + 1) + strlen(g->field[0]) + strlen(g->field[1]) + 2 + strlen(g->field[3]) +
           (*g->field[3] ? 1 : 0);
}

static void putGroup(struct group* gr, char* buf, const struct Group* g) {
    char** list = (char**)(buf + padding((uintptr_t)buf));
    int count = members(g);
    char* s = (char*)(list + count + 1);
    const char* m = g->field[3];

    gr->gr_name = s;
    s = stpcpy(s, g->field[0]) + 1;
    gr->gr_passwd = s;
    s = stpcpy(s, g->field[1]) + 1;
    gr->gr_gid = g->gid;
    gr->gr_mem = list;

    for (int i = 0; i < count; i++) {
        size_t length = strcspn(m, ",");

        list[i] = s;
        memcpy(s, m, length);
        s[length] = 0;
        s += length + 1;
        m += length + 1;
    }

    list[count] = NULL;
}

static struct passwd* userResult(struct Found f, const struct User* u) {
    static void* result;

    if (f.e) {
        free(f.text);

        if (f.e == ENOMEM) {
            errno = ENOMEM;
        }

        return NULL;
    }

    free(result);

    result = malloc(sizeof(struct passwd) + userSize(u));

    if (result) {
        putUser(result, (char*)result + sizeof(struct passwd), u);
    } else {
        errno = ENOMEM;
    }

    free(f.text);

    return result;
}

static struct group* groupResult(struct Found f, const struct Group* g) {
    static void* result;

    if (f.e) {
        free(f.text);

        if (f.e == ENOMEM) {
            errno = ENOMEM;
        }

        return NULL;
    }

    free(result);

    result = malloc(sizeof(struct group) + groupSize(g, sizeof(struct group)));

    if (result) {
        putGroup(result, (char*)result + sizeof(struct group), g);
    } else {
        errno = ENOMEM;
    }

    free(f.text);

    return result;
}

static int userInto(struct Found f, const struct User* u, struct passwd* pw, char* buf, size_t size, struct passwd** out) {
    int e = f.e == ENOENT ? 0 : f.e;

    *out = NULL;

    if (!f.e && userSize(u) > size) {
        e = ERANGE;
    } else if (!f.e) {
        putUser(pw, buf, u);
        *out = pw;
    }

    free(f.text);

    return e;
}

static int groupInto(struct Found f, const struct Group* g, struct group* gr, char* buf, size_t size, struct group** out) {
    int e = f.e == ENOENT ? 0 : f.e;

    *out = NULL;

    if (!f.e && groupSize(g, (uintptr_t)buf) > size) {
        e = ERANGE;
    } else if (!f.e) {
        putGroup(gr, buf, g);
        *out = gr;
    }

    free(f.text);

    return e;
}

static size_t userIndex, groupIndex;

struct passwd* __kernel_getpwnam(const char* name, size_t size) {
    struct User u;

    return userResult(findUser(byName, name, 0, NULL, &u), &u);
}

struct passwd* __kernel_getpwuid(uid_t uid, size_t size) {
    struct User u;

    return userResult(findUser(byId, NULL, uid, NULL, &u), &u);
}

struct passwd* __kernel_getpwent(size_t size) {
    struct User u;

    return userResult(findUser(next, NULL, 0, &userIndex, &u), &u);
}

void __kernel_setpwent(void) {
    userIndex = 0;
}

void __kernel_endpwent(void) {
    userIndex = 0;
}

int __kernel_getpwnam_r(const char* name, struct passwd* pw, size_t pwSize, char* buf, size_t size, struct passwd** out, size_t outSize) {
    struct User u;

    return userInto(findUser(byName, name, 0, NULL, &u), &u, pw, buf, size, out);
}

int __kernel_getpwuid_r(uid_t uid, struct passwd* pw, size_t pwSize, char* buf, size_t size, struct passwd** out, size_t outSize) {
    struct User u;

    return userInto(findUser(byId, NULL, uid, NULL, &u), &u, pw, buf, size, out);
}

struct group* __kernel_getgrnam(const char* name, size_t size) {
    struct Group g;

    return groupResult(findGroup(byName, name, 0, NULL, &g), &g);
}

struct group* __kernel_getgrgid(gid_t gid, size_t size) {
    struct Group g;

    return groupResult(findGroup(byId, NULL, gid, NULL, &g), &g);
}

struct group* __kernel_getgrent(size_t size) {
    struct Group g;

    return groupResult(findGroup(next, NULL, 0, &groupIndex, &g), &g);
}

void __kernel_setgrent(void) {
    groupIndex = 0;
}

void __kernel_endgrent(void) {
    groupIndex = 0;
}

int __kernel_getgrnam_r(const char* name, struct group* gr, size_t grSize, char* buf, size_t size, struct group** out, size_t outSize) {
    struct Group g;

    return groupInto(findGroup(byName, name, 0, NULL, &g), &g, gr, buf, size, out);
}

int __kernel_getgrgid_r(gid_t gid, struct group* gr, size_t grSize, char* buf, size_t size, struct group** out, size_t outSize) {
    struct Group g;

    return groupInto(findGroup(byId, NULL, gid, NULL, &g), &g, gr, buf, size, out);
}

// The user's groups, the one given first, each once, on the heap, and
// how many; null where there is no room.
static gid_t* groupsOf(const char* user, gid_t first, int* count) {
    char* text = readAll("/etc/group");
    char* at = text;
    int n = 1, room = 8;
    gid_t* list = malloc(room * sizeof *list);
    struct Group g;

    if (!list) {
        free(text);
        errno = ENOMEM;

        return NULL;
    }

    list[0] = first;

    while (text && nextGroup(&at, &g)) {
        int seen = 0;

        for (int i = 0; i < n; i++) {
            seen |= list[i] == g.gid;
        }

        if (seen || !member(&g, user)) {
            continue;
        }

        if (n == room) {
            gid_t* more = realloc(list, (room *= 2) * sizeof *list);

            if (!more) {
                free(list);
                free(text);
                errno = ENOMEM;

                return NULL;
            }

            list = more;
        }

        list[n++] = g.gid;
    }

    free(text);
    *count = n;

    return list;
}

int __kernel_getgrouplist(const char* user, gid_t group, gid_t* groups, int* count) {
    int n;
    gid_t* list = groupsOf(user, group, &n);

    if (!list) {
        return -1;
    }

    int room = *count < n ? *count : n;

    for (int i = 0; i < room; i++) {
        groups[i] = list[i];
    }

    free(list);
    *count = n;

    return room < n ? -1 : n;
}

int __kernel_initgroups(const char* user, gid_t group) {
    int n;
    gid_t* list = groupsOf(user, group, &n);

    if (!list) {
        return -1;
    }

    int result = setgroups(n, list);
    int e = errno;

    free(list);
    errno = e;

    return result;
}

static size_t shellIndex;

char* __kernel_getusershell(void) {
    static const char fallback[] = "/bin/sh\n/bin/csh\n";
    static char* result;
    int saved = errno;
    char* text = readAll("/etc/shells");

    errno = text ? errno : saved;

    char* at = text ? text : strdup(fallback);
    char* start = at;
    size_t i = 0;

    if (!at) {
        errno = ENOMEM;

        return NULL;
    }

    char* found = NULL;

    for (char* line; (line = nextLine(&at));) {
        while (isspace((unsigned char)*line)) {
            line++;
        }

        size_t n = strlen(line);

        while (n > 0 && isspace((unsigned char)line[n - 1])) {
            n--;
        }

        if (n == 0 || line[0] == '#' || i++ != shellIndex) {
            continue;
        }

        shellIndex++;
        found = malloc(n + 1);

        if (found) {
            memcpy(found, line, n);
            found[n] = 0;
            free(result);
            result = found;
        } else {
            errno = ENOMEM;
        }

        break;
    }

    free(start);

    return found;
}

void __kernel_setusershell(void) {
    shellIndex = 0;
}

void __kernel_endusershell(void) {
    shellIndex = 0;
}
