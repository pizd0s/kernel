#pragma once

#include <__base.h>

__BEGIN_DECLS

#define _IOC_NONE 0U
#define _IOC_WRITE 1U
#define _IOC_READ 2U
#define _IOC(a, b, c, d) (((a) << 30) | ((b) << 8) | (c) | ((d) << 16))
#define _IO(a, b) _IOC(_IOC_NONE, (a), (b), 0)
#define _IOW(a, b, c) _IOC(_IOC_WRITE, (a), (b), sizeof(c))
#define _IOR(a, b, c) _IOC(_IOC_READ, (a), (b), sizeof(c))
#define _IOWR(a, b, c) _IOC(_IOC_READ | _IOC_WRITE, (a), (b), sizeof(c))

// The requests the kernel takes: a terminal's, and a descriptor's of any kind.
#define TCGETS 0x5401
#define TCSETS 0x5402
#define TCSETSW 0x5403
#define TCSETSF 0x5404
#define TCSBRK 0x5409
#define TCXONC 0x540A
#define TCFLSH 0x540B
#define TIOCSCTTY 0x540E
#define TIOCGPGRP 0x540F
#define TIOCSPGRP 0x5410
#define TIOCOUTQ 0x5411
#define TIOCGWINSZ 0x5413
#define TIOCSWINSZ 0x5414
#define FIONREAD 0x541B
#define TIOCINQ FIONREAD
#define FIONBIO 0x5421
#define TIOCNOTTY 0x5422
#define TIOCGSID 0x5429
#define TIOCGPTN 0x80045430
#define TIOCSPTLCK 0x40045431
#define FIONCLEX 0x5450
#define FIOCLEX 0x5451

int ioctl(int, unsigned long, ...);

__END_DECLS
