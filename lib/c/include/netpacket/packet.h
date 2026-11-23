#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * A link's address, AF_PACKET's: its interface's index, its hardware's
 * type (net/if_arp.h's ARPHRD_) and its hardware address, sll_halen bytes
 * of sll_addr. getifaddrs gives one of each interface.
 */
struct sockaddr_ll {
    unsigned short sll_family;
    unsigned short sll_protocol;
    int sll_ifindex;
    unsigned short sll_hatype;
    unsigned char sll_pkttype;
    unsigned char sll_halen;
    unsigned char sll_addr[8];
};

#define PACKET_HOST 0
#define PACKET_BROADCAST 1
#define PACKET_MULTICAST 2
#define PACKET_OTHERHOST 3
#define PACKET_OUTGOING 4

__END_DECLS
