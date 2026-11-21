#pragma once

#include <bin/inet/dhcp.h>

#include <stdint.h>

// The stack of a card: FreeBSD's, as F-Stack runs it, its interface name
// the card's, of the lease's address, mask and router. Its clock ticks
// hz a second only as something is due: stackClock runs the ticks that
// went by, before anything asks the stack, so that what it does is of
// the time it is; stackFrames gives it the frames the card has;
// stackTicks runs what is due and is how long until the first callout,
// in milliseconds, -1 for none.
void stackStart(int card, const char* name, const uint8_t* mac, const Lease& lease);
void stackClock();
void stackFrames();
int stackTicks();
