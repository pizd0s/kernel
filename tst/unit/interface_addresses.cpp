//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "interface addresses: ok\n"

// The system's interfaces, as Linux's are: if_nameindex lists each with its
// index, which if_nametoindex and if_indextoname agree with, and
// if_indextoname fails with ENXIO for an index of none; getifaddrs gives
// each interface's link, an AF_PACKET address of its index, and its IPv4
// addresses. Every system has the loopback, lo: IFF_UP, IFF_LOOPBACK and
// IFF_RUNNING, its link of ARPHRD_LOOPBACK and six bytes of zero, its
// address 127.0.0.1 of the mask 255.0.0.0. An interface's name as the zone
// of an IPv6 address is its index. lib/c's alone: lo's IPv4
// address has no broadcast address, as glibc's has not; musl puts the
// address there again.

#include <arpa/inet.h>
#include <assert.h>
#include <errno.h>
#include <ifaddrs.h>
#include <net/if.h>
#include <net/if_arp.h>
#include <netdb.h>
#include <netinet/in.h>
#include <netpacket/packet.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>

int main() {
    const unsigned lo = if_nametoindex("lo");
    assert(lo != 0);
    char name[IF_NAMESIZE];
    assert(if_indextoname(lo, name) == name && strcmp(name, "lo") == 0);
    errno = 0;
    assert(if_indextoname(0, name) == nullptr && errno == ENXIO);
    assert(if_nametoindex("no-such-interface") == 0);
    addrinfo hints{};
    hints.ai_family = AF_INET6;
    hints.ai_flags = AI_NUMERICHOST;
    addrinfo* found = nullptr;
    assert(getaddrinfo("fe80::1%lo", "0", &hints, &found) == 0);
    assert(reinterpret_cast<sockaddr_in6*>(found->ai_addr)->sin6_scope_id == lo);
    freeaddrinfo(found);

    struct if_nameindex* all = if_nameindex();
    assert(all);
    bool listed = false;
    for (struct if_nameindex* i = all; i->if_index != 0; ++i) {
        assert(if_nametoindex(i->if_name) == i->if_index);
        listed = listed || (i->if_index == lo && strcmp(i->if_name, "lo") == 0);
    }
    assert(listed);
    if_freenameindex(all);

    struct ifaddrs* list = nullptr;
    assert(getifaddrs(&list) == 0 && list);
    bool link = false;
    bool inet = false;
    for (struct ifaddrs* a = list; a; a = a->ifa_next) {
        assert(a->ifa_name);
        if (!a->ifa_addr) {
            continue;
        }
        if (a->ifa_addr->sa_family == AF_PACKET) {
            const auto* l = reinterpret_cast<const sockaddr_ll*>(a->ifa_addr);
            assert(unsigned(l->sll_ifindex) == if_nametoindex(a->ifa_name));
        }
        if (strcmp(a->ifa_name, "lo") != 0) {
            continue;
        }
        const unsigned up = IFF_UP | IFF_LOOPBACK | IFF_RUNNING;
        assert((a->ifa_flags & up) == up);
        if (a->ifa_addr->sa_family == AF_PACKET) {
            const auto* l = reinterpret_cast<const sockaddr_ll*>(a->ifa_addr);
            static const unsigned char zero[6] = {};
            assert(l->sll_hatype == ARPHRD_LOOPBACK && l->sll_halen == 6 && memcmp(l->sll_addr, zero, 6) == 0);
            link = true;
        } else if (a->ifa_addr->sa_family == AF_INET) {
            const auto* in = reinterpret_cast<const sockaddr_in*>(a->ifa_addr);
            const auto* mask = reinterpret_cast<const sockaddr_in*>(a->ifa_netmask);
            if (in->sin_addr.s_addr == htonl(INADDR_LOOPBACK)) {
                assert(mask && mask->sin_family == AF_INET && mask->sin_addr.s_addr == htonl(0xff000000));
#ifdef __wasm__
                assert(a->ifa_broadaddr == nullptr);
#endif
                inet = true;
            }
        }
    }
    assert(link && inet);
    freeifaddrs(list);
    printf("interface addresses: ok\n");
}
