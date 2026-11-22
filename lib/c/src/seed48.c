#include <stdlib.h>
#include <string.h>

/*
 * seed48, POSIX's: the generator's X from its argument, the X it replaces
 * returned, and the multiplier and addend back to 0x5DEECE66D and 0xB,
 * which lcong48 may have changed. srand48 seeds by it (musl's), so it
 * puts them back too.
 */
extern unsigned short __seed48[7];

unsigned short* seed48(unsigned short* s) {
    static unsigned short previous[3];
    static const unsigned short standard[4] = {0xe66d, 0xdeec, 0x5, 0xb};

    memcpy(previous, __seed48, sizeof previous);
    memcpy(__seed48, s, sizeof previous);
    memcpy(__seed48 + 3, standard, sizeof standard);

    return previous;
}
