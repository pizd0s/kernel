#pragma once

#include <__base.h>

__BEGIN_DECLS

/* getrandom, the libcs': bytes of the kernel's cryptographic generator, which never blocks. */
#define GRND_NONBLOCK 0x0001
#define GRND_RANDOM 0x0002
#define GRND_INSECURE 0x0004

ssize_t getrandom(void*, size_t, unsigned);
int getentropy(void*, size_t);

__END_DECLS
