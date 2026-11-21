//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "inet loopback: ok\n"

// Sockets of the network's family on the loopback, 127.0.0.1: a TCP
// listener of a port the stack chooses, a connection to it accepted, its
// bytes both ways and its end; datagrams to a bound UDP socket, which
// carry their sender's address. In the system the loopback is a card of
// its own, lo, whose stack sockmgr opens once it is up.

#include <arpa/inet.h>
#include <assert.h>
#include <netinet/in.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

namespace {
    sockaddr_in loopback() {
        sockaddr_in a{};
        a.sin_family = AF_INET;
        a.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        return a;
    }

    sockaddr* name(sockaddr_in& a) {
        return reinterpret_cast<sockaddr*>(&a);
    }

    // The loopback's stack known to sockmgr: the system's starts with it,
    // sockmgr opening its stack a moment later.
    void up() {
#ifdef __wasm__
        for (int i = 0; i < 100; ++i) {
            char text[1024] = {};
            FILE* ctl = fopen("/dev/sockmgr/ctl", "r");
            if (ctl) {
                fread(text, 1, sizeof text - 1, ctl);
                fclose(ctl);
            }
            if (strstr(text, "\nstack lo ")) {
                return;
            }
            usleep(50000);
        }
        assert(!"the loopback's stack");
#endif
    }
}

int main() {
    up();
    const int listener = socket(AF_INET, SOCK_STREAM, 0);
    assert(listener >= 0);
    sockaddr_in at = loopback();
    assert(bind(listener, name(at), sizeof at) == 0);
    assert(listen(listener, 4) == 0);
    socklen_t length = sizeof at;
    assert(getsockname(listener, name(at), &length) == 0 && at.sin_port != 0);
    assert(at.sin_addr.s_addr == htonl(INADDR_LOOPBACK));

    const int client = socket(AF_INET, SOCK_STREAM, 0);
    assert(client >= 0 && connect(client, name(at), sizeof at) == 0);
    sockaddr_in peer{};
    length = sizeof peer;
    const int served = accept(listener, name(peer), &length);
    assert(served >= 0 && peer.sin_addr.s_addr == htonl(INADDR_LOOPBACK) && peer.sin_port != 0);
    char got[16] = {};
    assert(write(client, "ping", 4) == 4 && read(served, got, sizeof got) == 4 && memcmp(got, "ping", 4) == 0);
    assert(write(served, "pong", 4) == 4 && read(client, got, sizeof got) == 4 && memcmp(got, "pong", 4) == 0);
    assert(close(client) == 0 && read(served, got, sizeof got) == 0);
    assert(close(served) == 0 && close(listener) == 0);

    const int box = socket(AF_INET, SOCK_DGRAM, 0);
    sockaddr_in to = loopback();
    assert(box >= 0 && bind(box, name(to), sizeof to) == 0);
    length = sizeof to;
    assert(getsockname(box, name(to), &length) == 0 && to.sin_port != 0);
    const int sender = socket(AF_INET, SOCK_DGRAM, 0);
    assert(sender >= 0 && sendto(sender, "dgram", 5, 0, name(to), sizeof to) == 5);
    sockaddr_in from{};
    length = sizeof from;
    assert(recvfrom(box, got, sizeof got, 0, name(from), &length) == 5 && memcmp(got, "dgram", 5) == 0);
    assert(from.sin_family == AF_INET && from.sin_addr.s_addr == htonl(INADDR_LOOPBACK) && from.sin_port != 0);
    assert(close(sender) == 0 && close(box) == 0);
    puts("inet loopback: ok");
}
