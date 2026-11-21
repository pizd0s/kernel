#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

// The card of the stack, host.c's: the descriptor of the device, and the
// frames on it, into the stack, until none is there; how many, or -1
// once the device is gone. A frame inetTaken takes, true, the stack does
// not get: DHCP's answers, which the card's client has. inetCounts are
// the card's frames and bytes in and out, and the frames dropped, the
// stack's and before it.
#ifdef __cplusplus
extern "C" {
#endif

struct InetCounts {
    uint64_t inFrames;
    uint64_t inBytes;
    uint64_t outFrames;
    uint64_t outBytes;
    uint64_t dropped;
};

extern struct InetCounts inetCounts;
extern bool (*inetTaken)(const uint8_t* frame, size_t n);

void inetCard(int fd);
int inetReceive(void);

#ifdef __cplusplus
}
#endif
