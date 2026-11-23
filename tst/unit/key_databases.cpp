//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "key databases: ok\n"

// ndbm, POSIX's: a database of keys and contents in one file, the name's
// with .db after it. DBM_INSERT keeps a key's content, 1, where
// DBM_REPLACE replaces it; dbm_fetch gives a content or a null dptr;
// dbm_delete of a key there is not is negative; dbm_firstkey and
// dbm_nextkey visit each key once, and a key deleted as they go does not
// stop them; keys and contents are bytes, an empty content a pointer.
// The database stays when it is closed, a read-only open of it refuses
// changes (EPERM), O_EXCL an existing one, and a file that is no database
// is EINVAL, one shorter than the header too, but for an empty one. Opens
// of it, of this process and of a child of fork, see one another's
// changes; an open for writing rewrites it where most of it is of what is
// gone, and an open of before reads on, there is none in musl.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <ndbm.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/wait.h>
#include <unistd.h>

static datum text(const char* s) {
    return datum{const_cast<char*>(s), strlen(s)};
}

static bool holds(DBM* db, const char* key, const char* content) {
    datum got = dbm_fetch(db, text(key));
    return content ? got.dptr && got.dsize == strlen(content) && memcmp(got.dptr, content, got.dsize) == 0 : !got.dptr;
}

static off_t sizeOf(const char* path) {
    struct stat st;
    return stat(path, &st) == 0 ? st.st_size : -1;
}

int main() {
    char work[] = "dbm-XXXXXX";
    assert(mkdtemp(work) == work && chdir(work) == 0);
    DBM* db = dbm_open("words", O_RDWR | O_CREAT, 0644);
    assert(db && sizeOf("words.db") > 0 && dbm_error(db) == 0);
    assert(dbm_store(db, text("one"), text("first"), DBM_INSERT) == 0);
    assert(dbm_store(db, text("one"), text("second"), DBM_INSERT) == 1 && holds(db, "one", "first"));
    assert(dbm_store(db, text("one"), text("third"), DBM_REPLACE) == 0 && holds(db, "one", "third"));
    assert(dbm_store(db, text("two"), text("2"), DBM_INSERT) == 0 && holds(db, "missing", nullptr));
    const char bytes[] = {'a', 0, 'b'};
    assert(dbm_store(db, datum{const_cast<char*>(bytes), 3}, text(""), DBM_INSERT) == 0);
    datum empty = dbm_fetch(db, datum{const_cast<char*>(bytes), 3});
    assert(empty.dptr && empty.dsize == 0);
    assert(dbm_store(db, text("x"), text("y"), 7) < 0);

    int keys = 0;
    for (datum k = dbm_firstkey(db); k.dptr; k = dbm_nextkey(db)) {
        ++keys;
        assert(dbm_fetch(db, k).dptr);
    }
    assert(keys == 3);
    assert(dbm_delete(db, text("two")) == 0 && dbm_delete(db, text("two")) < 0 && holds(db, "two", nullptr));

    // Another open sees the changes, and a child's.
    DBM* other = dbm_open("words", O_RDONLY, 0);
    assert(other && holds(other, "one", "third"));
    assert(dbm_store(db, text("three"), text("3"), DBM_INSERT) == 0 && holds(other, "three", "3"));
    errno = 0;
    assert(dbm_store(other, text("four"), text("4"), DBM_INSERT) < 0 && errno == EPERM);
    errno = 0;
    assert(dbm_delete(other, text("one")) < 0 && errno == EPERM);
    const pid_t child = fork();
    assert(child >= 0);
    if (child == 0) {
        _exit(dbm_store(db, text("child"), text("was here"), DBM_INSERT) == 0 && holds(db, "one", "third") ? 0 : 1);
    }
    int status;
    assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    assert(holds(db, "child", "was here") && holds(other, "child", "was here"));
    dbm_close(other);
    dbm_close(db);

    db = dbm_open("words", O_RDONLY, 0);
    assert(db && holds(db, "one", "third") && holds(db, "three", "3") && holds(db, "two", nullptr));
    dbm_close(db);
    errno = 0;
    assert(!dbm_open("words", O_RDWR | O_CREAT | O_EXCL, 0644) && errno == EEXIST);
    const int fd = open("junk.db", O_WRONLY | O_CREAT, 0644);
    assert(fd >= 0 && write(fd, "not a database at all", 21) == 21 && close(fd) == 0);
    errno = 0;
    assert(!dbm_open("junk", O_RDONLY, 0) && errno == EINVAL);
    // shorter than the header: an open's making of one is of its bytes
    const int short_fd = open("spam.db", O_WRONLY | O_CREAT, 0644);
    assert(short_fd >= 0 && write(short_fd, "spam", 4) == 4 && close(short_fd) == 0);
    errno = 0;
    assert(!dbm_open("spam", O_RDONLY, 0) && errno == EINVAL);
    const int empty_fd = open("empty.db", O_WRONLY | O_CREAT, 0644);
    assert(empty_fd >= 0 && close(empty_fd) == 0);
    db = dbm_open("empty", O_RDONLY, 0);
    assert(db && dbm_firstkey(db).dptr == nullptr);
    dbm_close(db);

    // Most of it gone: the next open for writing rewrites it.
    db = dbm_open("words", O_WRONLY, 0);
    assert(db);
    static char big[50000];
    memset(big, 'z', sizeof big);
    for (int i = 0; i < 8; ++i) {
        big[0] = char('a' + i);
        assert(dbm_store(db, text("big"), datum{big, sizeof big}, DBM_REPLACE) == 0);
    }
    other = dbm_open("words", O_RDONLY, 0);
    assert(other);
    const off_t before = sizeOf("words.db");
    dbm_close(db);
    db = dbm_open("words", O_RDWR, 0);
    assert(db && sizeOf("words.db") < before / 4);
    datum got = dbm_fetch(db, text("big"));
    assert(got.dsize == sizeof big && static_cast<char*>(got.dptr)[0] == 'h' && holds(db, "child", "was here"));
    assert(dbm_store(db, text("after"), text("the rewrite"), DBM_INSERT) == 0 && holds(other, "after", "the rewrite"));
    keys = 0;
    for (datum k = dbm_firstkey(db); k.dptr; k = dbm_nextkey(db)) {
        assert(dbm_delete(db, k) == 0);
        ++keys;
    }
    assert(keys == 6 && !dbm_firstkey(db).dptr && !dbm_firstkey(other).dptr);
    assert(dbm_clearerr(db) == 0 && dbm_error(db) == 0);
    dbm_close(other);
    dbm_close(db);
    printf("key databases: ok\n");
}
