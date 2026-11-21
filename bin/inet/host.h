#pragma once

// The card of the stack, host.c's: the descriptor of the device, and the
// frames on it, into the stack, until none is there; how many, or -1
// once the device is gone.
#ifdef __cplusplus
extern "C" {
#endif

void inetCard(int fd);
int inetReceive(void);

#ifdef __cplusplus
}
#endif
