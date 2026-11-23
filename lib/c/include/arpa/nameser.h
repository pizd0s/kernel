#pragma once

#include <__base.h>
#include <stddef.h>
#include <stdint.h>

__BEGIN_DECLS

/*
 * The DNS's messages, RFC 1035's: their sizes, the classes, types,
 * opcodes and codes of their questions and answers, and their parse,
 * the kernel's: ns_initparse takes a message's header and finds its
 * sections, ns_parserr gives a record of a section, its name expanded,
 * its data where it is in the message.
 */
#define NS_PACKETSZ 512
#define NS_MAXDNAME 1025
#define NS_MAXMSG 65535
#define NS_MAXCDNAME 255
#define NS_MAXLABEL 63
#define NS_HFIXEDSZ 12
#define NS_QFIXEDSZ 4
#define NS_RRFIXEDSZ 10
#define NS_INT32SZ 4
#define NS_INT16SZ 2
#define NS_INT8SZ 1
#define NS_INADDRSZ 4
#define NS_IN6ADDRSZ 16
#define NS_CMPRSFLGS 0xc0
#define NS_DEFAULTPORT 53

typedef enum __ns_sect {
    ns_s_qd = 0,
    ns_s_zn = 0,
    ns_s_an = 1,
    ns_s_pr = 1,
    ns_s_ns = 2,
    ns_s_ud = 2,
    ns_s_ar = 3,
    ns_s_max = 4
} ns_sect;

typedef struct __ns_msg {
    const unsigned char* _msg;
    const unsigned char* _eom;
    uint16_t _id;
    uint16_t _flags;
    uint16_t _counts[ns_s_max];
    const unsigned char* _sections[ns_s_max];
    ns_sect _sect;
    int _rrnum;
    const unsigned char* _msg_ptr;
} ns_msg;

#define ns_msg_id(handle) ((handle)._id + 0)
#define ns_msg_base(handle) ((handle)._msg + 0)
#define ns_msg_end(handle) ((handle)._eom + 0)
#define ns_msg_size(handle) ((handle)._eom - (handle)._msg)
#define ns_msg_count(handle, section) ((handle)._counts[section] + 0)

typedef struct __ns_rr {
    char name[NS_MAXDNAME];
    uint16_t type;
    uint16_t rr_class;
    uint32_t ttl;
    uint16_t rdlength;
    const unsigned char* rdata;
} ns_rr;

#define ns_rr_name(rr) (((rr).name[0] != '\0') ? (rr).name : ".")
#define ns_rr_type(rr) ((ns_type)((rr).type + 0))
#define ns_rr_class(rr) ((ns_class)((rr).rr_class + 0))
#define ns_rr_ttl(rr) ((rr).ttl + 0)
#define ns_rr_rdlen(rr) ((rr).rdlength + 0)
#define ns_rr_rdata(rr) ((rr).rdata + 0)

typedef enum __ns_flag {
    ns_f_qr,
    ns_f_opcode,
    ns_f_aa,
    ns_f_tc,
    ns_f_rd,
    ns_f_ra,
    ns_f_z,
    ns_f_ad,
    ns_f_cd,
    ns_f_rcode,
    ns_f_max
} ns_flag;

typedef enum __ns_opcode {
    ns_o_query = 0,
    ns_o_iquery = 1,
    ns_o_status = 2,
    ns_o_notify = 4,
    ns_o_update = 5,
    ns_o_max = 6
} ns_opcode;

typedef enum __ns_rcode {
    ns_r_noerror = 0,
    ns_r_formerr = 1,
    ns_r_servfail = 2,
    ns_r_nxdomain = 3,
    ns_r_notimpl = 4,
    ns_r_refused = 5,
    ns_r_yxdomain = 6,
    ns_r_yxrrset = 7,
    ns_r_nxrrset = 8,
    ns_r_notauth = 9,
    ns_r_notzone = 10,
    ns_r_max = 11,
    ns_r_badvers = 16,
    ns_r_badsig = 16,
    ns_r_badkey = 17,
    ns_r_badtime = 18
} ns_rcode;

typedef enum __ns_update_operation {
    ns_uop_delete = 0,
    ns_uop_add = 1,
    ns_uop_max = 2
} ns_update_operation;

typedef enum __ns_class {
    ns_c_invalid = 0,
    ns_c_in = 1,
    ns_c_2 = 2,
    ns_c_chaos = 3,
    ns_c_hs = 4,
    ns_c_none = 254,
    ns_c_any = 255,
    ns_c_max = 65536
} ns_class;

typedef enum __ns_type {
    ns_t_invalid = 0,
    ns_t_a = 1,
    ns_t_ns = 2,
    ns_t_md = 3,
    ns_t_mf = 4,
    ns_t_cname = 5,
    ns_t_soa = 6,
    ns_t_mb = 7,
    ns_t_mg = 8,
    ns_t_mr = 9,
    ns_t_null = 10,
    ns_t_wks = 11,
    ns_t_ptr = 12,
    ns_t_hinfo = 13,
    ns_t_minfo = 14,
    ns_t_mx = 15,
    ns_t_txt = 16,
    ns_t_rp = 17,
    ns_t_afsdb = 18,
    ns_t_x25 = 19,
    ns_t_isdn = 20,
    ns_t_rt = 21,
    ns_t_nsap = 22,
    ns_t_nsap_ptr = 23,
    ns_t_sig = 24,
    ns_t_key = 25,
    ns_t_px = 26,
    ns_t_gpos = 27,
    ns_t_aaaa = 28,
    ns_t_loc = 29,
    ns_t_nxt = 30,
    ns_t_eid = 31,
    ns_t_nimloc = 32,
    ns_t_srv = 33,
    ns_t_atma = 34,
    ns_t_naptr = 35,
    ns_t_kx = 36,
    ns_t_cert = 37,
    ns_t_a6 = 38,
    ns_t_dname = 39,
    ns_t_sink = 40,
    ns_t_opt = 41,
    ns_t_apl = 42,
    ns_t_tkey = 249,
    ns_t_tsig = 250,
    ns_t_ixfr = 251,
    ns_t_axfr = 252,
    ns_t_mailb = 253,
    ns_t_maila = 254,
    ns_t_any = 255,
    ns_t_zxfr = 256,
    ns_t_max = 65536
} ns_type;

#define NS_GET16(s, cp) \
    do { \
        const unsigned char* __t_cp = (const unsigned char*)(cp); \
        (s) = (uint16_t)((uint16_t)__t_cp[0] << 8 | (uint16_t)__t_cp[1]); \
        (cp) += NS_INT16SZ; \
    } while (0)

#define NS_GET32(l, cp) \
    do { \
        const unsigned char* __t_cp = (const unsigned char*)(cp); \
        (l) = (uint32_t)__t_cp[0] << 24 | (uint32_t)__t_cp[1] << 16 | (uint32_t)__t_cp[2] << 8 | (uint32_t)__t_cp[3]; \
        (cp) += NS_INT32SZ; \
    } while (0)

#define NS_PUT16(s, cp) \
    do { \
        uint16_t __t_s = (uint16_t)(s); \
        unsigned char* __t_cp = (unsigned char*)(cp); \
        __t_cp[0] = (unsigned char)(__t_s >> 8); \
        __t_cp[1] = (unsigned char)__t_s; \
        (cp) += NS_INT16SZ; \
    } while (0)

#define NS_PUT32(l, cp) \
    do { \
        uint32_t __t_l = (uint32_t)(l); \
        unsigned char* __t_cp = (unsigned char*)(cp); \
        __t_cp[0] = (unsigned char)(__t_l >> 24); \
        __t_cp[1] = (unsigned char)(__t_l >> 16); \
        __t_cp[2] = (unsigned char)(__t_l >> 8); \
        __t_cp[3] = (unsigned char)__t_l; \
        (cp) += NS_INT32SZ; \
    } while (0)

static inline unsigned ns_get16(const unsigned char* __p) {
    return (unsigned)__p[0] << 8 | __p[1];
}

static inline unsigned long ns_get32(const unsigned char* __p) {
    return (unsigned long)__p[0] << 24 | (unsigned long)__p[1] << 16 | (unsigned long)__p[2] << 8 | __p[3];
}

static inline void ns_put16(unsigned __v, unsigned char* __p) {
    __p[0] = (unsigned char)(__v >> 8);
    __p[1] = (unsigned char)__v;
}

static inline void ns_put32(unsigned long __v, unsigned char* __p) {
    __p[0] = (unsigned char)(__v >> 24);
    __p[1] = (unsigned char)(__v >> 16);
    __p[2] = (unsigned char)(__v >> 8);
    __p[3] = (unsigned char)__v;
}

/* A flag of a message's header, ns_flag's, as a number. */
static inline int ns_msg_getflag(ns_msg __handle, int __flag) {
    static const unsigned short __masks[ns_f_max] = {0x8000, 0x7800, 0x0400, 0x0200, 0x0100, 0x0080, 0x0040, 0x0020, 0x0010, 0x000f};
    static const unsigned char __shifts[ns_f_max] = {15, 11, 10, 9, 8, 7, 6, 5, 4, 0};
    return __flag >= 0 && __flag < ns_f_max ? (__handle._flags & __masks[__flag]) >> __shifts[__flag] : 0;
}

int ns_initparse(const unsigned char*, int, ns_msg*);
int ns_parserr(ns_msg*, ns_sect, int, ns_rr*);
int ns_skiprr(const unsigned char*, const unsigned char*, ns_sect, int);
int ns_name_uncompress(const unsigned char*, const unsigned char*, const unsigned char*, char*, size_t);

__END_DECLS

#include <arpa/nameser_compat.h>
