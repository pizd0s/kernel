/*
 * ndbm: a database of keys and their contents in one file, name.db, a log
 * of its changes that every call reads on from where it was, so that the
 * opens of it, of any process, see one another's changes. The log is 16
 * bytes of header, "PZDBM1\n" and zeros, then records: a key's length and
 * its content's, little-endian (0xffffffff a deletion of the key), and
 * their bytes. An open for writing rewrites the file when most of it is of
 * what is gone. A DBM keeps each key's newest content's place, the keys in
 * the order of their newest records.
 */
#include <errno.h>
#include <fcntl.h>
#include <ndbm.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

#define MAGIC "PZDBM1\n"
#define HEADER 16
#define DELETED 0xffffffffu
#define LARGEST (1u << 26)
#define COMPACT (1 << 16)

struct Entry {
    struct Entry* chain;
    struct Entry* prev;
    struct Entry* next;
    off_t content;
    uint32_t size;
    uint32_t hash;
    size_t length;
    unsigned char key[];
};

struct __dbm {
    int fd;
    int writable;
    int failed;
    char* path;
    dev_t dev;
    ino_t ino;
    off_t scanned;
    off_t dead;
    struct Entry** buckets;
    size_t mask;
    size_t count;
    struct Entry* first;
    struct Entry* last;
    // dbm_firstkey's keys, each its length and bytes, and where
    // dbm_nextkey is in them.
    unsigned char* keys;
    size_t keysSize;
    size_t at;
    void* key;
    size_t keyRoom;
    void* value;
    size_t valueRoom;
};

DBM* __kernel_dbm_open(const char*, int, mode_t);
void __kernel_dbm_close(DBM*);
int __kernel_dbm_error(DBM*);
int __kernel_dbm_clearerr(DBM*);
int __kernel_dbm_store(DBM*, const void*, size_t, const void*, size_t, int);
int __kernel_dbm_delete(DBM*, const void*, size_t);
void* __kernel_dbm_fetch(DBM*, const void*, size_t, size_t*);
void* __kernel_dbm_firstkey(DBM*, size_t*);
void* __kernel_dbm_nextkey(DBM*, size_t*);

static uint32_t get32(const unsigned char* p) {
    return p[0] | p[1] << 8 | p[2] << 16 | (uint32_t)p[3] << 24;
}

static void put32(unsigned char* p, uint32_t v) {
    p[0] = v;
    p[1] = v >> 8;
    p[2] = v >> 16;
    p[3] = v >> 24;
}

static uint32_t hashOf(const unsigned char* key, size_t length) {
    uint32_t h = 2166136261u;

    for (size_t i = 0; i < length; i++) {
        h = (h ^ key[i]) * 16777619u;
    }

    return h;
}

static struct Entry** slotOf(DBM* d, const unsigned char* key, size_t length, uint32_t h) {
    struct Entry** link = &d->buckets[h & d->mask];

    for (; *link; link = &(*link)->chain) {
        if ((*link)->hash == h && (*link)->length == length && memcmp((*link)->key, key, length) == 0) {
            break;
        }
    }

    return link;
}

static struct Entry* find(DBM* d, const void* key, size_t length) {
    return d->buckets ? *slotOf(d, key, length, hashOf(key, length)) : NULL;
}

static void unlinkEntry(DBM* d, struct Entry* e) {
    if (e->prev) {
        e->prev->next = e->next;
    } else {
        d->first = e->next;
    }

    if (e->next) {
        e->next->prev = e->prev;
    } else {
        d->last = e->prev;
    }
}

static void appendEntry(DBM* d, struct Entry* e) {
    e->prev = d->last;
    e->next = NULL;

    if (d->last) {
        d->last->next = e;
    } else {
        d->first = e;
    }

    d->last = e;
}

static int grow(DBM* d) {
    size_t size = d->buckets ? 2 * (d->mask + 1) : 64;
    struct Entry** buckets = calloc(size, sizeof *buckets);

    if (!buckets) {
        return -1;
    }

    for (struct Entry* e = d->first; e; e = e->next) {
        e->chain = buckets[e->hash & (size - 1)];
        buckets[e->hash & (size - 1)] = e;
    }

    free(d->buckets);
    d->buckets = buckets;
    d->mask = size - 1;

    return 0;
}

// put makes a key's content size bytes at content, of the newest record.
static int put(DBM* d, const unsigned char* key, size_t length, off_t content, uint32_t size) {
    if ((!d->buckets || d->count > d->mask) && grow(d) != 0) {
        return -1;
    }

    uint32_t h = hashOf(key, length);
    struct Entry** link = slotOf(d, key, length, h);
    struct Entry* e = *link;

    if (e) {
        d->dead += 8 + length + e->size;
        unlinkEntry(d, e);
    } else {
        e = malloc(sizeof *e + length);

        if (!e) {
            return -1;
        }

        memcpy(e->key, key, length);
        e->length = length;
        e->hash = h;
        e->chain = NULL;
        *link = e;
        d->count++;
    }

    e->content = content;
    e->size = size;
    appendEntry(d, e);

    return 0;
}

static void drop(DBM* d, const unsigned char* key, size_t length) {
    if (!d->buckets) {
        return;
    }

    struct Entry** link = slotOf(d, key, length, hashOf(key, length));
    struct Entry* e = *link;

    if (e) {
        d->dead += 8 + length + e->size;
        *link = e->chain;
        unlinkEntry(d, e);
        free(e);
        d->count--;
    }
}

static void reset(DBM* d) {
    for (struct Entry *e = d->first, *next; e; e = next) {
        next = e->next;
        free(e);
    }

    free(d->buckets);
    free(d->keys);
    d->buckets = NULL;
    d->mask = d->count = 0;
    d->first = d->last = NULL;
    d->scanned = d->dead = 0;
    d->keys = NULL;
    d->keysSize = d->at = 0;
}

// apply takes the whole records of n bytes of the log from base, how many
// bytes they are in *used.
static int apply(DBM* d, const unsigned char* b, size_t n, off_t base, size_t* used) {
    size_t at = 0;

    while (at + 8 <= n) {
        uint32_t length = get32(b + at), size = get32(b + at + 4);

        if (length > LARGEST || (size != DELETED && size > LARGEST)) {
            break;
        }

        size_t record = 8 + (size_t)length + (size == DELETED ? 0 : size);

        if (at + record > n) {
            break;
        }

        if (size == DELETED) {
            drop(d, b + at + 8, length);
            d->dead += record;
        } else if (put(d, b + at + 8, length, base + (off_t)(at + 8 + length), size) != 0) {
            *used = at;
            errno = ENOMEM;

            return -1;
        }

        at += record;
    }

    *used = at;

    return 0;
}

static int load(DBM* d);

static int reopen(DBM* d) {
    int fd = open(d->path, d->writable ? O_RDWR | O_APPEND | O_CLOEXEC : O_RDONLY | O_CLOEXEC);

    if (fd < 0) {
        return -1;
    }

    close(d->fd);
    d->fd = fd;

    return load(d);
}

// update reads the records written since it last did; a file replaced at
// the name it opens again, one shorter than it read from the start.
static int update(DBM* d) {
    struct stat st;

    if (stat(d->path, &st) == 0 && (st.st_dev != d->dev || st.st_ino != d->ino)) {
        return reopen(d);
    }

    if (fstat(d->fd, &st) != 0) {
        return -1;
    }

    if (st.st_size < d->scanned) {
        reset(d);
    }

    if (d->scanned == 0) {
        unsigned char head[HEADER];

        if (st.st_size < HEADER) {
            return 0;
        }

        if (pread(d->fd, head, HEADER, 0) != HEADER || memcmp(head, MAGIC, sizeof MAGIC) != 0) {
            errno = EINVAL;

            return -1;
        }

        d->scanned = HEADER;
    }

    if (st.st_size == d->scanned) {
        return 0;
    }

    size_t n = st.st_size - d->scanned, used = 0;
    unsigned char* tail = malloc(n);

    if (!tail) {
        errno = ENOMEM;

        return -1;
    }

    ssize_t got = pread(d->fd, tail, n, d->scanned);
    int result = got < 0 ? -1 : apply(d, tail, got, d->scanned, &used);
    int error = errno;

    d->scanned += used;
    free(tail);
    errno = error;

    return result;
}

static int load(DBM* d) {
    struct stat st;

    if (fstat(d->fd, &st) != 0) {
        return -1;
    }

    d->dev = st.st_dev;
    d->ino = st.st_ino;
    reset(d);

    if (st.st_size == 0 && d->writable) {
        unsigned char head[HEADER] = MAGIC;

        if (write(d->fd, head, HEADER) < 0) {
            return -1;
        }
    }

    return update(d);
}

static int failed(DBM* d) {
    d->failed = 1;

    return -1;
}

static int readAt(DBM* d, void* to, size_t n, off_t at) {
    ssize_t got = pread(d->fd, to, n, at);

    if (got != (ssize_t)n) {
        if (got >= 0) {
            errno = EIO;
        }

        return -1;
    }

    return 0;
}

static int writeRecord(DBM* d, const void* key, size_t length, const void* content, size_t size, int deleted) {
    size_t total = 8 + length + size;
    unsigned char* b = malloc(total);

    if (!b) {
        errno = ENOMEM;

        return -1;
    }

    put32(b, length);
    put32(b + 4, deleted ? DELETED : size);
    memcpy(b + 8, key, length);

    if (size) {
        memcpy(b + 8 + length, content, size);
    }

    ssize_t n = write(d->fd, b, total);
    int error = n < 0 ? errno : EIO;

    free(b);

    if (n != (ssize_t)total) {
        errno = error;

        return -1;
    }

    return update(d);
}

static char* temporaryName(const char* path) {
    char digits[16];
    int n = 0;

    for (unsigned pid = getpid(); n == 0 || pid; pid /= 10) {
        digits[n++] = '0' + pid % 10;
    }

    size_t length = strlen(path);
    char* name = malloc(length + 5 + n + 1);

    if (name) {
        memcpy(name, path, length);
        memcpy(name + length, ".tmp.", 5);

        for (int i = 0; i < n; i++) {
            name[length + 5 + i] = digits[n - 1 - i];
        }

        name[length + 5 + n] = 0;
    }

    return name;
}

// compact rewrites the file, when most of it is of what is gone, with the
// keys' newest contents alone, through a file beside it.
static int compact(DBM* d) {
    if (d->scanned < COMPACT || 2 * d->dead < d->scanned) {
        return 0;
    }

    struct stat st;

    if (fstat(d->fd, &st) != 0) {
        return -1;
    }

    size_t total = HEADER;

    for (struct Entry* e = d->first; e; e = e->next) {
        total += 8 + e->length + e->size;
    }

    unsigned char* b = calloc(1, total);
    char* name = temporaryName(d->path);

    if (!b || !name) {
        free(b);
        free(name);
        errno = ENOMEM;

        return -1;
    }

    memcpy(b, MAGIC, sizeof MAGIC);

    size_t at = HEADER;

    for (struct Entry* e = d->first; e; e = e->next) {
        put32(b + at, e->length);
        put32(b + at + 4, e->size);
        memcpy(b + at + 8, e->key, e->length);

        if (readAt(d, b + at + 8 + e->length, e->size, e->content) != 0) {
            free(b);
            free(name);

            return -1;
        }

        at += 8 + e->length + e->size;
    }

    int fd = open(name, O_WRONLY | O_CREAT | O_EXCL | O_CLOEXEC, st.st_mode & 07777);
    int result = -1;

    if (fd >= 0) {
        ssize_t n = write(fd, b, total);
        int error = n < 0 ? errno : EIO;

        close(fd);

        if (n == (ssize_t)total && rename(name, d->path) == 0) {
            result = 0;
        } else {
            error = n == (ssize_t)total ? errno : error;
            unlink(name);
            errno = error;
        }
    }

    free(b);
    free(name);

    return result == 0 ? reopen(d) : -1;
}

// pathOf is file's name with .db after it, from the root: the name an
// update checks whether the file is still there.
static char* pathOf(const char* file) {
    size_t length = strlen(file);
    char* cwd = NULL;

    if (file[0] != '/') {
        cwd = malloc(4096);

        if (cwd && !getcwd(cwd, 4096)) {
            free(cwd);
            cwd = NULL;
        }
    }

    size_t base = cwd ? strlen(cwd) : 0;

    if (base && cwd[base - 1] == '/') {
        base--;
    }

    char* path = malloc(base + 1 + length + 4);

    if (path) {
        char* p = path;

        if (cwd) {
            memcpy(p, cwd, base);
            p += base;
            *p++ = '/';
        }

        memcpy(p, file, length);
        memcpy(p + length, ".db", 4);
    }

    free(cwd);

    return path;
}

static void release(DBM* d) {
    reset(d);
    free(d->key);
    free(d->value);
    free(d->path);
    free(d);
}

DBM* __kernel_dbm_open(const char* file, int flags, mode_t mode) {
    DBM* d = calloc(1, sizeof *d);

    if (!d || !(d->path = pathOf(file))) {
        free(d);
        errno = ENOMEM;

        return NULL;
    }

    d->writable = (flags & O_ACCMODE) != O_RDONLY;

    int how = (flags & ~O_ACCMODE) | O_CLOEXEC;

    if (d->writable) {
        how |= O_RDWR | O_APPEND;
    }

    d->fd = open(d->path, how, mode);

    if (d->fd < 0 || load(d) != 0) {
        int error = errno;

        if (d->fd >= 0) {
            close(d->fd);
        }

        release(d);
        errno = error;

        return NULL;
    }

    if (d->writable) {
        compact(d);
    }

    return d;
}

void __kernel_dbm_close(DBM* d) {
    if (d) {
        close(d->fd);
        release(d);
    }
}

int __kernel_dbm_error(DBM* d) {
    if (!d) {
        errno = EBADF;

        return -1;
    }

    return d->failed;
}

int __kernel_dbm_clearerr(DBM* d) {
    if (!d) {
        errno = EBADF;

        return -1;
    }

    d->failed = 0;

    return 0;
}

int __kernel_dbm_store(DBM* d, const void* key, size_t length, const void* content, size_t size, int mode) {
    if (!d) {
        errno = EBADF;

        return -1;
    }

    if ((mode != DBM_INSERT && mode != DBM_REPLACE) || length > LARGEST || size > LARGEST) {
        errno = EINVAL;

        return -1;
    }

    if (!d->writable) {
        errno = EPERM;

        return -1;
    }

    if (update(d) != 0) {
        return failed(d);
    }

    if (mode == DBM_INSERT && find(d, key, length)) {
        return 1;
    }

    return writeRecord(d, key, length, content, size, 0) != 0 ? failed(d) : 0;
}

int __kernel_dbm_delete(DBM* d, const void* key, size_t length) {
    if (!d) {
        errno = EBADF;

        return -1;
    }

    if (!d->writable) {
        errno = EPERM;

        return -1;
    }

    if (update(d) != 0) {
        return failed(d);
    }

    if (!find(d, key, length)) {
        return -1;
    }

    return writeRecord(d, key, length, NULL, 0, 1) != 0 ? failed(d) : 0;
}

// room makes *at a buffer of more than n bytes, which a datum of n bytes,
// none among them, points into.
static void* room(void** at, size_t* size, size_t n) {
    if (*size < n + 1) {
        free(*at);
        *size = (n + 16) & ~(size_t)15;
        *at = malloc(*size);

        if (!*at) {
            *size = 0;
            errno = ENOMEM;
        }
    }

    return *at;
}

void* __kernel_dbm_fetch(DBM* d, const void* key, size_t length, size_t* size) {
    *size = 0;

    if (!d) {
        errno = EBADF;

        return NULL;
    }

    if (update(d) != 0) {
        failed(d);

        return NULL;
    }

    struct Entry* e = find(d, key, length);

    if (!e) {
        return NULL;
    }

    if (!room(&d->value, &d->valueRoom, e->size) || readAt(d, d->value, e->size, e->content) != 0) {
        failed(d);

        return NULL;
    }

    *size = e->size;

    return d->value;
}

static void* nextKey(DBM* d, size_t* size) {
    while (d->at < d->keysSize) {
        const unsigned char* p = d->keys + d->at;
        uint32_t length = get32(p);

        d->at += 4 + length;

        if (!find(d, p + 4, length)) {
            continue;
        }

        if (!room(&d->key, &d->keyRoom, length)) {
            failed(d);

            return NULL;
        }

        memcpy(d->key, p + 4, length);
        *size = length;

        return d->key;
    }

    return NULL;
}

void* __kernel_dbm_firstkey(DBM* d, size_t* size) {
    *size = 0;

    if (!d) {
        errno = EBADF;

        return NULL;
    }

    if (update(d) != 0) {
        failed(d);

        return NULL;
    }

    size_t total = 0;

    for (struct Entry* e = d->first; e; e = e->next) {
        total += 4 + e->length;
    }

    free(d->keys);
    d->keys = NULL;
    d->keysSize = d->at = 0;

    if (total && !(d->keys = malloc(total))) {
        errno = ENOMEM;
        failed(d);

        return NULL;
    }

    unsigned char* p = d->keys;

    for (struct Entry* e = d->first; e; e = e->next) {
        put32(p, e->length);
        memcpy(p + 4, e->key, e->length);
        p += 4 + e->length;
    }

    d->keysSize = total;

    return nextKey(d, size);
}

void* __kernel_dbm_nextkey(DBM* d, size_t* size) {
    *size = 0;

    if (!d) {
        errno = EBADF;

        return NULL;
    }

    if (update(d) != 0) {
        failed(d);

        return NULL;
    }

    return nextKey(d, size);
}
