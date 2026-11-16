#pragma once

#include <lib/protocol/protocol.h>

// What the servers of the system share, the services of bin/tools and
// lib/fuse: registering a device at rootfs's /dev/init, answering a
// request, the -r descriptor they close once they are up, and the loop of
// their endpoints. The protocol is protocol.h's.

[[noreturn, gnu::format(printf, 1, 2)]] void fatal(const char* format, ...);

// A reply: the header, then data; a reader gone is its own loss.
void answer(int fd, uint32_t op, uint32_t arg, const void* data = nullptr, size_t size = 0);
void fail(int fd, int e);

// A device registered: ours of the pair rootfs hands its opens on to, and
// its stat, as rootfs has its name.
struct Device {
    int end;
    Stat st;
};

// Registers a device of name and mode, S_IFCHR, S_IFBLK or S_IFSOCK with
// the permissions, on an endpoint of /dev/init: the far end of a pair goes
// with the request, the device's stat comes back. 0, or the errno.
int enroll(int registry, const char* name, uint32_t mode, Device& out);

// Registers a device at /dev/init, or ends the service.
Device enroll(const char* program, const char* name, uint32_t mode);

// The options before a service's arguments: -r fd, the descriptor it
// closes once it is up, -1 without. The arguments left start at
// argv[first].
int readyDescriptor(int argc, char** argv, int& first);

// What an event of the loop is of: ours of a device's pair, where its
// opens come, or the endpoint of an open file of it, with what its open
// lets it do and what the service keeps of it; or a socket that listens,
// whose connections the loop accepts.
struct Endpoint {
    int fd;
    bool channel;
    bool reads;
    bool writes;
    void* of;
    bool listens = false;
};

Endpoint* watch(int epoll, Endpoint e);

// Lets go of an endpoint watch gave: it is closed, and no event names it.
void unwatch(int epoll, Endpoint* e);

// A request on an endpoint: its header and body, its descriptors, the
// reply's endpoint last, a memfd of a READ's or WRITE's before it.
struct Request {
    Header h;
    const unsigned char* body;
    size_t size;
    int fds[maxFds];
    size_t count;
    bool onEndpoint; // a question with no endpoint of its own: answered on the one it came on

    int reply() const;
    int window() const;
    bool copy(size_t offset, void* out, size_t length) const;

    template <typename T>
    bool at(size_t offset, T& out) const;
};

template <typename T>
bool Request::at(size_t offset, T& out) const {
    return copy(offset, &out, sizeof out);
}

// handle answers a request, true for an OPEN on a channel whose endpoint
// serve keeps; gone hears of an endpoint whose other end closed.
using Handle = bool (*)(Endpoint& e, Request& r);
using Gone = void (*)(Endpoint& e);
using Accepted = void (*)(Endpoint& e, int connection);

// Serves the endpoints of epoll while a device's channel or any endpoint
// is open, or until stopServing: when rootfs lets go of the devices, as
// the system ends, what is open of them is still served until it closes.
// Every request goes to handle, its descriptors closed after it but for
// those it set to -1. An OPEN on a channel, which devfs handed on, keeps
// its endpoint when handle says so, what it lets do from its flags. A
// request without an endpoint is a question, answered on the endpoint it
// came on. An endpoint whose other end closed goes to gone, then is closed.
// A socket that listens has its connections accepted, each to accepted.
//
// A READ or a WRITE that waits keeps its endpoint until handle answers it:
// handle sets its descriptor in r.fds to -1 and watches it, of saying what
// waits. A CANCEL comes on it as a question, and gone hears that the asker
// went; once answered, the endpoint is let go of (unwatch).
void serve(int epoll, Handle handle, Gone gone = nullptr, Accepted accepted = nullptr);

// Ends serve once the events it has are handled.
void stopServing();
