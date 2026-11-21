// The network device vm gives the system with --net, without a stack: a
// SOCK_SEQPACKET end, one packet an Ethernet frame, which NETS names after
// the disks of DISKS. Behind it is vm's network, gvisor-tap-vsock's, with
// its gateway: the probe asks it its address (ARP), an address of its
// own (DHCP), and an echo (ICMP), and prints what it answered.
//
//     net_probe

#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <unistd.h>

namespace {
    const uint8_t ours[6] = {0x5a, 0x94, 0xef, 0xe4, 0x0c, 0xee};
    const uint8_t everyone[6] = {0xff, 0xff, 0xff, 0xff, 0xff, 0xff};
    const uint8_t gateway[4] = {192, 168, 127, 1};
    constexpr uint32_t xid = 0x70697a64;
    uint8_t me[4]; // ours once DHCP offered it, whose ARP requests the probe answers

    struct Frame {
        uint8_t b[2048];
        size_t n = 0;

        void put(const void* data, size_t size) {
            memcpy(b + n, data, size);
            n += size;
        }

        void u8(uint8_t v) {
            b[n++] = v;
        }

        void u16(uint16_t v) {
            u8(uint8_t(v >> 8));
            u8(uint8_t(v));
        }

        void u32(uint32_t v) {
            u16(uint16_t(v >> 16));
            u16(uint16_t(v));
        }

        void zeros(size_t count) {
            memset(b + n, 0, count);
            n += count;
        }
    };

    uint16_t be16(const uint8_t* p) {
        return uint16_t(p[0] << 8 | p[1]);
    }

    uint32_t be32(const uint8_t* p) {
        return uint32_t(be16(p)) << 16 | be16(p + 2);
    }

    uint16_t checksum(const uint8_t* p, size_t n) {
        uint32_t sum = 0;
        for (size_t i = 0; i + 1 < n; i += 2) {
            sum += be16(p + i);
        }
        if (n % 2 != 0) {
            sum += uint32_t(p[n - 1]) << 8;
        }
        while (sum >> 16) {
            sum = (sum & 0xffff) + (sum >> 16);
        }
        return uint16_t(~sum);
    }

    void ethernet(Frame& f, const uint8_t* to, uint16_t type) {
        f.put(to, 6);
        f.put(ours, 6);
        f.u16(type);
    }

    // An IPv4 header of a datagram of length bytes after it, its checksum
    // made.
    void ip(Frame& f, uint8_t protocol, const uint8_t* from, const uint8_t* to, size_t length) {
        const size_t at = f.n;
        f.u8(0x45);
        f.u8(0);
        f.u16(uint16_t(20 + length));
        f.u16(1);
        f.u16(0);
        f.u8(64);
        f.u8(protocol);
        f.u16(0);
        f.put(from, 4);
        f.put(to, 4);
        const uint16_t sum = checksum(f.b + at, 20);
        f.b[at + 10] = uint8_t(sum >> 8);
        f.b[at + 11] = uint8_t(sum);
    }

    int device() {
        int fd = 3;
        if (const char* disks = getenv("DISKS")) {
            for (const char* at = disks; *at;) {
                while (*at == ' ') {
                    ++at;
                }
                if (*at) {
                    ++fd;
                }
                while (*at && *at != ' ') {
                    ++at;
                }
            }
        }
        return fd;
    }

    void send(int fd, const Frame& f) {
        if (::send(fd, f.b, f.n, 0) != ssize_t(f.n)) {
            printf("send: %s\n", strerror(errno));
            exit(1);
        }
    }

    // Answers an ARP request for our address, as the gateway asks it
    // before it answers us.
    void answer(int fd, const Frame& asked) {
        const uint8_t* a = asked.b + 14;
        Frame f;
        ethernet(f, a + 8, 0x0806);
        f.u16(1);
        f.u16(0x0800);
        f.u8(6);
        f.u8(4);
        f.u16(2);
        f.put(ours, 6);
        f.put(me, 4);
        f.put(a + 8, 10);
        send(fd, f);
    }

    // The next frame of ethertype type to us or to everyone, or none
    // in the time the device's SO_RCVTIMEO gives.
    bool next(int fd, uint16_t type, Frame& f) {
        for (;;) {
            const ssize_t n = recv(fd, f.b, sizeof f.b, 0);
            if (n < 0) {
                return false;
            }
            f.n = size_t(n);
            const bool mine = memcmp(f.b, ours, 6) == 0 || memcmp(f.b, everyone, 6) == 0;
            const uint8_t* a = f.b + 14;
            if (f.n >= 42 && mine && be16(f.b + 12) == 0x0806 && be16(a + 6) == 1 && memcmp(a + 24, me, 4) == 0) {
                answer(fd, f);
            } else if (f.n >= 14 && mine && be16(f.b + 12) == type) {
                return true;
            }
        }
    }

    // The gateway's hardware address, into hw.
    bool arp(int fd, uint8_t* hw) {
        Frame f;
        ethernet(f, everyone, 0x0806);
        f.u16(1);
        f.u16(0x0800);
        f.u8(6);
        f.u8(4);
        f.u16(1);
        f.put(ours, 6);
        f.zeros(4);
        f.zeros(6);
        f.put(gateway, 4);
        send(fd, f);
        Frame got;
        while (next(fd, 0x0806, got)) {
            const uint8_t* a = got.b + 14;
            if (got.n >= 42 && be16(a + 6) == 2 && memcmp(a + 14, gateway, 4) == 0) {
                memcpy(hw, a + 8, 6);
                printf("arp: 192.168.127.1 is at %02x:%02x:%02x:%02x:%02x:%02x\n", hw[0], hw[1], hw[2], hw[3], hw[4], hw[5]);
                return true;
            }
        }
        printf("arp: no answer\n");
        return false;
    }

    // A DISCOVER, and the address the OFFER has for us, into offered.
    bool dhcp(int fd, uint8_t* offered) {
        Frame f;
        ethernet(f, everyone, 0x0800);
        const uint8_t none[4] = {0, 0, 0, 0};
        const uint8_t all[4] = {255, 255, 255, 255};
        const size_t bootp = 236 + 4 + 3 + 1;
        ip(f, 17, none, all, 8 + bootp);
        f.u16(68);
        f.u16(67);
        f.u16(uint16_t(8 + bootp));
        f.u16(0);
        f.u8(1);
        f.u8(1);
        f.u8(6);
        f.u8(0);
        f.u32(xid);
        f.u16(0);
        f.u16(0x8000);
        f.zeros(16);
        f.put(ours, 6);
        f.zeros(10 + 64 + 128);
        f.u32(0x63825363);
        f.u8(53);
        f.u8(1);
        f.u8(1);
        f.u8(255);
        send(fd, f);
        Frame got;
        while (next(fd, 0x0800, got)) {
            const uint8_t* h = got.b + 14;
            const uint8_t* u = h + 4 * (h[0] & 15);
            const uint8_t* b = u + 8;
            if (got.n < size_t(b - got.b) + 240 || h[9] != 17 || be16(u + 2) != 68 || be32(b + 4) != xid) {
                continue;
            }
            memcpy(offered, b + 16, 4);
            memcpy(me, offered, 4);
            printf("dhcp: offered %d.%d.%d.%d by %d.%d.%d.%d\n", b[16], b[17], b[18], b[19], h[12], h[13], h[14], h[15]);
            return true;
        }
        printf("dhcp: no offer\n");
        return false;
    }

    void ping(int fd, const uint8_t* from, const uint8_t* hw) {
        Frame f;
        ethernet(f, hw, 0x0800);
        const char text[] = "pizdos";
        const size_t length = 8 + sizeof text;
        ip(f, 1, from, gateway, length);
        const size_t at = f.n;
        f.u8(8);
        f.u8(0);
        f.u16(0);
        f.u16(0x1234);
        f.u16(1);
        f.put(text, sizeof text);
        const uint16_t sum = checksum(f.b + at, length);
        f.b[at + 2] = uint8_t(sum >> 8);
        f.b[at + 3] = uint8_t(sum);
        send(fd, f);
        Frame got;
        while (next(fd, 0x0800, got)) {
            const uint8_t* h = got.b + 14;
            const uint8_t* m = h + 4 * (h[0] & 15);
            if (h[9] == 1 && m[0] == 0 && be16(m + 4) == 0x1234 && memcmp(m + 8, text, sizeof text) == 0) {
                printf("icmp: echo reply from %d.%d.%d.%d, %s\n", h[12], h[13], h[14], h[15], m + 8);
                return;
            }
        }
        printf("icmp: no reply\n");
    }
}

int main() {
    const char* nets = getenv("NETS");
    if (!nets || strcmp(nets, "0") != 0) {
        printf("NETS: %s\n", nets ? nets : "none");
        return 1;
    }
    const int fd = device();
    timeval wait{5, 0};
    if (setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &wait, sizeof wait) != 0) {
        printf("setsockopt: %s\n", strerror(errno));
        return 1;
    }
    uint8_t hw[6];
    uint8_t offered[4];
    if (arp(fd, hw) && dhcp(fd, offered)) {
        ping(fd, offered, hw);
    }
    return 0;
}
