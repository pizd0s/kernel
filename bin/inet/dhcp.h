#pragma once

#include <stddef.h>
#include <stdint.h>

// What DHCP gave a card: its address, the network's mask, its router and
// name server, in network order, and the lease's length in seconds. An
// address 0 is no lease.
struct Lease {
    uint32_t address;
    uint32_t mask;
    uint32_t router;
    uint32_t dns;
    uint32_t seconds;
};

// Where the client is: asking for a lease (DISCOVER, then REQUEST of an
// OFFER), with one, asking for it again at half its time (unicast to its
// server) or at seven eighths (broadcast to any), or having given it up.
enum class Dhcp {
    Discovering,
    Requesting,
    Bound,
    Renewing,
    Rebinding,
    Released,
};

// A DHCP client of one card, of the card's frames: dhcpStart broadcasts
// DISCOVER from the card's hardware address; dhcpFrame takes a frame the
// card got, true for an answer to the client, which the stack does not
// get; dhcpTimer sends again what had no answer, asks for the lease
// again at half its time and at seven eighths, lets it go at its end,
// and is how long until it acts next, in milliseconds, -1 for never.
// dhcpRenew asks for the lease again now, or for one when there is none;
// dhcpRelease gives it up (RELEASE). changed, of dhcpStart's, hears of
// each lease ACKed, new or the same again, and of a lease gone: dhcpLease
// is the lease then, dhcpState where the client is, dhcpLeases how many
// leases it got.
using DhcpChanged = void (*)();

void dhcpStart(int card, const uint8_t* mac, DhcpChanged changed);
bool dhcpFrame(const uint8_t* frame, size_t n);
int dhcpTimer();
void dhcpRenew();
void dhcpRelease();
const Lease& dhcpLease();
Dhcp dhcpState();
unsigned dhcpLeases();
