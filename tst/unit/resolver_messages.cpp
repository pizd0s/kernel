//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "resolver messages: ok\n"

// The resolver's messages, as BIND's resolver has them in glibc and musl:
// res_mkquery's question, recursion desired; dn_comp's names, each a
// pointer to the longest end of a name before it that dnptrs lists, which
// dn_expand writes out again and dn_skipname steps over; a loop of
// pointers refused; ns_initparse and ns_parserr's header, sections and
// records of an answer, a CNAME's data a name of the message, HEADER's
// fields. lib/c's alone: res_init's state of the system, its servers the
// cards' and its search list LOCALDOMAIN's, and res_query's and
// res_search's TRY_AGAIN where no card has a name server, as the unit's
// system has none; ns_name_uncompress's root is ".", as glibc's; a
// question asks for no AD, as glibc's, where musl's sets it.

#include <arpa/inet.h>
#include <arpa/nameser.h>
#include <assert.h>
#include <errno.h>
#include <netdb.h>
#include <resolv.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static const unsigned char question[] = {3, 'w', 'w', 'w', 7, 'E', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0};

int main() {
    unsigned char q[512];
    const int n = res_mkquery(QUERY, "www.Example.com", C_IN, T_MX, nullptr, 0, nullptr, q, sizeof q);
    assert(n == NS_HFIXEDSZ + int(sizeof question) + NS_QFIXEDSZ);
    assert(q[2] == 0x01 && ns_get16(q + 4) == 1 && ns_get16(q + 6) == 0 && ns_get16(q + 8) == 0 && ns_get16(q + 10) == 0);
    assert(memcmp(q + NS_HFIXEDSZ, question, sizeof question) == 0);
    assert(ns_get16(q + NS_HFIXEDSZ + sizeof question) == T_MX && ns_get16(q + NS_HFIXEDSZ + sizeof question + 2) == C_IN);
    char longest[80];
    memset(longest, 'x', 64);
    strcpy(longest + 64, ".com");
    assert(res_mkquery(QUERY, longest, C_IN, T_A, nullptr, 0, nullptr, q, sizeof q) == -1);
    assert(res_mkquery(QUERY, "a..b", C_IN, T_A, nullptr, 0, nullptr, q, sizeof q) == -1);
    assert(res_mkquery(QUERY, "www.example.com", C_IN, T_A, nullptr, 0, nullptr, q, 20) == -1);

    unsigned char m[512] = {};
    unsigned char* dnptrs[8] = {m, nullptr};
    unsigned char** last = dnptrs + 8;
    unsigned char* at = m + NS_HFIXEDSZ;
    const int first = dn_comp("www.example.com", at, int(m + sizeof m - at), dnptrs, last);
    assert(first == 17 && memcmp(at, "\3www\7example\3com", 17) == 0);
    const int second = dn_comp("mail.example.com", at + first, int(m + sizeof m - at - first), dnptrs, last);
    assert(second == 7 && memcmp(at + first, "\4mail\xc0\x10", 7) == 0);
    const int third = dn_comp("example.com", at + first + second, 64, dnptrs, last);
    assert(third == 2 && at[first + second] == 0xc0 && at[first + second + 1] == 0x10);
    const unsigned char* eom = at + first + second + third;
    char name[NS_MAXDNAME];
    assert(dn_expand(m, eom, at + first, name, sizeof name) == 7 && strcmp(name, "mail.example.com") == 0);
    assert(dn_expand(m, eom, at + first + second, name, sizeof name) == 2 && strcmp(name, "example.com") == 0);
    assert(dn_skipname(at, eom) == 17 && dn_skipname(at + first, eom) == 7 && dn_skipname(at + first + second, eom) == 2);
    assert(dn_expand(m, eom, at + first, name, 10) == -1);
    unsigned char loop[] = {0xc0, 0x00};
    assert(dn_expand(loop, loop + 2, loop, name, sizeof name) == -1);
    assert(dn_comp("www.example.com", q, 5, nullptr, nullptr) == -1);

    // An answer: www.example.com is web.example.com, which is 192.0.2.7.
    unsigned char a[512];
    unsigned char* p = a;
    NS_PUT16(0x1234, p);
    NS_PUT16(0x8180, p);
    NS_PUT16(1, p);
    NS_PUT16(2, p);
    NS_PUT16(0, p);
    NS_PUT16(0, p);
    memcpy(p, "\3www\7example\3com", 17);
    p += 17;
    NS_PUT16(T_A, p);
    NS_PUT16(C_IN, p);
    NS_PUT16(0xc00c, p);
    NS_PUT16(T_CNAME, p);
    NS_PUT16(C_IN, p);
    NS_PUT32(3600, p);
    NS_PUT16(6, p);
    memcpy(p, "\3web\xc0\x10", 6);
    p += 6;
    NS_PUT16(0xc000 | unsigned(p - a - 6), p);
    NS_PUT16(T_A, p);
    NS_PUT16(C_IN, p);
    NS_PUT32(300, p);
    NS_PUT16(4, p);
    memcpy(p, "\xc0\x00\x02\x07", 4);
    p += 4;
    const int size = int(p - a);

    const HEADER* h = reinterpret_cast<const HEADER*>(a);
    assert(ntohs(h->id) == 0x1234 && h->qr == 1 && h->rd == 1 && h->ra == 1 && h->rcode == NOERROR && h->opcode == QUERY);
    assert(ntohs(h->qdcount) == 1 && ntohs(h->ancount) == 2 && h->aa == 0 && h->tc == 0);
    assert(ns_skiprr(a + NS_HFIXEDSZ, a + size, ns_s_qd, 1) == 21);
    ns_msg msg;
    assert(ns_initparse(a, size, &msg) == 0);
    assert(ns_msg_id(msg) == 0x1234 && ns_msg_count(msg, ns_s_qd) == 1 && ns_msg_count(msg, ns_s_an) == 2 && ns_msg_count(msg, ns_s_ar) == 0);
    assert(ns_msg_getflag(msg, ns_f_qr) == 1 && ns_msg_getflag(msg, ns_f_rd) == 1 && ns_msg_getflag(msg, ns_f_rcode) == ns_r_noerror);
    ns_rr rr;
    assert(ns_parserr(&msg, ns_s_qd, 0, &rr) == 0 && strcmp(ns_rr_name(rr), "www.example.com") == 0);
    assert(ns_rr_type(rr) == ns_t_a && ns_rr_class(rr) == ns_c_in);
    assert(ns_parserr(&msg, ns_s_an, 0, &rr) == 0 && ns_rr_type(rr) == ns_t_cname && ns_rr_ttl(rr) == 3600 && ns_rr_rdlen(rr) == 6);
    assert(ns_name_uncompress(a, a + size, ns_rr_rdata(rr), name, sizeof name) == 6 && strcmp(name, "web.example.com") == 0);
    assert(ns_parserr(&msg, ns_s_an, 1, &rr) == 0 && strcmp(rr.name, "web.example.com") == 0 && ns_rr_type(rr) == ns_t_a);
    assert(ns_rr_ttl(rr) == 300 && ns_rr_rdlen(rr) == 4 && memcmp(ns_rr_rdata(rr), "\xc0\x00\x02\x07", 4) == 0);
    errno = 0;
    assert(ns_parserr(&msg, ns_s_an, 2, &rr) == -1 && errno == ENODEV);
    assert(ns_parserr(&msg, ns_s_an, 0, &rr) == 0 && ns_rr_type(rr) == ns_t_cname);
    assert(ns_initparse(a, size - 1, &msg) == -1 && ns_initparse(a, 11, &msg) == -1);
    unsigned char four[4];
    ns_put32(0x01020304, four);
    ns_put16(0xbeef, four);
    assert(ns_get16(four) == 0xbeef && ns_get32(four) == 0xbeef0304);

#ifdef __wasm__
    assert(res_mkquery(QUERY, "www.Example.com", C_IN, T_MX, nullptr, 0, nullptr, q, sizeof q) == n && q[3] == 0);
    unsigned char root[] = {0};
    assert(ns_name_uncompress(root, root + 1, root, name, sizeof name) == 1 && strcmp(name, ".") == 0);
    assert(dn_expand(root, root + 1, root, name, sizeof name) == 1 && strcmp(name, "") == 0);
    setenv("LOCALDOMAIN", "example.net example.org", 1);
    assert(res_init() == 0);
    assert((_res.options & RES_INIT) && (_res.options & RES_RECURSE) && _res.retrans == RES_TIMEOUT && _res.retry == RES_DFLRETRY);
    assert(_res.ndots == 1 && _res.nscount == 0);
    assert(strcmp(_res.dnsrch[0], "example.net") == 0 && strcmp(_res.dnsrch[1], "example.org") == 0 && !_res.dnsrch[2]);
    assert(strcmp(_res.defdname, "example.net") == 0);
    unsigned char answer[512];
    errno = 0;
    assert(res_query("www.example.com", C_IN, T_A, answer, sizeof answer) == -1 && errno == ENETUNREACH);
    assert(res_search("www", C_IN, T_A, answer, sizeof answer) == -1);
    errno = 0;
    assert(res_send(q, n, answer, sizeof answer) == -1 && errno == ENETUNREACH);
    struct __res_state mine = {};
    assert(res_ninit(&mine) == 0 && mine.ndots == 1 && strcmp(mine.dnsrch[1], "example.org") == 0);
    res_nclose(&mine);
#endif
    printf("resolver messages: ok\n");
}
