#include <bin/inet/host.h>
#include <bin/inet/stack.h>

#include <arpa/inet.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

#include "ff_api.h"
#include "ff_config.h"

extern "C" {
#include "ff_veth.h"

int ff_freebsd_init(void);
void ff_hardclock_ticks(int n);
int ff_callout_next(void);
void ff_update_current_ts(void);
void ff_tcp_hpts_softclock(void);
void ff_swi_net_excute(void);
int inet_address(const char* ifname, uint32_t old, uint32_t oldRouter, uint32_t address, uint32_t mask, uint32_t router);
}

namespace {
    constexpr int hz = 100;

    struct ff_port_cfg port;
    char name[64];
    char address[INET_ADDRSTRLEN];
    char netmask[INET_ADDRSTRLEN];
    char broadcast[INET_ADDRSTRLEN];
    char gateway[INET_ADDRSTRLEN];
    bool started;
    long long start; // ms the ticks are counted from
    long long ticked;

    long long nowMs() {
        timespec ts;
        clock_gettime(CLOCK_MONOTONIC, &ts);
        return (long long)ts.tv_sec * 1000 + ts.tv_nsec / 1000000;
    }

    void text(char* out, uint32_t a) {
        inet_ntop(AF_INET, &a, out, INET_ADDRSTRLEN);
    }
}

void stackStart(int card, const char* named, const uint8_t* mac, const Lease& lease) {
    inetCard(card);
    ff_global_cfg.freebsd.hz = hz;
    ff_global_cfg.freebsd.physmem = 64ul << 20;
    if (ff_freebsd_init() != 0) {
        fprintf(stderr, "inet: the stack does not start\n");
        exit(1);
    }
    snprintf(name, sizeof name, "%s", named);
    text(address, lease.address);
    text(netmask, lease.mask);
    text(broadcast, lease.address | ~lease.mask);
    text(gateway, lease.router);
    port.name = name;
    port.ifname = name;
    memcpy(port.mac, mac, 6);
    port.addr = address;
    port.netmask = netmask;
    port.broadcast = broadcast;
    port.gateway = gateway;
    port.mtu = 1500;
    if (!ff_veth_attach(&port)) {
        fprintf(stderr, "inet: the card does not attach\n");
        exit(1);
    }
    start = nowMs();
    started = true;
}

int stackLease(const Lease& had, const Lease& lease) {
    if (had.address == lease.address && had.mask == lease.mask && had.router == lease.router) {
        return 0;
    }
    stackClock();
    return inet_address(name, had.address, had.router, lease.address, lease.mask, lease.router);
}

void stackClock() {
    if (!started) {
        return;
    }
    const long long due = (nowMs() - start) / (1000 / hz);
    if (due > ticked) {
        ff_hardclock_ticks(int(due - ticked));
        ticked = due;
        ff_update_current_ts();
    }
}

void stackFrames() {
    stackClock();
    if (inetReceive() < 0) {
        fprintf(stderr, "inet: the card is gone\n");
        exit(1);
    }
    ff_tcp_hpts_softclock();
    ff_swi_net_excute();
}

int stackTicks() {
    stackClock();
    ff_tcp_hpts_softclock();
    ff_swi_net_excute();
    const int next = ff_callout_next();
    if (next < 0) {
        return -1;
    }
    const long long left = start + (ticked + next) * (1000 / hz) - nowMs();
    return int(left > 0 ? left : 0);
}
