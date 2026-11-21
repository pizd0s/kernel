//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "path sockets: ok\n"

// AF_UNIX sockets named by paths: of a connection, its listener's queue
// and accept, and of datagrams, which carry their sender's name. The
// system has no network: AF_INET and AF_INET6 are no domains of it.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <poll.h>
#include <stddef.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

static sockaddr_un named(const char* path) {
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    strcpy(address.sun_path, path);
    return address;
}

static sockaddr* raw(sockaddr_un& address) {
    return reinterpret_cast<sockaddr*>(&address);
}

int main() {
    assert(socket(-1, SOCK_STREAM, 0) == -1 && errno == EAFNOSUPPORT);
    assert(socket(AF_UNIX, -1, 0) == -1 && errno == EINVAL);
#ifdef __wasm__
    assert(socket(AF_INET6, SOCK_DGRAM, 0) == -1 && errno == EAFNOSUPPORT);
#endif
    int listener = socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK | SOCK_CLOEXEC, 0);
    assert(listener >= 0 && (fcntl(listener, F_GETFD) & FD_CLOEXEC));
    sockaddr_un address = named("stream.sock"), peer{};
    assert(bind(listener, raw(address), sizeof(address)) == 0);
    socklen_t length = sizeof(peer);
    assert(getsockname(listener, raw(peer), &length) == 0 && length == offsetof(sockaddr_un, sun_path) + strlen("stream.sock") + 1);
    assert(strcmp(peer.sun_path, "stream.sock") == 0);
    length = sizeof(peer);
    assert(getpeername(listener, raw(peer), &length) == -1 && errno == ENOTCONN);
    int refused = socket(AF_UNIX, SOCK_STREAM, 0);
    assert(refused >= 0 && connect(refused, raw(address), sizeof(address)) == -1 && errno == ECONNREFUSED);
    assert(close(refused) == 0);
    int other = socket(AF_UNIX, SOCK_SEQPACKET, 0);
    assert(other >= 0 && connect(other, raw(address), sizeof(address)) == -1 && errno == EPROTOTYPE);
    assert(close(other) == 0);
    assert(listen(listener, 4) == 0);
    assert(accept(listener, nullptr, nullptr) == -1 && (errno == EAGAIN || errno == EWOULDBLOCK));
    int client = socket(AF_UNIX, SOCK_STREAM, 0);
    assert(client >= 0 && connect(client, raw(address), sizeof(address)) == 0);
    length = sizeof(peer);
    int accepted = accept4(listener, raw(peer), &length, SOCK_CLOEXEC | SOCK_NONBLOCK);
    assert(accepted >= 0 && length == sizeof(sa_family_t) && peer.sun_family == AF_UNIX);
    assert((fcntl(accepted, F_GETFD) & FD_CLOEXEC) && (fcntl(accepted, F_GETFL) & O_NONBLOCK));
    sockaddr_un observed{};
    length = sizeof(observed);
    assert(getpeername(client, raw(observed), &length) == 0 && strcmp(observed.sun_path, "stream.sock") == 0);
    int type = 0;
    length = sizeof(type);
    assert(getsockopt(accepted, SOL_SOCKET, SO_TYPE, &type, &length) == 0 && type == SOCK_STREAM);
    assert(sockatmark(accepted) == 0);
    assert(sockatmark(-1) == -1 && errno == EBADF);
    assert(send(client, "hello", 5, 0) == 5);
    pollfd ready{accepted, POLLIN, 0};
    assert(poll(&ready, 1, 1000) == 1);
    char text[32];
    assert(recv(accepted, text, sizeof(text), 0) == 5 && memcmp(text, "hello", 5) == 0);
    assert(send(accepted, "ack", 3, 0) == 3);
    assert(recv(client, text, 3, MSG_WAITALL) == 3 && memcmp(text, "ack", 3) == 0);
    assert(close(client) == 0);
    assert(recv(accepted, text, sizeof(text), 0) == 0);
    assert(close(accepted) == 0);

    client = socket(AF_UNIX, SOCK_STREAM | SOCK_NONBLOCK, 0);
    assert(client >= 0 && connect(client, raw(address), sizeof(address)) == 0);
    ready = {client, POLLOUT, 0};
    assert(poll(&ready, 1, 1000) == 1);
    int error = -1;
    length = sizeof(error);
    assert(getsockopt(client, SOL_SOCKET, SO_ERROR, &error, &length) == 0 && error == 0);
    accepted = accept(listener, nullptr, nullptr);
    assert(accepted >= 0 && close(accepted) == 0 && close(client) == 0 && close(listener) == 0);
    client = socket(AF_UNIX, SOCK_STREAM, 0);
    assert(client >= 0 && connect(client, raw(address), sizeof(address)) == -1 && errno == ECONNREFUSED);
    assert(close(client) == 0 && unlink("stream.sock") == 0);

    int server = socket(AF_UNIX, SOCK_DGRAM | SOCK_CLOEXEC, 0);
    client = socket(AF_UNIX, SOCK_DGRAM, 0);
    assert(server >= 0 && client >= 0);
    sockaddr_un serverName = named("server.sock"), clientName = named("client.sock");
    assert(bind(server, raw(serverName), sizeof(serverName)) == 0);
    assert(bind(client, raw(clientName), sizeof(clientName)) == 0);
    sockaddr_un otherName = named("other.sock");
    assert(bind(client, raw(otherName), sizeof(otherName)) == -1 && errno == EINVAL);
    assert(access("other.sock", F_OK) == -1 && errno == ENOENT);
    length = sizeof(type);
    assert(getsockopt(server, SOL_SOCKET, SO_TYPE, &type, &length) == 0 && type == SOCK_DGRAM);
    assert(send(client, "nowhere", 7, 0) == -1 && errno == ENOTCONN);
    assert(sendto(client, "datagram", 8, 0, raw(serverName), sizeof(serverName)) == 8);
    length = sizeof(peer);
    peer = {};
    assert(recvfrom(server, text, sizeof(text), 0, raw(peer), &length) == 8);
    assert(memcmp(text, "datagram", 8) == 0 && strcmp(peer.sun_path, "client.sock") == 0);
    assert(length == offsetof(sockaddr_un, sun_path) + strlen("client.sock") + 1);
    iovec outgoing[] = {{const_cast<char*>("hello"), 5}, {const_cast<char*>("!"), 1}};
    msghdr message{};
    message.msg_name = &peer;
    message.msg_namelen = length;
    message.msg_iov = outgoing;
    message.msg_iovlen = 2;
    assert(sendmsg(server, &message, 0) == 6);
    sockaddr_un sender{};
    iovec incoming[] = {{text, sizeof(text)}};
    message = {};
    message.msg_name = &sender;
    message.msg_namelen = sizeof(sender);
    message.msg_iov = incoming;
    message.msg_iovlen = 1;
    assert(recvmsg(client, &message, 0) == 6 && memcmp(text, "hello!", 6) == 0);
    assert(strcmp(sender.sun_path, "server.sock") == 0);
    assert(recv(client, text, sizeof(text), MSG_DONTWAIT) == -1 && errno == EAGAIN);
    int unnamed = socket(AF_UNIX, SOCK_DGRAM, 0);
    assert(unnamed >= 0 && sendto(unnamed, "anon", 4, 0, raw(serverName), sizeof(serverName)) == 4);
    length = sizeof(peer);
    assert(recvfrom(server, text, sizeof(text), 0, raw(peer), &length) == 4 && length == 0);
    assert(close(unnamed) == 0);
    assert(connect(client, raw(serverName), sizeof(serverName)) == 0);
    length = sizeof(observed);
    assert(getpeername(client, raw(observed), &length) == 0 && strcmp(observed.sun_path, "server.sock") == 0);
    assert(write(client, "joined", 6) == 6);
    assert(read(server, text, sizeof(text)) == 6 && memcmp(text, "joined", 6) == 0);
    assert(close(server) == 0);
    assert(send(client, "gone", 4, 0) == -1 && errno == ECONNREFUSED);
    assert(close(client) == 0 && unlink("server.sock") == 0 && unlink("client.sock") == 0);
#ifdef __wasm__
    // The names of sockets are the VFS's: there is no abstract one.
    sockaddr_un abstract{};
    abstract.sun_family = AF_UNIX;
    strcpy(abstract.sun_path + 1, "abstract");
    server = socket(AF_UNIX, SOCK_DGRAM, 0);
    assert(server >= 0 && bind(server, raw(abstract), offsetof(sockaddr_un, sun_path) + 9) == -1 && errno == EOPNOTSUPP);
    assert(close(server) == 0);
#endif
    puts("path sockets: ok");
}
