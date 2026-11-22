/*
 * What lib/c's stdio has that FreeBSD's has not, over FreeBSD's FILE
 * (stdio.h, local.h):
 * - flockfile, ftrylockfile and funlockfile: a process of lib/c's has one
 *   thread, which owns every stream;
 * - fgetc_unlocked and fgets_unlocked, and the _unlocked forms of the wide
 *   functions, glibc's: the functions, whose lock is nothing;
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

void flockfile(FILE* fp) {
    (void)fp;
}

int ftrylockfile(FILE* fp) {
    (void)fp;

    return 0;
}

void funlockfile(FILE* fp) {
    (void)fp;
}

int fgetc_unlocked(FILE* fp) {
    return getc_unlocked(fp);
}

char* fgets_unlocked(char* s, int n, FILE* fp) {
    return fgets(s, n, fp);
}

wint_t fgetwc_unlocked(FILE* fp) {
    return fgetwc(fp);
}

wint_t getwc_unlocked(FILE* fp) {
    return fgetwc(fp);
}

wint_t getwchar_unlocked(void) {
    return fgetwc(stdin);
}

wint_t fputwc_unlocked(wchar_t wc, FILE* fp) {
    return fputwc(wc, fp);
}

wint_t putwc_unlocked(wchar_t wc, FILE* fp) {
    return fputwc(wc, fp);
}

wint_t putwchar_unlocked(wchar_t wc) {
    return fputwc(wc, stdout);
}

wchar_t* fgetws_unlocked(wchar_t* restrict s, int n, FILE* restrict fp) {
    return fgetws(s, n, fp);
}

int fputws_unlocked(const wchar_t* restrict s, FILE* restrict fp) {
    return fputws(s, fp);
}

static int flushLineBuffered(FILE* fp) {
    return fp->_flags & __SLBF ? __sflush(fp) : 0;
}

void _flushlbf(void) {
    _fwalk(flushLineBuffered);
}

int __fsetlocking(FILE* fp, int type) {
    int was = fp->_flags2 & CALLER_LOCKS ? FSETLOCKING_BYCALLER : FSETLOCKING_INTERNAL;

    if (type == FSETLOCKING_BYCALLER) {
        fp->_flags2 |= CALLER_LOCKS;
    } else if (type == FSETLOCKING_INTERNAL) {
        fp->_flags2 &= ~CALLER_LOCKS;
    }

    return was;
}

int __fwriting(FILE* fp) {
    return (fp->_flags & __SWR) != 0;
}

int __freading(FILE* fp) {
    return (fp->_flags & __SRD) != 0;
}

int __freadable(FILE* fp) {
    return (fp->_flags & (__SRD | __SRW)) != 0;
}

int __fwritable(FILE* fp) {
    return (fp->_flags & (__SWR | __SRW)) != 0;
}

int __flbf(FILE* fp) {
    return (fp->_flags & __SLBF) != 0;
}

size_t __fbufsize(FILE* fp) {
    return fp->_bf._size;
}

size_t __fpending(FILE* fp) {
    return fp->_flags & __SWR && fp->_p ? (size_t)(fp->_p - fp->_bf._base) : 0;
}

int __fpurge(FILE* fp) {
    return fpurge(fp);
}

// What a stream reading has buffered, what ungetc pushed back with it.
size_t __freadahead(FILE* fp) {
    if (fp->_flags & __SWR || fp->_r < 0) {
        return 0;
    }

    return fp->_r + (HASUB(fp) ? fp->_ur : 0);
}

const char* __freadptr(FILE* fp, size_t* size) {
    if (!(fp->_flags & __SRD) || fp->_r <= 0) {
        return NULL;
    }

    *size = fp->_r;

    return (const char*)fp->_p;
}

void __freadptrinc(FILE* fp, size_t n) {
    fp->_p += n;
    fp->_r -= n;
}

void __fseterr(FILE* fp) {
    fp->_flags |= __SERR;
}
