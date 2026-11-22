#pragma once

/*
 * lib/c's stdio.h for FreeBSD's stdio, with what FreeBSD's stdio.h gives
 * its sources: the FILE structure, FreeBSD's, whose lock is lib/c's mutex
 * type and unused (a process has one thread), its flags, and the inline
 * reads and writes of a stream's buffer.
 */
#include <pthread.h>
#include <sys/types.h>
#include <wchar.h>

#include_next <stdio.h>

struct __sbuf {
    unsigned char* _base;
    int _size;
};

struct __sFILE {
    unsigned char* _p;
    int _r;
    int _w;
    short _flags;
    short _file;
    struct __sbuf _bf;
    int _lbfsize;

    void* _cookie;
    int (*_close)(void*);
    int (*_read)(void*, char*, int);
    fpos_t (*_seek)(void*, fpos_t, int);
    int (*_write)(void*, const char*, int);

    struct __sbuf _ub;
    unsigned char* _up;
    int _ur;

    unsigned char _ubuf[3];
    unsigned char _nbuf[1];

    struct __sbuf _lb;

    int _blksize;
    fpos_t _offset;

    pthread_mutex_t _fl_mutex;
    void* _fl_owner;
    int _fl_count;
    int _orientation;
    mbstate_t _mbstate;
    int _flags2;
};

#define __SLBF 0x0001
#define __SNBF 0x0002
#define __SRD 0x0004
#define __SWR 0x0008
#define __SRW 0x0010
#define __SEOF 0x0020
#define __SERR 0x0040
#define __SMBF 0x0080
#define __SAPP 0x0100
#define __SSTR 0x0200
#define __SOPT 0x0400
#define __SNPT 0x0800
#define __SOFF 0x1000
#define __SMOD 0x2000
#define __SALC 0x4000
#define __SIGN 0x8000

#define __S2OAP 0x0001

__BEGIN_DECLS

int __srget(FILE*);
int __swbuf(int, FILE*);

__END_DECLS

#define __sgetc(p) (--(p)->_r < 0 ? __srget(p) : (int)(*(p)->_p++))

static __inline int __sputc(int _c, FILE* _p) {
    if (--_p->_w >= 0 || (_p->_w >= _p->_lbfsize && (char)_c != '\n')) {
        return (*_p->_p++ = _c);
    }

    return __swbuf(_c, _p);
}

#define __sfeof(p) (((p)->_flags & __SEOF) != 0)
#define __sferror(p) (((p)->_flags & __SERR) != 0)
#define __sclearerr(p) ((void)((p)->_flags &= ~(__SERR | __SEOF)))
#define __sfileno(p) ((p)->_file)
