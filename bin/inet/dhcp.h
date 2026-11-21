#pragma once

#include <stddef.h>
#include <stdint.h>

// What DHCP gave a card: its address, the network's mask, its router and
// name server, in network order, and the lease's length in seconds.
struct Lease {
    uint32_t address;
    uint32_t mask;
    uint32_t router;
    uint32_t dns;
    uint32_t seconds;
};

// A DHCP client of one card, on the card's frames before its stack has
// them: dhcpStart broadcasts DISCOVER from the card's hardware address;
// dhcpFrame takes each frame the card gets, REQUESTs what an OFFER
// offers and is true at its ACK, the lease dhcpLease's then; dhcpTimer
// sends again what had no answer and is how long until it sends next, in
// milliseconds.
void dhcpStart(int card, const uint8_t* mac);
bool dhcpFrame(const uint8_t* frame, size_t n);
int dhcpTimer();
const Lease& dhcpLease();
