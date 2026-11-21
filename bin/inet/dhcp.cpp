#include <bin/inet/dhcp.h>
#include <bin/inet/host.h>

#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

namespace {
    enum class State {
        Discovering,
        Requesting,
        Bound,
    };

    constexpr int resend = 2000;
    constexpr uint8_t discover = 1;
    constexpr uint8_t offer = 2;
    constexpr uint8_t request = 3;
    constexpr uint8_t ack = 5;
    constexpr uint8_t nak = 6;

    int card = -1;
    uint8_t hw[6];
    uint32_t xid;
    State state;
    uint32_t offered;
    uint32_t server;
    long long due;
    Lease lease;

    struct Frame {
        uint8_t b[600];
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

    long long nowMs() {
        timespec ts;
        clock_gettime(CLOCK_MONOTONIC, &ts);
        return (long long)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
    }

    uint16_t checksum(const uint8_t* p, size_t n) {
        uint32_t sum = 0;
        for (size_t i = 0; i + 1 < n; i += 2) {
            sum += be16(p + i);
        }
        while (sum >> 16) {
            sum = (sum & 0xffff) + (sum >> 16);
        }
        return uint16_t(~sum);
    }

    // A message of a type broadcast to the servers, of 0.0.0.0, asking for
    // the broadcast of the answer: a REQUEST names the address and its
    // server.
    void send(uint8_t type) {
        Frame f;
        const uint8_t everyone[6] = {0xff, 0xff, 0xff, 0xff, 0xff, 0xff};
        f.put(everyone, 6);
        f.put(hw, 6);
        f.u16(0x0800);
        const size_t options = type == request ? 3 + 6 + 6 + 7 + 1 : 3 + 7 + 1;
        const size_t udp = 8 + 236 + 4 + options;
        const size_t ip = f.n;
        f.u8(0x45);
        f.u8(0);
        f.u16(uint16_t(20 + udp));
        f.u16(0);
        f.u16(0);
        f.u8(64);
        f.u8(17);
        f.u16(0);
        f.zeros(4);
        f.u32(0xffffffff);
        const uint16_t sum = checksum(f.b + ip, 20);
        f.b[ip + 10] = uint8_t(sum >> 8);
        f.b[ip + 11] = uint8_t(sum);
        f.u16(68);
        f.u16(67);
        f.u16(uint16_t(udp));
        f.u16(0);
        f.u8(1);
        f.u8(1);
        f.u8(6);
        f.u8(0);
        f.u32(xid);
        f.u16(0);
        f.u16(0x8000);
        f.zeros(16);
        f.put(hw, 6);
        f.zeros(10 + 64 + 128);
        f.u32(0x63825363);
        f.u8(53);
        f.u8(1);
        f.u8(type);
        if (type == request) {
            f.u8(50);
            f.u8(4);
            f.put(&offered, 4);
            f.u8(54);
            f.u8(4);
            f.put(&server, 4);
        }
        // What to give: the mask, the router, the name server, the lease.
        f.u8(55);
        f.u8(5);
        f.u8(1);
        f.u8(3);
        f.u8(6);
        f.u8(51);
        f.u8(26);
        f.u8(255);
        if (::send(card, f.b, f.n, MSG_DONTWAIT) == ssize_t(f.n)) {
            ++inetCounts.outFrames;
            inetCounts.outBytes += f.n;
        } else {
            ++inetCounts.dropped;
        }
        due = nowMs() + resend;
    }
}

void dhcpStart(int fd, const uint8_t* mac) {
    card = fd;
    memcpy(hw, mac, sizeof hw);
    getentropy(&xid, sizeof xid);
    state = State::Discovering;
    send(discover);
}

bool dhcpFrame(const uint8_t* frame, size_t n) {
    if (state == State::Bound || n < 14 + 20 + 8 + 240 || be16(frame + 12) != 0x0800) {
        return false;
    }
    const uint8_t* ip = frame + 14;
    const size_t header = size_t(ip[0] & 15) * 4;
    const uint8_t* udp = ip + header;
    if (ip[9] != 17 || n < 14 + header + 8 + 240 || be16(udp + 2) != 68) {
        return false;
    }
    const uint8_t* m = udp + 8;
    if (m[0] != 2 || be32(m + 4) != xid || be32(m + 236) != 0x63825363) {
        return false;
    }
    uint8_t type = 0;
    Lease got{};
    memcpy(&got.address, m + 16, 4);
    uint32_t from = 0;
    for (const uint8_t *o = m + 240, *end = frame + n; o < end && *o != 255;) {
        if (*o == 0) {
            ++o;
            continue;
        }
        if (o + 2 > end || o + 2 + o[1] > end) {
            break;
        }
        const uint8_t code = o[0], length = o[1];
        const uint8_t* v = o + 2;
        if (code == 53 && length >= 1) {
            type = v[0];
        } else if (code == 54 && length >= 4) {
            memcpy(&from, v, 4);
        } else if (code == 1 && length >= 4) {
            memcpy(&got.mask, v, 4);
        } else if (code == 3 && length >= 4) {
            memcpy(&got.router, v, 4);
        } else if (code == 6 && length >= 4) {
            memcpy(&got.dns, v, 4);
        } else if (code == 51 && length >= 4) {
            got.seconds = be32(v);
        }
        o += 2 + length;
    }
    if (state == State::Discovering && type == offer) {
        offered = got.address;
        server = from;
        state = State::Requesting;
        send(request);
    } else if (state == State::Requesting && type == ack) {
        lease = got;
        state = State::Bound;
        return true;
    } else if (state == State::Requesting && type == nak) {
        state = State::Discovering;
        send(discover);
    }
    return false;
}

int dhcpTimer() {
    if (state == State::Bound) {
        return -1;
    }
    const long long now = nowMs();
    if (now >= due) {
        send(state == State::Requesting ? request : discover);
    }
    return int(due - nowMs());
}

const Lease& dhcpLease() {
    return lease;
}
