// The system's interfaces, the loopback's and the card's, as if_nameindex
// and getifaddrs give them: their indexes, flags, links and addresses.
// The sockets of the network's family in the system, through sockmgr and
// the card's stack: a TCP connection to a server of the host's, at
// host.internal, 192.168.127.254, which getaddrinfo finds through dns and
// the gateway's name server, a text sent and its answer read; a socket that listens
// on the card's address and a connection to it, the card's own, accepted;
// a connect that does not wait, and poll of it; epoll's watch of the socket,
// made before connect, of sockmgr's, which sees its text, the stack's; the
// errors of a socket on no stack; a name the gateway has not, and
// gethostbyname's of one it has. The resolver: res_init's server, the
// card's, and search list, LOCALDOMAIN's; res_query's answer, a message
// ns_parserr reads, and the code of a name the gateway has not;
// res_search's name of the search list.
//
//     inet_sockets <port>

#include <arpa/inet.h>
#include <arpa/nameser.h>
#include <errno.h>
#include <fcntl.h>
#include <ifaddrs.h>
#include <net/if.h>
#include <netdb.h>
#include <netinet/in.h>
#include <netpacket/packet.h>
#include <resolv.h>
#include <poll.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/socket.h>
#include <unistd.h>

namespace {
    sockaddr_in at(const char* address, int port) {
        sockaddr_in a{};
        a.sin_family = AF_INET;
        a.sin_port = htons(uint16_t(port));
        inet_pton(AF_INET, address, &a.sin_addr);
        return a;
    }

    void check(const char* what, int r) {
        if (r < 0) {
            printf("%s: %s\n", what, strerror(errno));
            exit(1);
        }
    }

    // An interface's flags, those it may have, and an address of its entry.
    void describe(const ifaddrs* a) {
        static const struct {
            unsigned flag;
            const char* name;
        } flags[] = {{IFF_UP, "up"}, {IFF_BROADCAST, "broadcast"}, {IFF_LOOPBACK, "loopback"}, {IFF_RUNNING, "running"},
                     {IFF_MULTICAST, "multicast"}};
        printf("%s", a->ifa_name);
        for (const auto& f : flags) {
            if (a->ifa_flags & f.flag) {
                printf(" %s", f.name);
            }
        }
        char text[INET_ADDRSTRLEN];
        if (a->ifa_addr->sa_family == AF_PACKET) {
            const sockaddr* const ats[] = {a->ifa_addr, a->ifa_broadaddr};
            for (const sockaddr* at : ats) {
                const auto* l = reinterpret_cast<const sockaddr_ll*>(at);
                printf(at == a->ifa_addr ? " link %d" : " broadcast", l->sll_ifindex);
                for (int i = 0; i < l->sll_halen; ++i) {
                    printf("%c%02x", i == 0 ? ' ' : ':', l->sll_addr[i]);
                }
            }
        } else {
            printf(" inet %s", inet_ntop(AF_INET, &reinterpret_cast<const sockaddr_in*>(a->ifa_addr)->sin_addr, text, sizeof text));
            printf(" netmask %s", inet_ntop(AF_INET, &reinterpret_cast<const sockaddr_in*>(a->ifa_netmask)->sin_addr, text, sizeof text));
            if (a->ifa_broadaddr) {
                printf(" broadcast %s", inet_ntop(AF_INET, &reinterpret_cast<const sockaddr_in*>(a->ifa_broadaddr)->sin_addr, text, sizeof text));
            }
        }
        printf("\n");
    }

    void resolver() {
        setenv("LOCALDOMAIN", "internal", 1);
        res_init();
        char text[INET_ADDRSTRLEN];
        printf("res_init: %d server %s, search %s\n", _res.nscount, inet_ntop(AF_INET, &_res.nsaddr_list[0].sin_addr, text, sizeof text),
               _res.dnsrch[0]);
        unsigned char answer[NS_PACKETSZ];
        const int n = res_query("host.internal", C_IN, T_A, answer, sizeof answer);
        ns_msg msg;
        if (n < 0 || ns_initparse(answer, n, &msg) != 0) {
            printf("res_query: %s\n", hstrerror(h_errno));
            return;
        }
        for (int i = 0; i < ns_msg_count(msg, ns_s_an); ++i) {
            ns_rr rr;
            if (ns_parserr(&msg, ns_s_an, i, &rr) == 0 && ns_rr_type(rr) == ns_t_a) {
                printf("res_query: %s a %s\n", ns_rr_name(rr), inet_ntop(AF_INET, ns_rr_rdata(rr), text, sizeof text));
            }
        }
        const int none = res_query("nope.internal", C_IN, T_A, answer, sizeof answer);
        printf("res_query nope.internal: %d %s\n", none, hstrerror(h_errno));
        const int found = res_search("host", C_IN, T_A, answer, sizeof answer);
        printf("res_search host: %s\n", found > 0 ? "found" : hstrerror(h_errno));
    }

    void interfaces() {
        struct if_nameindex* all = if_nameindex();
        for (const struct if_nameindex* i = all; i && i->if_index != 0; ++i) {
            printf("interface %u %s\n", i->if_index, i->if_name);
        }
        if_freenameindex(all);
        ifaddrs* list = nullptr;
        check("getifaddrs", getifaddrs(&list));
        for (const ifaddrs* a = list; a; a = a->ifa_next) {
            describe(a);
        }
        freeifaddrs(list);
    }
}

int main(int argc, char** argv) {
    if (argc != 2) {
        return 2;
    }
    const int port = atoi(argv[1]);
    setvbuf(stdout, nullptr, _IONBF, 0);
    interfaces();

    addrinfo hints{};
    hints.ai_family = AF_INET;
    hints.ai_socktype = SOCK_STREAM;
    hints.ai_flags = AI_CANONNAME;
    char service[16];
    snprintf(service, sizeof service, "%d", port);
    addrinfo* found = nullptr;
    if (const int e = getaddrinfo("host.internal", service, &hints, &found); e != 0) {
        printf("getaddrinfo: %s\n", gai_strerror(e));
        return 1;
    }
    char text[INET_ADDRSTRLEN];
    const auto* host = reinterpret_cast<const sockaddr_in*>(found->ai_addr);
    printf("host.internal is %s, %s\n", inet_ntop(AF_INET, &host->sin_addr, text, sizeof text), found->ai_canonname);
    const int s = socket(found->ai_family, found->ai_socktype, found->ai_protocol);
    check("socket", s);
    check("connect", connect(s, found->ai_addr, found->ai_addrlen));
    freeaddrinfo(found);
    sockaddr_in self{};
    socklen_t length = sizeof self;
    check("getsockname", getsockname(s, reinterpret_cast<sockaddr*>(&self), &length));
    printf("connected from %s\n", inet_ntop(AF_INET, &self.sin_addr, text, sizeof text));
    check("write", int(write(s, "hello", 5)));
    char got[64] = {};
    const ssize_t n = read(s, got, sizeof got - 1);
    check("read", int(n));
    printf("read %s\n", got);
    printf("read at the end %zd\n", read(s, got, sizeof got));
    close(s);

    const int l = socket(AF_INET, SOCK_STREAM, 0);
    const sockaddr_in local = at("192.168.127.2", 7000);
    check("bind", bind(l, reinterpret_cast<const sockaddr*>(&local), sizeof local));
    check("listen", listen(l, 4));
    const int c = socket(AF_INET, SOCK_STREAM | SOCK_NONBLOCK, 0);
    const int watch = epoll_create1(0);
    epoll_event want{};
    want.events = EPOLLIN;
    want.data.fd = c;
    check("epoll_ctl", epoll_ctl(watch, EPOLL_CTL_ADD, c, &want));
    const int started = connect(c, reinterpret_cast<const sockaddr*>(&local), sizeof local);
    printf("connect that does not wait: %s\n", started == 0 ? "done" : strerror(errno));
    pollfd p{c, POLLOUT, 0};
    printf("poll: %d %s\n", poll(&p, 1, 5000), (p.revents & POLLOUT) != 0 ? "POLLOUT" : "-");
    sockaddr_in peer{};
    length = sizeof peer;
    const int a = accept(l, reinterpret_cast<sockaddr*>(&peer), &length);
    check("accept", a);
    printf("accepted from %s\n", inet_ntop(AF_INET, &peer.sin_addr, text, sizeof text));
    check("send", int(send(c, "ping", 4, 0)));
    memset(got, 0, sizeof got);
    check("recv", int(recv(a, got, sizeof got - 1, 0)));
    printf("recv %s\n", got);
    epoll_event seen{};
    printf("epoll before the answer: %d\n", epoll_wait(watch, &seen, 1, 0));
    check("answer", int(write(a, "pong", 4)));
    const int ready = epoll_wait(watch, &seen, 1, 5000);
    printf("epoll: %d %s\n", ready, ready == 1 && seen.data.fd == c && (seen.events & EPOLLIN) != 0 ? "EPOLLIN" : "-");
    memset(got, 0, sizeof got);
    check("read the answer", int(read(c, got, sizeof got - 1)));
    printf("read %s\n", got);
    close(watch);
    close(a);
    close(c);
    close(l);

    const int u = socket(AF_INET, SOCK_STREAM, 0);
    errno = 0;
    printf("read of a socket on no stack: %zd %s\n", read(u, got, 1), strerror(errno));
    const sockaddr_in other = at("10.1.2.3", 1);
    errno = 0;
    printf("bind to no card's address: %d %s\n", bind(u, reinterpret_cast<const sockaddr*>(&other), sizeof other), strerror(errno));
    close(u);
    errno = 0;
    printf("socket of AF_INET6: %d %s\n", socket(AF_INET6, SOCK_STREAM, 0) < 0 ? -1 : 0, strerror(errno));

    addrinfo* none = nullptr;
    printf("nope.internal: %s\n", gai_strerror(getaddrinfo("nope.internal", nullptr, &hints, &none)));
    const hostent* gateway = gethostbyname("Gateway.Internal");
    printf("gateway.internal: %s\n", gateway ? inet_ntoa(*reinterpret_cast<const in_addr*>(gateway->h_addr_list[0])) : "-");
    resolver();
    return 0;
}
