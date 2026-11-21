// inet: the system's TCP/IP, FreeBSD's stack as F-Stack runs it on one
// thread, on the network device vm gives the system (vm run --net): the
// device is the stack's card, its address 192.168.127.2/24, its gateway
// vm's network's, 192.168.127.1. The stack keeps its time by ticks of hz
// a second, run between the frames.
//
//     inet probe <address> <port> <text>
//
// probe connects to a TCP address, sends the text, prints what comes
// back until the other side closes, and ends: the stack, end to end.

#include <bin/inet/host.h>

#include <errno.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>
#include <arpa/inet.h>
#include <netinet/in.h>

#include "ff_api.h"
#include "ff_config.h"

extern "C" {
#include "ff_veth.h"
}

extern "C" {
    int ff_freebsd_init(void);
    void ff_hardclock(void);
    void ff_update_current_ts(void);
    void ff_tcp_hpts_softclock(void);
    void ff_swi_net_excute(void);
}

namespace {
    constexpr int hz = 100;

    struct ff_port_cfg port;
    char portName[] = "net0";
    char address[] = "192.168.127.2";
    char netmask[] = "255.255.255.0";
    char broadcast[] = "192.168.127.255";
    char gateway[] = "192.168.127.1";

    long long nowMs() {
        timespec ts;
        clock_gettime(CLOCK_MONOTONIC, &ts);
        return (long long)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
    }

    // The device: after the disks DISKS names, which NETS names.
    int device() {
        if (!getenv("NETS")) {
            return -1;
        }
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

    void start(int card) {
        inetCard(card);
        ff_global_cfg.freebsd.hz = hz;
        ff_global_cfg.freebsd.physmem = 64ul << 20;
        if (ff_freebsd_init() != 0) {
            fprintf(stderr, "inet: the stack does not start\n");
            exit(1);
        }
        port.name = portName;
        port.ifname = portName;
        const unsigned char mac[6] = {0x5a, 0x94, 0xef, 0xe4, 0x0c, 0xee};
        memcpy(port.mac, mac, sizeof mac);
        port.addr = address;
        port.netmask = netmask;
        port.broadcast = broadcast;
        port.gateway = gateway;
        port.mtu = 1500;
        if (!ff_veth_attach(&port)) {
            fprintf(stderr, "inet: the card does not attach\n");
            exit(1);
        }
    }

    // One turn of the stack: the frames there are, or a wait for them up
    // to the next tick, and the ticks due.
    long long nextTick;

    void turn(int card, int waitMs) {
        const long long now = nowMs();
        if (nextTick == 0) {
            nextTick = now + 1000 / hz;
        }
        pollfd p{card, POLLIN, 0};
        const long long until = nextTick - now;
        poll(&p, 1, waitMs < until ? waitMs : (int)(until > 0 ? until : 0));
        if (inetReceive() < 0) {
            fprintf(stderr, "inet: the device is gone\n");
            exit(1);
        }
        for (long long at = nowMs(); at >= nextTick; nextTick += 1000 / hz) {
            ff_hardclock();
            ff_update_current_ts();
        }
        ff_tcp_hpts_softclock();
        ff_swi_net_excute();
    }

    int probe(int card, const char* host, int port, const char* text) {
        const int s = ff_socket(AF_INET, SOCK_STREAM, 0);
        if (s < 0) {
            printf("socket: %s\n", strerror(errno));
            return 1;
        }
        int on = 1;
        if (ff_ioctl(s, FIONBIO, &on) < 0) {
            printf("ioctl FIONBIO: %s\n", strerror(errno));
            return 1;
        }
        sockaddr_in to{};
        to.sin_family = AF_INET;
        to.sin_port = htons(uint16_t(port));
        inet_pton(AF_INET, host, &to.sin_addr);
        if (ff_connect(s, reinterpret_cast<linux_sockaddr*>(&to), sizeof to) < 0 && errno != EINPROGRESS) {
            printf("connect: %s\n", strerror(errno));
            return 1;
        }
        const long long deadline = nowMs() + 10000;
        size_t sent = 0;
        const size_t length = strlen(text);
        bool told = false;
        while (nowMs() < deadline) {
            turn(card, 10);
            if (sent < length) {
                const ssize_t n = ff_write(s, text + sent, length - sent);
                if (n < 0 && errno != EAGAIN && errno != ENOTCONN) {
                    printf("write: %s\n", strerror(errno));
                    return 1;
                }
                if (n > 0 && !told) {
                    printf("connected to %s:%d\n", host, port);
                    told = true;
                }
                sent += n > 0 ? size_t(n) : 0;
                continue;
            }
            char got[512];
            const ssize_t n = ff_read(s, got, sizeof got);
            if (n == 0) {
                printf("closed\n");
                ff_close(s);
                return 0;
            }
            if (n < 0 && errno != EAGAIN) {
                printf("read: %s\n", strerror(errno));
                return 1;
            }
            if (n > 0) {
                printf("received: %.*s\n", int(n), got);
            }
        }
        printf("timed out\n");
        return 1;
    }
}

int main(int argc, char** argv) {
    const int card = device();
    if (card < 0) {
        fprintf(stderr, "inet: no network device (vm run --net)\n");
        return 1;
    }
    if (argc == 5 && strcmp(argv[1], "probe") == 0) {
        start(card);
        return probe(card, argv[2], atoi(argv[3]), argv[4]);
    }
    fprintf(stderr, "usage: inet probe <address> <port> <text>\n");
    return 2;
}
