#pragma once

#include <__base.h>
#include <sys/types.h>

__BEGIN_DECLS

/*
 * ndbm, POSIX's, the kernel's: a database of keys and their contents,
 * byte strings, in one file, the name's with .db after it. The file is a
 * log of the database's changes, each a key and its content or the key's
 * deletion, which an open of it reads as other processes write it; one
 * open for writing rewrites it where more than half is of what is gone.
 * dbm_fetch's content and dbm_firstkey's and dbm_nextkey's key are the
 * DBM's until its next call of the kind or dbm_close. dbm_open's
 * descriptor is a descriptor of the process's, FD_CLOEXEC.
 */
typedef struct {
    void* dptr;
    size_t dsize;
} datum;

typedef struct __dbm DBM;

#define DBM_INSERT 0
#define DBM_REPLACE 1

int dbm_clearerr(DBM*);
void dbm_close(DBM*);
int dbm_delete(DBM*, datum);
int dbm_error(DBM*);
datum dbm_fetch(DBM*, datum);
datum dbm_firstkey(DBM*);
datum dbm_nextkey(DBM*);
DBM* dbm_open(const char*, int, mode_t);
int dbm_store(DBM*, datum, datum, int);

__END_DECLS
