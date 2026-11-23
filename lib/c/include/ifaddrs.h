#pragma once

#include <__base.h>

__BEGIN_DECLS

struct sockaddr;

/*
 * The system's interfaces and their addresses, the kernel's, of /dev/net:
 * first each interface's link, an AF_PACKET address (netpacket/packet.h)
 * and its broadcast address, then each IPv4 address an interface has,
 * with its mask and, where the interface has IFF_BROADCAST, its
 * broadcast address. ifa_flags are the interface's, net/if.h's IFF_.
 * The list is one block, which freeifaddrs frees.
 */
struct ifaddrs {
    struct ifaddrs* ifa_next;
    char* ifa_name;
    unsigned int ifa_flags;
    struct sockaddr* ifa_addr;
    struct sockaddr* ifa_netmask;
    union {
        struct sockaddr* ifu_broadaddr;
        struct sockaddr* ifu_dstaddr;
    } ifa_ifu;
    void* ifa_data;
};

#define ifa_broadaddr ifa_ifu.ifu_broadaddr
#define ifa_dstaddr ifa_ifu.ifu_dstaddr

int getifaddrs(struct ifaddrs**);
void freeifaddrs(struct ifaddrs*);

__END_DECLS
