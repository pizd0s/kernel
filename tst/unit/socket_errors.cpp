//@ compile-flags: -g -D_GNU_SOURCE -fno-builtin
//@ stdout: "socket errors: ok\n"

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stddef.h>
#include <stdio.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/un.h>
#include <unistd.h>

int main() {
    sockaddr_storage address{};
    socklen_t length = sizeof(address);
    assert(getsockname(-1, reinterpret_cast<sockaddr*>(&address), &length) == -1 && errno == EBADF);
    assert(getpeername(-1, reinterpret_cast<sockaddr*>(&address), &length) == -1 && errno == EBADF);
    assert(bind(-1, reinterpret_cast<sockaddr*>(&address), length) == -1 && errno == EBADF);
    msghdr message{};
    assert(sendmsg(-1, &message, 0) == -1 && errno == EBADF);
    assert(recvmsg(-1, &message, 0) == -1 && errno == EBADF);
    int pair[2] = {-1, -1};
    assert(socketpair(AF_INET, SOCK_STREAM, 0, pair) == -1 && errno == EOPNOTSUPP);
    assert(socketpair(AF_UNIX, -1, 0, pair) == -1 && errno == EINVAL);

    int local = socket(AF_UNIX, SOCK_DGRAM, 0);
    assert(local >= 0);
    int value = 0;
    length = sizeof(value);
    assert(getsockopt(local, SOL_SOCKET, 0x7fffffff, &value, &length) == -1 && errno == ENOPROTOOPT);
    assert(sockatmark(local) == -1 && errno == EOPNOTSUPP);
    unsigned char tooLarge[129]{};
    tooLarge[0] = AF_UNIX;
    assert(bind(local, reinterpret_cast<sockaddr*>(tooLarge), sizeof(tooLarge)) == -1 && errno == EINVAL);
    sockaddr_un name{};
    name.sun_family = AF_UNIX;
    assert(connect(local, reinterpret_cast<sockaddr*>(&name), offsetof(sockaddr_un, sun_path)) == -1 && errno == EINVAL);
    strcpy(name.sun_path, "missing/socket");
    assert(connect(local, reinterpret_cast<sockaddr*>(&name), sizeof(name)) == -1 && errno == ENOENT);
    assert(sendto(local, "x", 1, 0, reinterpret_cast<sockaddr*>(&name), sizeof(name)) == -1 && errno == ENOENT);
    assert(listen(local, 1) == -1 && errno == EOPNOTSUPP);
#ifdef __wasm__
    // A name is one of the VFS's: there is no abstract one.
    memset(name.sun_path, 0, sizeof(name.sun_path));
    memset(name.sun_path + 1, 'x', sizeof(name.sun_path) - 1);
    assert(bind(local, reinterpret_cast<sockaddr*>(&name), sizeof(name)) == -1 && errno == EOPNOTSUPP);
#endif
    assert(close(local) == 0);

    rlimit saved{};
    assert(getrlimit(RLIMIT_NOFILE, &saved) == 0);
    rlimit limited = saved;
    for (rlim_t count = 3; count != 5; ++count) {
        limited.rlim_cur = count;
        assert(setrlimit(RLIMIT_NOFILE, &limited) == 0);
        assert(socketpair(AF_UNIX, SOCK_STREAM, 0, pair) == -1 && errno == EMFILE);
        if (count == 4) {
            int fd = socket(AF_UNIX, SOCK_DGRAM, 0);
            assert(fd == 3 && close(fd) == 0);
        }
    }
    assert(setrlimit(RLIMIT_NOFILE, &saved) == 0);

    int listener = socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK, 0);
    sockaddr_un endpoint{};
    endpoint.sun_family = AF_UNIX;
    strcpy(endpoint.sun_path, "listener.sock");
    assert(listener >= 0 && bind(listener, reinterpret_cast<sockaddr*>(&endpoint), sizeof(endpoint)) == 0);
    sockaddr_un second = endpoint;
    strcpy(second.sun_path, "second.sock");
    assert(bind(listener, reinterpret_cast<sockaddr*>(&second), sizeof(second)) == -1 && errno == EINVAL);
    int again = socket(AF_UNIX, SOCK_STREAM, 0);
    assert(again >= 0 && bind(again, reinterpret_cast<sockaddr*>(&endpoint), sizeof(endpoint)) == -1 && errno == EADDRINUSE);
    assert(close(again) == 0 && listen(listener, 2) == 0);
    int clients[2];
    for (int& client : clients) {
        client = socket(AF_UNIX, SOCK_STREAM, 0);
        assert(client >= 0 && connect(client, reinterpret_cast<sockaddr*>(&endpoint), sizeof(endpoint)) == 0);
    }
    limited.rlim_cur = 3;
    assert(setrlimit(RLIMIT_NOFILE, &limited) == 0);
    assert(accept(listener, nullptr, nullptr) == -1 && errno == EMFILE);
    assert(setrlimit(RLIMIT_NOFILE, &saved) == 0);
    // EMFILE took no connection: both are there to accept.
    for (int i = 0; i != 2; ++i) {
        int accepted = accept(listener, nullptr, nullptr);
        assert(accepted >= 0 && close(accepted) == 0);
    }
    assert(accept(listener, nullptr, nullptr) == -1 && errno == EAGAIN);
    for (int client : clients) {
        assert(close(client) == 0);
    }
    assert(close(listener) == 0 && unlink("listener.sock") == 0);
    assert(socketpair(AF_UNIX, SOCK_STREAM, 0, pair) == 0);
    message.msg_iovlen = 1025;
    assert(sendmsg(pair[0], &message, 0) == -1 && (errno == EINVAL || errno == EMSGSIZE));
    assert(recvmsg(pair[0], &message, MSG_DONTWAIT) == -1 && (errno == EINVAL || errno == EMSGSIZE));
    timeval timeout{0, 10000};
    assert(setsockopt(pair[0], SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout)) == 0);
    assert(send(pair[1], "x", 1, 0) == 1);
    char received[2]{};
    assert(recv(pair[0], received, sizeof(received), MSG_WAITALL) == 1 && received[0] == 'x');
    assert(recv(pair[0], received, 1, 0) == -1 && (errno == EAGAIN || errno == EWOULDBLOCK));
    assert(close(pair[0]) == 0 && close(pair[1]) == 0);
    puts("socket errors: ok");
}
