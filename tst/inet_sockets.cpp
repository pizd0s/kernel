// The sockets of the network's family in the system, through sockmgr and
// the card's stack: a TCP connection to a server of the host's, at
// 192.168.127.254, a text sent and its answer read; a socket that listens
// on the card's address and a connection to it, the card's own, accepted;
// a connect that does not wait, and poll of it; epoll's watch of the socket,
// made before connect, of sockmgr's, which sees its text, the stack's; the
// errors of a socket on no stack.
//
//     inet_sockets <port>

#include <arpa/inet.h>
#include <errno.h>
#include <fcntl.h>
#include <netinet/in.h>
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
}

int main(int argc, char** argv) {
    if (argc != 2) {
        return 2;
    }
    const int port = atoi(argv[1]);
    setvbuf(stdout, nullptr, _IONBF, 0);

    const int s = socket(AF_INET, SOCK_STREAM, 0);
    check("socket", s);
    const sockaddr_in host = at("192.168.127.254", port);
    check("connect", connect(s, reinterpret_cast<const sockaddr*>(&host), sizeof host));
    sockaddr_in self{};
    socklen_t length = sizeof self;
    check("getsockname", getsockname(s, reinterpret_cast<sockaddr*>(&self), &length));
    char text[INET_ADDRSTRLEN];
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
    return 0;
}
