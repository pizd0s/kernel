#pragma once

#include <bin/inet/dhcp.h>

#include <stdint.h>

// The stack of a card: FreeBSD's, as F-Stack runs it, its interface name
// the card's, of the lease's address, mask and router. stackFrames gives
// it the frames the card has; stackTicks runs its ticks of hz that are
// due and is how long until the next, in milliseconds.
void stackStart(int card, const char* name, const uint8_t* mac, const Lease& lease);
void stackFrames();
int stackTicks();
