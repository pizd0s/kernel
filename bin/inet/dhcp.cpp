#include <bin/inet/dhcp.h>
#include <bin/inet/host.h>

#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

namespace {
    // A frame being made: its bytes and how many.
    struct Frame {
        uint8_t b[600];
        size_t n;
    };
}

static constexpr int resend = 2000;           // ms, a DISCOVER's or a REQUEST's of an OFFER again
static constexpr long long leastWait = 60000; // ms, at least, before asking for the lease again
static constexpr uint32_t forever = 0xffffffff;
static constexpr uint8_t discover = 1;
static constexpr uint8_t offer = 2;
static constexpr uint8_t request = 3;
static constexpr uint8_t ack = 5;
static constexpr uint8_t nak = 6;
static constexpr uint8_t release = 7;

static int card = -1;
static uint8_t hw[6];
static uint8_t serverHw[6]; // the hardware address the lease's ACK came from
static uint32_t xid;
static Dhcp state = Dhcp::Released;
static uint32_t offered;
static uint32_t server;
static long long due = -1;
static long long leased; // ms the lease was ACKed at
static Lease lease;
static unsigned leases;
static DhcpChanged changed;

static void put(Frame& f, const void* data, size_t size) {
    memcpy(f.b + f.n, data, size);
    f.n += size;
}

static void u8(Frame& f, uint8_t v) {
    f.b[f.n++] = v;
}

static void u16(Frame& f, uint16_t v) {
    u8(f, uint8_t(v >> 8));
    u8(f, uint8_t(v));
}

static void u32(Frame& f, uint32_t v) {
    u16(f, uint16_t(v >> 16));
    u16(f, uint16_t(v));
}

static void zeros(Frame& f, size_t count) {
    memset(f.b + f.n, 0, count);
    f.n += count;
}

static void at16(Frame& f, size_t at, uint16_t v) {
    f.b[at] = uint8_t(v >> 8);
    f.b[at + 1] = uint8_t(v);
}

static uint16_t be16(const uint8_t* p) {
    return uint16_t(p[0] << 8 | p[1]);
}

static uint32_t be32(const uint8_t* p) {
    return uint32_t(be16(p)) << 16 | be16(p + 2);
}

static long long nowMs() {
    timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (long long)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
}

static uint16_t checksum(const uint8_t* p, size_t n) {
    uint32_t sum = 0;
    for (size_t i = 0; i + 1 < n; i += 2) {
        sum += be16(p + i);
    }
    while (sum >> 16) {
        sum = (sum & 0xffff) + (sum >> 16);
    }
    return uint16_t(~sum);
}

static void anew() {
    getentropy(&xid, sizeof xid);
}

// A message of a type. Asking for a lease, of 0.0.0.0, broadcast, the
// answer to be broadcast; a REQUEST of an OFFER names the address and
// its server. Asking for the lease again, of its address: unicast to its
// server at half its time, broadcast at seven eighths. RELEASE, unicast
// to the server.
static void send(uint8_t type) {
    const bool ours = type == release || state == Dhcp::Renewing || state == Dhcp::Rebinding;
    const bool unicast = type == release || state == Dhcp::Renewing;
    const uint8_t everyone[6] = {0xff, 0xff, 0xff, 0xff, 0xff, 0xff};
    const uint32_t from = ours ? lease.address : 0;
    Frame f{};
    put(f, unicast ? serverHw : everyone, 6);
    put(f, hw, 6);
    u16(f, 0x0800);
    const size_t ip = f.n;
    u8(f, 0x45);
    u8(f, 0);
    u16(f, 0);
    u16(f, 0);
    u16(f, 0);
    u8(f, 64);
    u8(f, 17);
    u16(f, 0);
    put(f, &from, 4);
    if (unicast) {
        put(f, &server, 4);
    } else {
        u32(f, 0xffffffff);
    }
    const size_t udp = f.n;
    u16(f, 68);
    u16(f, 67);
    u16(f, 0);
    u16(f, 0);
    u8(f, 1);
    u8(f, 1);
    u8(f, 6);
    u8(f, 0);
    u32(f, xid);
    u16(f, 0);
    u16(f, ours ? 0 : 0x8000);
    put(f, &from, 4);
    zeros(f, 12);
    put(f, hw, 6);
    zeros(f, 10 + 64 + 128);
    u32(f, 0x63825363);
    u8(f, 53);
    u8(f, 1);
    u8(f, type);
    if (type == request && state == Dhcp::Requesting) {
        u8(f, 50);
        u8(f, 4);
        put(f, &offered, 4);
    }
    if ((type == request && state == Dhcp::Requesting) || type == release) {
        u8(f, 54);
        u8(f, 4);
        put(f, &server, 4);
    }
    if (type != release) {
        // What to give: the mask, the router, the name server, the lease.
        u8(f, 55);
        u8(f, 5);
        u8(f, 1);
        u8(f, 3);
        u8(f, 6);
        u8(f, 51);
        u8(f, 26);
    }
    u8(f, 255);
    at16(f, ip + 2, uint16_t(f.n - ip));
    at16(f, udp + 4, uint16_t(f.n - udp));
    at16(f, ip + 10, checksum(f.b + ip, 20));
    if (::send(card, f.b, f.n, MSG_DONTWAIT) == ssize_t(f.n)) {
        ++inetCounts.outFrames;
        inetCounts.outBytes += f.n;
    } else {
        ++inetCounts.dropped;
    }
}

static long long t1() {
    return leased + (long long)lease.seconds * 500;
}

static long long t2() {
    return leased + (long long)lease.seconds * 875;
}

static long long ends() {
    return leased + (long long)lease.seconds * 1000;
}

// Asks for a lease, from the start.
static void discovering() {
    state = Dhcp::Discovering;
    anew();
    send(discover);
    due = nowMs() + resend;
}

// Asks for the lease again, and again after half the time left to
// limit, a minute at least, up to limit.
static void asking(long long limit) {
    send(request);
    const long long now = nowMs();
    const long long wait = (limit - now) / 2 > leastWait ? (limit - now) / 2 : leastWait;
    due = now + wait < limit ? now + wait : limit;
}

// The lease gone, its end or a NAK: the card has no address, and the
// client asks for one.
static void lost() {
    lease = {};
    discovering();
    changed();
}

void dhcpStart(int fd, const uint8_t* mac, DhcpChanged heard) {
    card = fd;
    changed = heard;
    memcpy(hw, mac, sizeof hw);
    discovering();
}

bool dhcpFrame(const uint8_t* frame, size_t n) {
    if (n < 14 + 20 + 8 + 240 || be16(frame + 12) != 0x0800) {
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
    const bool expecting = state == Dhcp::Requesting || state == Dhcp::Renewing || state == Dhcp::Rebinding;
    if (state == Dhcp::Discovering && type == offer) {
        offered = got.address;
        server = from;
        state = Dhcp::Requesting;
        send(request);
        due = nowMs() + resend;
    } else if (expecting && type == ack && got.address != 0) {
        // What an ACK of the lease again leaves out stays as it was.
        got.mask = got.mask ? got.mask : lease.mask;
        got.router = got.router ? got.router : lease.router;
        got.dns = got.dns ? got.dns : lease.dns;
        got.seconds = got.seconds ? got.seconds : lease.seconds;
        server = from ? from : server;
        memcpy(serverHw, frame + 6, sizeof serverHw);
        lease = got;
        state = Dhcp::Bound;
        leased = nowMs();
        ++leases;
        due = lease.seconds == forever ? -1 : t1();
        changed();
    } else if (state == Dhcp::Requesting && type == nak) {
        discovering();
    } else if (expecting && type == nak) {
        lost();
    }
    return true;
}

int dhcpTimer() {
    const long long now = nowMs();
    switch (state) {
        case Dhcp::Released:
            return -1;
        case Dhcp::Discovering:
        case Dhcp::Requesting:
            if (now >= due) {
                send(state == Dhcp::Requesting ? request : discover);
                due = now + resend;
            }
            break;
        case Dhcp::Bound:
            if (due < 0) {
                return -1;
            }
            if (now >= t1()) {
                state = Dhcp::Renewing;
                anew();
                asking(t2());
            }
            break;
        case Dhcp::Renewing:
            if (now >= t2()) {
                state = Dhcp::Rebinding;
                anew();
                asking(ends());
            } else if (now >= due) {
                asking(t2());
            }
            break;
        case Dhcp::Rebinding:
            if (now >= ends()) {
                lost();
            } else if (now >= due) {
                asking(ends());
            }
            break;
    }
    const long long left = due - nowMs();
    return int(left > 0 ? left : 0);
}

void dhcpRenew() {
    if (lease.address == 0) {
        return discovering();
    }
    state = Dhcp::Renewing;
    anew();
    asking(t2() > nowMs() ? t2() : nowMs() + leastWait);
}

void dhcpRelease() {
    if (lease.address != 0) {
        send(release);
    }
    lease = {};
    state = Dhcp::Released;
    due = -1;
    changed();
}

const Lease& dhcpLease() {
    return lease;
}

Dhcp dhcpState() {
    return state;
}

unsigned dhcpLeases() {
    return leases;
}
