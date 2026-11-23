/*
 * What lib/c's stdio has that FreeBSD's has not, over FreeBSD's FILE
 * (stdio.h, local.h):
 * - flockfile, ftrylockfile and funlockfile: a process of lib/c's has one
 *   thread, which owns every stream;
 * - fgetc_unlocked and fgets_unlocked, glibc's: the functions, whose lock
 *   is nothing;
 * - stdio_ext.h's functions, Solaris' and glibc's, gnulib's way into a
 *   FILE it cannot see into among them (__freadahead, __freadptr).
 */
#include "namespace.h"

#include <stdio.h>
#include <stdio_ext.h>
#include <stdlib.h>
#include <wchar.h>

#include "libc_private.h"
#include "local.h"
#include "un-namespace.h"

// __fsetlocking's bit of _flags2: the caller locks the stream.
#define CALLER_LOCKS 0x8000

// The standard streams, which stdio.h's macros call for.
FILE* __kernel___stdin(void) {
    return stdin;
}

FILE* __kernel___stdout(void) {
    return stdout;
}

FILE* __kernel___stderr(void) {
    return stderr;
}

void __kernel_flockfile(FILE* fp) {
    (void)fp;
}

int __kernel_ftrylockfile(FILE* fp) {
    (void)fp;

    return 0;
}

void __kernel_funlockfile(FILE* fp) {
    (void)fp;
}

int __kernel_fgetc_unlocked(FILE* fp) {
    return getc_unlocked(fp);
}

char* __kernel_fgets_unlocked(char* s, int n, FILE* fp) {
    return fgets(s, n, fp);
}

static int flushLineBuffered(FILE* fp) {
    return fp->_flags & __SLBF ? __sflush(fp) : 0;
}

void __kernel__flushlbf(void) {
    _fwalk(flushLineBuffered);
}

int __kernel___fsetlocking(FILE* fp, int type) {
    int was = fp->_flags2 & CALLER_LOCKS ? FSETLOCKING_BYCALLER : FSETLOCKING_INTERNAL;

    if (type == FSETLOCKING_BYCALLER) {
        fp->_flags2 |= CALLER_LOCKS;
    } else if (type == FSETLOCKING_INTERNAL) {
        fp->_flags2 &= ~CALLER_LOCKS;
    }

    return was;
}

int __kernel___fwriting(FILE* fp) {
    return (fp->_flags & __SWR) != 0;
}

int __kernel___freading(FILE* fp) {
    return (fp->_flags & __SRD) != 0;
}

int __kernel___freadable(FILE* fp) {
    return (fp->_flags & (__SRD | __SRW)) != 0;
}

int __kernel___fwritable(FILE* fp) {
    return (fp->_flags & (__SWR | __SRW)) != 0;
}

int __kernel___flbf(FILE* fp) {
    return (fp->_flags & __SLBF) != 0;
}

size_t __kernel___fbufsize(FILE* fp) {
    return fp->_bf._size;
}

size_t __kernel___fpending(FILE* fp) {
    return fp->_flags & __SWR && fp->_p ? (size_t)(fp->_p - fp->_bf._base) : 0;
}

int __kernel___fpurge(FILE* fp) {
    return fpurge(fp);
}

// What a stream reading has buffered, what ungetc pushed back with it.
size_t __kernel___freadahead(FILE* fp) {
    if (fp->_flags & __SWR || fp->_r < 0) {
        return 0;
    }

    return fp->_r + (HASUB(fp) ? fp->_ur : 0);
}

const char* __kernel___freadptr(FILE* fp, size_t* size) {
    if (!(fp->_flags & __SRD) || fp->_r <= 0) {
        return NULL;
    }

    *size = fp->_r;

    return (const char*)fp->_p;
}

void __kernel___freadptrinc(FILE* fp, size_t n) {
    fp->_p += n;
    fp->_r -= n;
}

void __kernel___fseterr(FILE* fp) {
    fp->_flags |= __SERR;
}
