/*
 * What lib/c's stdio has that Bionic's has not, over Bionic's FILE
 * (libc/stdio/local.h):
 * - fopencookie, glibc's and FreeBSD's stream of functions, over funopen;
 * - getw and putw, as SUSv2 had them;
 * - stdio_ext.h's __freadptr and __freadptrinc, by which gnulib reads a
 *   FILE it cannot see into;
 * - the _unlocked forms of the wide functions, glibc's: the functions,
 *   whose lock is nothing in a process of one thread.
 */
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdio_ext.h>
#include <stdlib.h>
#include <wchar.h>

#include "local.h"

namespace {

struct Cookie {
    void* cookie;
    cookie_io_functions_t io;
};

// A read of no function is the end of the file.
int cookieRead(void* at, char* buf, int n) {
    Cookie* c = static_cast<Cookie*>(at);

    return c->io.read ? c->io.read(c->cookie, buf, n) : 0;
}

// A write of no function is discarded, as written.
int cookieWrite(void* at, const char* buf, int n) {
    Cookie* c = static_cast<Cookie*>(at);

    return c->io.write ? c->io.write(c->cookie, buf, n) : n;
}

fpos_t cookieSeek(void* at, fpos_t offset, int whence) {
    Cookie* c = static_cast<Cookie*>(at);
    off_t position = offset;

    if (!c->io.seek) {
        errno = ESPIPE;

        return -1;
    }

    return c->io.seek(c->cookie, &position, whence) == 0 ? position : -1;
}

int cookieClose(void* at) {
    Cookie* c = static_cast<Cookie*>(at);
    int result = c->io.close ? c->io.close(c->cookie) : 0;

    free(c);

    return result;
}

}

FILE* fopencookie(void* cookie, const char* mode, cookie_io_functions_t io) {
    int modeFlags;
    int flags = __sflags(mode, &modeFlags);

    if (flags == 0) {
        return nullptr;
    }

    Cookie* c = static_cast<Cookie*>(malloc(sizeof(Cookie)));

    if (!c) {
        return nullptr;
    }

    c->cookie = cookie;
    c->io = io;

    FILE* fp = funopen(c, cookieRead, cookieWrite, cookieSeek, cookieClose);

    if (!fp) {
        free(c);

        return nullptr;
    }

    // What the mode lets the stream do, not what it has functions for.
    fp->_flags = flags;

    if (modeFlags & O_APPEND) {
        fseeko(fp, 0, SEEK_END);
    }

    return fp;
}

int getw(FILE* fp) {
    int w;

    return fread(&w, sizeof w, 1, fp) == 1 ? w : EOF;
}

int putw(int w, FILE* fp) {
    return fwrite(&w, sizeof w, 1, fp) == 1 ? 0 : EOF;
}

// What a stream reading has buffered and not yet given: none but where it
// is reading.
const char* __freadptr(FILE* fp, size_t* size) {
    if (!(fp->_flags & __SRD) || fp->_r <= 0) {
        return nullptr;
    }

    *size = fp->_r;

    return reinterpret_cast<const char*>(fp->_p);
}

void __freadptrinc(FILE* fp, size_t n) {
    fp->_p += n;
    fp->_r -= n;
}

wint_t fgetwc_unlocked(FILE* fp) {
    return __fgetwc_unlock(fp);
}

wint_t getwc_unlocked(FILE* fp) {
    return __fgetwc_unlock(fp);
}

wint_t getwchar_unlocked(void) {
    return __fgetwc_unlock(stdin);
}

wint_t fputwc_unlocked(wchar_t wc, FILE* fp) {
    return __fputwc_unlock(wc, fp);
}

wint_t putwc_unlocked(wchar_t wc, FILE* fp) {
    return __fputwc_unlock(wc, fp);
}

wint_t putwchar_unlocked(wchar_t wc) {
    return __fputwc_unlock(wc, stdout);
}

wchar_t* fgetws_unlocked(wchar_t* __restrict s, int n, FILE* __restrict fp) {
    return fgetws(s, n, fp);
}

int fputws_unlocked(const wchar_t* __restrict s, FILE* __restrict fp) {
    return fputws(s, fp);
}
