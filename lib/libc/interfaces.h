#pragma once

#include <netinet/in.h>

/*
 * The system's cards, /dev/net's, as their files say: the loopback first,
 * then by name, a card only where its ctl can be opened, its index its
 * place from 1. A card is up where its ctl names a stack that is not
 * "off"; its Ethernet address is its addr file's; its IPv4 address and
 * mask the "inet a.b.c.d/bits" line of its iface/status, and its name
 * server the last "dns a.b.c.d" line there.
 */
struct Interface {
    char name[64];
    unsigned index;
    unsigned flags;
    unsigned char mac[6];
    int inet;
    struct in_addr addr;
    struct in_addr mask;
    int named;
    struct in_addr dns;
};

// The cards on the heap, and how many; -1 and ENOMEM where there is no
// room for them.
int __interfaces(struct Interface** list);
