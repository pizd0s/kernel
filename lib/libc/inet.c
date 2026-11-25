/*
 * The addresses of IPv4 as text and of its classes: inet_network reads
 * what inet_aton does, in the host's order; a network of a class and a
 * host on it make an address, and an address is of the class its first
 * byte says (A below 128, B below 192, C the rest).
 */
#include "sized.h"

#include <arpa/inet.h>
#include <netinet/in.h>
#include <stddef.h>

// inet_aton of FreeBSD's, as a program's forwarder passes the address's
// size.
int __kernel_inet_aton(const char* text, struct in_addr* a, size_t size) {
    sized("inet_aton", size, sizeof(struct in_addr));

    return inet_aton(text, a);
}

in_addr_t __kernel_inet_network(const char* text) {
    struct in_addr a;

    return inet_aton(text, &a) ? ntohl(a.s_addr) : INADDR_NONE;
}

struct in_addr __kernel_inet_makeaddr(in_addr_t net, in_addr_t host) {
    if (net < 256) {
        host = (host & 0xffffff) | net << 24;
    } else if (net < 65536) {
        host = (host & 0xffff) | net << 16;
    } else if (net < 1 << 24) {
        host = (host & 0xff) | net << 8;
    } else {
        host |= net;
    }

    return (struct in_addr){htonl(host)};
}

in_addr_t __kernel_inet_lnaof(struct in_addr in) {
    in_addr_t a = ntohl(in.s_addr);

    return a >> 24 < 128 ? a & 0xffffff : a >> 24 < 192 ? a & 0xffff : a & 0xff;
}

in_addr_t __kernel_inet_netof(struct in_addr in) {
    in_addr_t a = ntohl(in.s_addr);

    return a >> 24 < 128 ? a >> 24 : a >> 24 < 192 ? a >> 16 : a >> 8;
}
