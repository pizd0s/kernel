#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <ftw.h>
#include <limits.h>
#include <pthread.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

/*
 * nftw, POSIX's: a walk of the tree under a path, calling the program's
 * function for each name in it. At most fd_limit directories are open at
 * once, their descriptors FD_CLOEXEC: to open one more, the walk reads the
 * rest of the highest open one into memory and closes it, so the limit
 * never cuts the tree short. FTW_CHDIR makes a directory the working one
 * while the names in it are reported, its parent while it is, and puts
 * the caller's back at the end. FTW_MOUNT reports nothing of another
 * device; FTW_XDEV reports a directory of another device but nothing in
 * it. Without FTW_PHYS links are followed and a directory met again under
 * itself is not entered, nor, with FTW_DEPTH, reported.
 */
typedef struct Level {
    struct Level* up;
    dev_t dev;
    ino_t ino;
    size_t length;
    DIR* dir;
    char* names;
    size_t next;
    size_t used;
} Level;

typedef struct {
    int (*fn)(const char*, const struct stat*, int, struct FTW*);
    int flags;
    int limit;
    int open;
    dev_t device;
    int home;
    Level* deepest;
    char path[PATH_MAX + 1];
} Walk;

// The rest of a directory's names, read into memory, its stream closed.
static int keep(Walk* walk, Level* level) {
    size_t capacity = level->used;

    for (struct dirent* entry; (errno = 0, entry = readdir(level->dir));) {
        size_t n = strlen(entry->d_name) + 1;

        if (level->used + n > capacity) {
            capacity = 2 * (level->used + n);
            char* grown = realloc(level->names, capacity);

            if (!grown) {
                return -1;
            }

            level->names = grown;
        }

        memcpy(level->names + level->used, entry->d_name, n);
        level->used += n;
    }

    if (errno) {
        return -1;
    }

    closedir(level->dir);
    level->dir = 0;
    walk->open--;

    return 0;
}

// A slot for one more open directory, freed at the highest open one.
static int room(Walk* walk) {
    if (walk->open < walk->limit) {
        return 0;
    }

    Level* highest = 0;

    for (Level* level = walk->deepest; level; level = level->up) {
        if (level->dir) {
            highest = level;
        }
    }

    return highest ? keep(walk, highest) : 0;
}

// The next name of a directory, 0 at its end; errno says why, if not 0.
static const char* next(Level* level) {
    if (level->dir) {
        struct dirent* entry;

        errno = 0;
        entry = readdir(level->dir);

        return entry ? entry->d_name : 0;
    }

    if (level->next >= level->used) {
        errno = 0;

        return 0;
    }

    const char* name = level->names + level->next;
    level->next += strlen(name) + 1;

    return name;
}

// Back to the directory a name of which was just reported: the open
// stream's, or the path from the caller's.
static int back(Walk* walk, Level* up) {
    if (!up) {
        return fchdir(walk->home);
    }

    if (up->dir) {
        return fchdir(dirfd(up->dir));
    }

    char saved = walk->path[up->length];
    walk->path[up->length] = 0;
    int r = walk->path[0] == '/' ? 0 : fchdir(walk->home);

    if (r == 0) {
        r = chdir(walk->path);
    }

    walk->path[up->length] = saved;

    return r;
}

static int visit(Walk* walk, size_t length, int depth, int base, Level* up) {
    const char* name = (walk->flags & FTW_CHDIR) && up ? walk->path + base : walk->path;
    int follow = !(walk->flags & FTW_PHYS);
    struct stat st;
    int type;

    if ((follow ? stat(name, &st) : lstat(name, &st)) < 0) {
        if (errno == EACCES) {
            type = FTW_NS;
        } else if (follow && lstat(name, &st) == 0 && S_ISLNK(st.st_mode)) {
            type = FTW_SLN;
        } else {
            return -1;
        }
    } else if (S_ISDIR(st.st_mode)) {
        type = FTW_D;
    } else if (S_ISLNK(st.st_mode)) {
        type = FTW_SL;
    } else {
        type = FTW_F;
    }

    if (!up) {
        walk->device = st.st_dev;
    } else if ((walk->flags & FTW_MOUNT) && type != FTW_NS && st.st_dev != walk->device) {
        return 0;
    }

    struct FTW ftw = {base, depth};

    if (type != FTW_D) {
        return walk->fn(walk->path, &st, type, &ftw);
    }

    for (Level* level = up; level; level = level->up) {
        if (level->dev == st.st_dev && level->ino == st.st_ino) {
            return walk->flags & FTW_DEPTH ? 0 : walk->fn(walk->path, &st, FTW_D, &ftw);
        }
    }

    int enter = !((walk->flags & FTW_XDEV) && st.st_dev != walk->device);
    Level level = {up, st.st_dev, st.st_ino, length, 0, 0, 0, 0};

    if (enter) {
        if (room(walk) < 0) {
            return -1;
        }

        int fd = open(name, O_RDONLY | O_DIRECTORY | O_CLOEXEC);

        if (fd < 0) {
            if (errno != EACCES) {
                return -1;
            }

            type = FTW_DNR;
            enter = 0;
        } else if (!(level.dir = fdopendir(fd))) {
            close(fd);

            return -1;
        } else {
            walk->open++;
        }
    }

    int r = 0;

    if (!(walk->flags & FTW_DEPTH)) {
        r = walk->fn(walk->path, &st, type, &ftw);
    }

    if (enter && r == 0) {
        Level* deepest = walk->deepest;
        size_t joint = length && walk->path[length - 1] == '/' ? length - 1 : length;

        walk->deepest = &level;

        if ((walk->flags & FTW_CHDIR) && fchdir(dirfd(level.dir)) < 0) {
            r = -1;
        }

        for (const char* entry; r == 0 && (entry = next(&level));) {
            if (entry[0] == '.' && (!entry[1] || (entry[1] == '.' && !entry[2]))) {
                continue;
            }

            size_t n = strlen(entry);

            if (joint + 1 + n > PATH_MAX) {
                errno = ENAMETOOLONG;
                r = -1;
                break;
            }

            walk->path[joint] = '/';
            memcpy(walk->path + joint + 1, entry, n + 1);
            r = visit(walk, joint + 1 + n, depth + 1, (int)joint + 1, &level);
        }

        if (r == 0 && errno) {
            r = -1;
        }

        walk->path[length] = 0;
        walk->deepest = deepest;

        if (level.dir) {
            closedir(level.dir);
            walk->open--;
        }

        free(level.names);

        if ((walk->flags & FTW_CHDIR) && back(walk, up) < 0 && r == 0) {
            r = -1;
        }
    }

    if (r == 0 && (walk->flags & FTW_DEPTH)) {
        r = walk->fn(walk->path, &st, type == FTW_D ? FTW_DP : type, &ftw);
    }

    return r;
}

int nftw(const char* path, int (*fn)(const char*, const struct stat*, int, struct FTW*), int fd_limit,
         int flags) {
    size_t length = strlen(path);

    if (length > PATH_MAX) {
        errno = ENAMETOOLONG;

        return -1;
    }

    Walk* walk = malloc(sizeof *walk);

    if (!walk) {
        return -1;
    }

    *walk = (Walk){fn, flags, fd_limit < 1 ? 1 : fd_limit, 0, 0, -1, 0, {0}};
    memcpy(walk->path, path, length + 1);

    if ((flags & FTW_CHDIR) && (walk->home = open(".", O_RDONLY | O_DIRECTORY | O_CLOEXEC)) < 0) {
        free(walk);

        return -1;
    }

    size_t end = length;

    while (end > 1 && path[end - 1] == '/') {
        end--;
    }

    size_t base = end;

    while (base > 0 && path[base - 1] != '/') {
        base--;
    }

    if (base == end) {
        base = 0;
    }

    int state;
    pthread_setcancelstate(PTHREAD_CANCEL_DISABLE, &state);

    int r = visit(walk, length, 0, (int)base, 0);
    int saved = errno;

    if (walk->home >= 0) {
        fchdir(walk->home);
        close(walk->home);
    }

    free(walk);
    pthread_setcancelstate(state, 0);
    errno = saved;

    return r;
}

/*
 * ftw, POSIX.1-2017's: nftw with FTW_PHYS, the stat of a name as lstat()
 * gives it, which ftw allows, by way of a function of nftw's kind that
 * calls the program's with three arguments: musl's passed the program's
 * function to nftw as if it took four, which wasm traps on. The function
 * is kept for the walk and the caller's back after it, so a walk in a
 * walk keeps its own.
 */
static int (*walker)(const char*, const struct stat*, int);

static int step(const char* path, const struct stat* st, int type, struct FTW* ftw) {
    return walker(path, st, type);
}

int ftw(const char* path, int (*fn)(const char*, const struct stat*, int), int ndirs) {
    int (*outer)(const char*, const struct stat*, int) = walker;

    walker = fn;
    int r = nftw(path, step, ndirs, FTW_PHYS);
    walker = outer;

    return r;
}
