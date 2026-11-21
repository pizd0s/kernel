// What F-Stack's stack wants of its host, which DPDK and Linux are to it
// there: memory, the clocks, randomness, the environment, its log, its
// configuration, and the network card, here the device vm gives the
// system (vm run --net), a SOCK_SEQPACKET end whose packets are Ethernet
// frames. The stack is FreeBSD's, run on one thread: its card is one
// queue, so every check of the queues a flow goes to passes, and no flow
// is tracked across a reload.

#include <errno.h>
#include <stdarg.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

#include "ff_api.h"
#include "ff_config.h"
#include "ff_dpdk_if.h"
#include "ff_errno.h"
#include "ff_flow_map.h"
#include "ff_flow_peer.h"
#include "ff_host_interface.h"
#include "ff_log.h"
#include "ff_veth.h"

#include "host.h"

struct ff_config ff_global_cfg;

void panic(const char* format, ...) __attribute__((noreturn));

// The card of the stack's one interface.
struct ff_dpdk_if_context {
    void* sc;
    void* ifp;
    uint16_t mtu;
};

static int card = -1;
static struct ff_dpdk_if_context* interface;
static int64_t nowSec;
static long nowNsec;

void inetCard(int fd) {
    card = fd;
}

// ---- memory -----------------------------------------------------------------------

void* ff_mmap(void* addr, uint64_t len, int prot, int flags, int fd, uint64_t offset) {
    (void)addr;
    (void)prot;
    (void)flags;
    (void)fd;
    (void)offset;
    void* p = aligned_alloc(4096, (size_t)((len + 4095) & ~4095ull));
    if (!p) {
        return ff_MAP_FAILED;
    }
    memset(p, 0, (size_t)len);
    return p;
}

int ff_munmap(void* addr, uint64_t len) {
    (void)len;
    free(addr);
    return 0;
}

void* ff_malloc(uint64_t size) {
    return malloc((size_t)size);
}

void* ff_calloc(uint64_t number, uint64_t size) {
    return calloc((size_t)number, (size_t)size);
}

void* ff_realloc(void* p, uint64_t size) {
    return realloc(p, (size_t)size);
}

void ff_free(void* p) {
    free(p);
}

// ---- the clocks -------------------------------------------------------------------

void ff_clock_gettime(int id, int64_t* sec, long* nsec) {
    struct timespec ts;
    clock_gettime(id == ff_CLOCK_REALTIME ? CLOCK_REALTIME : CLOCK_MONOTONIC, &ts);
    *sec = ts.tv_sec;
    *nsec = ts.tv_nsec;
}

uint64_t ff_clock_gettime_ns(int id) {
    int64_t sec;
    long nsec;
    ff_clock_gettime(id, &sec, &nsec);
    return (uint64_t)sec * ff_NSEC_PER_SEC + (uint64_t)nsec;
}

uint64_t ff_get_tsc_ns(void) {
    return ff_clock_gettime_ns(ff_CLOCK_MONOTONIC);
}

void ff_update_current_ts(void) {
    ff_clock_gettime(ff_CLOCK_REALTIME, &nowSec, &nowNsec);
}

void ff_get_current_time(int64_t* sec, long* nsec) {
    if (nowSec == 0) {
        ff_update_current_ts();
    }
    *sec = nowSec;
    *nsec = nowNsec;
}

// ---- randomness, the environment, errors, the log ---------------------------------------

void ff_arc4rand(void* ptr, unsigned int len, int reseed) {
    (void)reseed;
    for (unsigned int at = 0; at < len; at += 256) {
        const unsigned int n = len - at < 256 ? len - at : 256;
        if (getentropy((char*)ptr + at, n) != 0) {
            panic("inet: getentropy: errno %d", errno);
        }
    }
}

uint32_t ff_arc4random(void) {
    uint32_t r;
    ff_arc4rand(&r, sizeof r, 0);
    return r;
}

int ff_setenv(const char* name, const char* value) {
    return setenv(name, value, 1);
}

char* ff_getenv(const char* name) {
    return getenv(name);
}

// The errno of FreeBSD's the stack sets, as lib/c's, as F-Stack's host
// interface translates it to Linux's.
void ff_os_errno(int error) {
    switch (error) {
        case ff_EPERM:       errno = EPERM; break;
        case ff_ENOENT:      errno = ENOENT; break;
        case ff_ESRCH:       errno = ESRCH; break;
        case ff_EINTR:       errno = EINTR; break;
        case ff_EIO:         errno = EIO; break;
        case ff_ENXIO:       errno = ENXIO; break;
        case ff_E2BIG:       errno = E2BIG; break;
        case ff_ENOEXEC:     errno = ENOEXEC; break;
        case ff_EBADF:       errno = EBADF; break;
        case ff_ECHILD:      errno = ECHILD; break;
        case ff_EDEADLK:     errno = EDEADLK; break;
        case ff_ENOMEM:      errno = ENOMEM; break;
        case ff_EACCES:      errno = EACCES; break;
        case ff_EFAULT:      errno = EFAULT; break;
        case ff_ENOTBLK:     errno = ENOTBLK; break;
        case ff_EBUSY:       errno = EBUSY; break;
        case ff_EEXIST:      errno = EEXIST; break;
        case ff_EXDEV:       errno = EXDEV; break;
        case ff_ENODEV:      errno = ENODEV; break;
        case ff_ENOTDIR:     errno = ENOTDIR; break;
        case ff_EISDIR:      errno = EISDIR; break;
        case ff_EINVAL:      errno = EINVAL; break;
        case ff_ENFILE:      errno = ENFILE; break;
        case ff_EMFILE:      errno = EMFILE; break;
        case ff_ENOTTY:      errno = ENOTTY; break;
        case ff_ETXTBSY:     errno = ETXTBSY; break;
        case ff_EFBIG:       errno = EFBIG; break;
        case ff_ENOSPC:      errno = ENOSPC; break;
        case ff_ESPIPE:      errno = ESPIPE; break;
        case ff_EROFS:       errno = EROFS; break;
        case ff_EMLINK:      errno = EMLINK; break;
        case ff_EPIPE:       errno = EPIPE; break;
        case ff_EDOM:        errno = EDOM; break;
        case ff_ERANGE:      errno = ERANGE; break;

        /* case ff_EAGAIN:       same as EWOULDBLOCK */
        case ff_EWOULDBLOCK:     errno = EWOULDBLOCK; break;

        case ff_EINPROGRESS:     errno = EINPROGRESS; break;
        case ff_EALREADY:        errno = EALREADY; break;
        case ff_ENOTSOCK:        errno = ENOTSOCK; break;
        case ff_EDESTADDRREQ:    errno = EDESTADDRREQ; break;
        case ff_EMSGSIZE:        errno = EMSGSIZE; break;
        case ff_EPROTOTYPE:      errno = EPROTOTYPE; break;
        case ff_ENOPROTOOPT:     errno = ENOPROTOOPT; break;
        case ff_EPROTONOSUPPORT: errno = EPROTONOSUPPORT; break;
        case ff_ESOCKTNOSUPPORT: errno = ESOCKTNOSUPPORT; break;

        /* case ff_EOPNOTSUPP:   same as ENOTSUP */
        case ff_ENOTSUP:         errno = ENOTSUP; break;

        case ff_EPFNOSUPPORT:    errno = EPFNOSUPPORT; break;
        case ff_EAFNOSUPPORT:    errno = EAFNOSUPPORT; break;
        case ff_EADDRINUSE:      errno = EADDRINUSE; break;
        case ff_EADDRNOTAVAIL:   errno = EADDRNOTAVAIL; break;
        case ff_ENETDOWN:        errno = ENETDOWN; break;
        case ff_ENETUNREACH:     errno = ENETUNREACH; break;
        case ff_ENETRESET:       errno = ENETRESET; break;
        case ff_ECONNABORTED:    errno = ECONNABORTED; break;
        case ff_ECONNRESET:      errno = ECONNRESET; break;
        case ff_ENOBUFS:         errno = ENOBUFS; break;
        case ff_EISCONN:         errno = EISCONN; break;
        case ff_ENOTCONN:        errno = ENOTCONN; break;
        case ff_ESHUTDOWN:       errno = ESHUTDOWN; break;
        case ff_ETOOMANYREFS:    errno = ETOOMANYREFS; break;
        case ff_ETIMEDOUT:       errno = ETIMEDOUT; break;
        case ff_ECONNREFUSED:    errno = ECONNREFUSED; break;
        case ff_ELOOP:           errno = ELOOP; break;
        case ff_ENAMETOOLONG:    errno = ENAMETOOLONG; break;
        case ff_EHOSTDOWN:       errno = EHOSTDOWN; break;
        case ff_EHOSTUNREACH:    errno = EHOSTUNREACH; break;
        case ff_ENOTEMPTY:       errno = ENOTEMPTY; break;
        case ff_EUSERS:      errno = EUSERS; break;
        case ff_EDQUOT:      errno = EDQUOT; break;
        case ff_ESTALE:      errno = ESTALE; break;
        case ff_EREMOTE:     errno = EREMOTE; break;
        case ff_ENOLCK:      errno = ENOLCK; break;
        case ff_ENOSYS:      errno = ENOSYS; break;
        case ff_EIDRM:       errno = EIDRM; break;
        case ff_ENOMSG:      errno = ENOMSG; break;
        case ff_EOVERFLOW:   errno = EOVERFLOW; break;
        case ff_ECANCELED:   errno = ECANCELED; break;
        case ff_EILSEQ:      errno = EILSEQ; break;
        case ff_EBADMSG:     errno = EBADMSG; break;
        case ff_EMULTIHOP:   errno = EMULTIHOP; break;
        case ff_ENOLINK:     errno = ENOLINK; break;
        case ff_EPROTO:      errno = EPROTO; break;
        default:              errno = error; break;
    }

}

int ff_log(uint32_t level, uint32_t logtype, const char* format, ...) {
    (void)logtype;
    if (level > FF_LOG_WARNING && !getenv("INET_LOG")) {
        return 0;
    }
    va_list ap;
    va_start(ap, format);
    const int n = vfprintf(stderr, format, ap);
    va_end(ap);
    return n;
}

void panic(const char* format, ...) {
    va_list ap;
    va_start(ap, format);
    fputs("inet: panic: ", stderr);
    vfprintf(stderr, format, ap);
    fputc('\n', stderr);
    va_end(ap);
    abort();
}

// ---- the queues of one card, the flows of no reload -------------------------------------

int ff_cur_proc_id(void) {
    return 0;
}

int ff_in_pcbladdr(uint16_t family, void* faddr, uint16_t fport, void* laddr) {
    (void)family;
    (void)faddr;
    (void)fport;
    (void)laddr;
    return 0;
}

int ff_rss_check(void* softc, uint32_t saddr, uint32_t daddr, uint16_t sport, uint16_t dport) {
    (void)softc;
    (void)saddr;
    (void)daddr;
    (void)sport;
    (void)dport;
    return 1;
}

int ff_rss_check6(void* softc, const uint8_t* saddr6, const uint8_t* daddr6, uint16_t sport, uint16_t dport) {
    (void)softc;
    (void)saddr6;
    (void)daddr6;
    (void)sport;
    (void)dport;
    return 1;
}

int ff_rss_adjust_sport(void* softc, uint32_t saddr, uint32_t daddr, uint16_t dport, uint16_t* out_sport, uint16_t first,
                        uint16_t last) {
    (void)softc;
    (void)saddr;
    (void)daddr;
    (void)dport;
    (void)out_sport;
    (void)first;
    (void)last;
    return -1;
}

int ff_rss_adjust_sport6(void* softc, const uint8_t* saddr6, const uint8_t* daddr6, uint16_t dport, uint16_t* out_sport,
                         uint16_t first, uint16_t last) {
    (void)softc;
    (void)saddr6;
    (void)daddr6;
    (void)dport;
    (void)out_sport;
    (void)first;
    (void)last;
    return -1;
}

int ff_rss_tbl_set_portrange(uint16_t first, uint16_t last) {
    (void)first;
    (void)last;
    return 0;
}

int ff_rss_tbl_get_portrange(uint32_t saddr, uint32_t daddr, uint16_t sport, uint16_t* rss_first, uint16_t* rss_last,
                             uint16_t** rss_portrange) {
    (void)saddr;
    (void)daddr;
    (void)sport;
    (void)rss_first;
    (void)rss_last;
    (void)rss_portrange;
    return -1;
}

int ff_rss_tbl6_set_portrange(uint16_t first, uint16_t last) {
    (void)first;
    (void)last;
    return 0;
}

int ff_rss_tbl6_get_portrange(const uint8_t* saddr6, const uint8_t* daddr6, uint16_t sport, uint16_t* rss_first,
                              uint16_t* rss_last, uint16_t** rss_portrange) {
    (void)saddr6;
    (void)daddr6;
    (void)sport;
    (void)rss_first;
    (void)rss_last;
    (void)rss_portrange;
    return -1;
}

int ff_flow_map_active(void) {
    return 0;
}

int ff_flow_map_admit(const struct ff_flow_key* key, int* created) {
    (void)key;
    if (created) {
        *created = 0;
    }
    return 1;
}

int ff_flow_map_lookup(const struct ff_flow_key* key) {
    (void)key;
    return 0;
}

int ff_flow_map_lport_ok(uint16_t lport) {
    (void)lport;
    return 1;
}

int ff_flow_map_revoke(const struct ff_flow_key* key) {
    (void)key;
    return 0;
}

void ff_flow_peer_note(const struct ff_flow_key* key) {
    (void)key;
}

int ff_flow_peer_recording(void) {
    return 0;
}

// ---- the card -----------------------------------------------------------------------

struct ff_dpdk_if_context* ff_dpdk_register_if(void* sc, void* ifp, struct ff_port_cfg* cfg) {
    struct ff_dpdk_if_context* ctx = calloc(1, sizeof *ctx);
    if (!ctx) {
        return NULL;
    }
    ctx->sc = sc;
    ctx->ifp = ifp;
    ctx->mtu = cfg->mtu;
    interface = ctx;
    return ctx;
}

void ff_dpdk_deregister_if(struct ff_dpdk_if_context* ctx) {
    if (ctx == interface) {
        interface = NULL;
    }
    free(ctx);
}

int ff_dpdk_if_get_mtu(struct ff_dpdk_if_context* ctx, uint16_t* mtu) {
    *mtu = ctx->mtu;
    return 0;
}

int ff_dpdk_if_set_mtu(struct ff_dpdk_if_context* ctx, uint16_t mtu) {
    ctx->mtu = mtu;
    return 0;
}

// A frame the stack sends: its mbuf's bytes, one packet on the card; a
// card with no room drops it, as a full ring of a card's does.
int ff_dpdk_if_send(struct ff_dpdk_if_context* ctx, void* m, int total) {
    static unsigned char frame[65536];
    (void)ctx;
    if (total <= 0 || total > (int)sizeof frame || ff_mbuf_copydata(m, frame, 0, total) != 0) {
        ff_mbuf_free(m);
        return -1;
    }
    ff_mbuf_free(m);
    send(card, frame, (size_t)total, MSG_DONTWAIT);
    return 0;
}

// The buffer of a frame received, which an mbuf took as its storage.
void ff_dpdk_pktmbuf_free(void* m) {
    free(m);
}

int inetReceive(void) {
    enum { room = 2048 };
    int frames = 0;
    for (;;) {
        unsigned char* b = malloc(room);
        if (!b) {
            return frames;
        }
        const ssize_t n = recv(card, b, room, MSG_DONTWAIT);
        if (n <= 0 || !interface) {
            free(b);
            return n == 0 ? -1 : frames;
        }
        void* m = ff_mbuf_gethdr(b, (uint16_t)n, b, (uint16_t)n, 0);
        if (!m) {
            free(b);
            continue;
        }
        ff_veth_process_packet(interface->ifp, m);
        ++frames;
    }
}
