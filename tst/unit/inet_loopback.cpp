//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "inet loopback: ok\n"

// Sockets of the network's family on the loopback, 127.0.0.1: a TCP
// listener of a port the stack chooses, a connection to it accepted, its
// bytes both ways and its end; datagrams to a bound UDP socket, which
// carry their sender's address. A socket's timeouts: a receive that has
// waited SO_RCVTIMEO and a send that has waited SO_SNDTIMEO for room are
// EAGAIN, not sooner; what comes in the time is received; a timeout set
// before the socket has a stack holds too, and a timeout of 0 is none. In
// the system the loopback is a card of its own, lo, whose stack sockmgr
// opens once it is up.

#include <arpa/inet.h>
#include <assert.h>
#include <errno.h>
#include <netinet/in.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/wait.h>
#include <time.h>
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

    long long now() {
        timespec ts;
        clock_gettime(CLOCK_MONOTONIC, &ts);
        return ts.tv_sec * 1000000LL + ts.tv_nsec / 1000;
    }

    void timeout(int fd, int option, long us) {
        const timeval tv{us / 1000000, us % 1000000};
        assert(setsockopt(fd, SOL_SOCKET, option, &tv, sizeof tv) == 0);
    }

    // That a call waits 50 milliseconds, its socket's timeout, and is
    // EAGAIN.
    template <typename Call>
    void timesOut(Call call) {
        const long long start = now();
        errno = 0;
        assert(call() == -1 && errno == EAGAIN);
        assert(now() - start >= 50000);
    }

    // That a send does, once there is no room for it.
    template <typename Call>
    void fills(Call call) {
        long long start;
        ssize_t n;
        do {
            start = now();
            errno = 0;
            n = call();
        } while (n > 0);
        assert(n == -1 && errno == EAGAIN);
        assert(now() - start >= 50000);
    }

    // A byte a child writes to from, after a while.
    pid_t later(int from) {
        const pid_t child = fork();
        assert(child >= 0);
        if (child == 0) {
            usleep(150000);
            _exit(write(from, "l", 1) == 1 ? 0 : 1);
        }
        return child;
    }

    void ended(pid_t child) {
        int status;
        assert(waitpid(child, &status, 0) == child && WIFEXITED(status) && WEXITSTATUS(status) == 0);
    }

    void timeouts(int client, int served) {
        char got[16];
        timeout(client, SO_RCVTIMEO, 50000);
        timesOut([&] { return recv(client, got, sizeof got, 0); });
        timesOut([&] { return read(client, got, sizeof got); });

        // What comes in the time is received, and without one whenever it
        // does.
        const long waits[] = {60000000, 0};
        for (const long us : waits) {
            timeout(client, SO_RCVTIMEO, us);
            const pid_t child = later(served);
            assert(recv(client, got, sizeof got, 0) == 1 && got[0] == 'l');
            ended(child);
        }

        timeout(client, SO_SNDTIMEO, 50000);
        static char block[16384];
        fills([&] { return send(client, block, sizeof block, 0); });
        fills([&] { return write(client, block, sizeof block); });
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
    assert(close(served) == 0);

    const int timed = socket(AF_INET, SOCK_STREAM, 0);
    assert(timed >= 0 && connect(timed, name(at), sizeof at) == 0);
    const int far = accept(listener, nullptr, nullptr);
    assert(far >= 0);
    timeouts(timed, far);
    assert(close(timed) == 0 && close(far) == 0 && close(listener) == 0);

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
    timeout(box, SO_RCVTIMEO, 50000);
    timesOut([&] { return recvfrom(box, got, sizeof got, 0, nullptr, nullptr); });
    assert(close(sender) == 0 && close(box) == 0);

    const int early = socket(AF_INET, SOCK_DGRAM, 0);
    to = loopback();
    assert(early >= 0);
    timeout(early, SO_RCVTIMEO, 50000);
    assert(bind(early, name(to), sizeof to) == 0);
    timesOut([&] { return recv(early, got, sizeof got, 0); });
    assert(close(early) == 0);
    puts("inet loopback: ok");
}
