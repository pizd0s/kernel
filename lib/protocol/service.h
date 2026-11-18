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

// Tells the kernel, on an open file's endpoint, the events poll finds of
// it now (READY), and a terminal's foreground group and flags; its OPEN's
// OK said openTells.
void tell(int fd, uint32_t events, uint32_t foreground = 0, uint32_t flags = 0);

// A device registered: ours of the pair rootfs hands its opens on to, and
// its stat, as rootfs has its name.
struct Device {
    int end;
    Stat st;
};

// Registers a device of name and mode, S_IFCHR, S_IFBLK or S_IFSOCK with
// the permissions, its owner uid and gid, on an endpoint of /dev/init:
// the far end of a pair goes with the request, the device's stat comes
// back on the endpoint. 0, or the errno.
int enroll(int registry, const char* name, uint32_t mode, uint32_t uid, uint32_t gid, Device& out);

// Registers a device of the service's at /dev/init, or ends the service.
Device enroll(const char* program, const char* name, uint32_t mode);

// The options before a service's arguments: -r fd, the descriptor it
// closes once it is up, -1 without. The arguments left start at
// argv[first].
int readyDescriptor(int argc, char** argv, int& first);

// What an event of the loop is of: ours of a device's pair, where its
// opens come, each with where to answer it; or the endpoint of an open
// file of it, with what its open lets it do and what the service keeps of
// it, which who asks connects on; or a socket that listens, whose
// connections the loop accepts; or one whose requests are answered on it,
// answers, as a socket a program speaks the protocol on is. The loop's
// own are the connections to an open file, file theirs: the requests of
// the open file come on them and are answered there.
struct Endpoint {
    int fd;
    bool channel;
    bool reads;
    bool writes;
    void* of;
    bool listens = false;
    bool answers = false;
    Endpoint* file = nullptr;  // a connection's open file
    Endpoint* first = nullptr; // an open file's connections,
    Endpoint* after = nullptr; // each the next of its file's
    bool kept = false;         // a connection whose request waits (keep)
    Endpoint* next = nullptr;  // among those let go of, the loop's
};

Endpoint* watch(int epoll, Endpoint e);

// Lets go of an endpoint watch gave: it is closed, and no event names it.
void unwatch(int epoll, Endpoint* e);

// A request on an endpoint: its header and body, its descriptors, where
// to answer it last, a memfd of a READ's or WRITE's before it.
struct Request {
    Header h;
    const unsigned char* body;
    size_t size;
    int fds[maxFds];
    size_t count;
    bool onEndpoint;      // answered on the endpoint it came on, whose descriptor is the last
    Endpoint* connection; // the connection it came on, of an open file's

    int reply() const;
    int window() const;
    bool copy(size_t offset, void* out, size_t length) const;

    // A READ's or a WRITE's body, false for one that is not: just an Io,
    // whose flags are the op's.
    bool io(Io& out) const;

    template <typename T>
    bool at(size_t offset, T& out) const;
};

template <typename T>
bool Request::at(size_t offset, T& out) const {
    return copy(offset, &out, sizeof out);
}

// Answers an OPEN with a new open file: a pair whose second end goes
// with the OK to who asked, the open file what says it is, the OPEN's
// flags its; the first, watched in epoll with of, to read and write as
// the flags let it, is the open file's endpoint, which who asks connects
// on. That endpoint, or none, the error answered.
Endpoint* answerOpen(int epoll, const Request& r, Opened what, void* of);

// Keeps a request to answer it later, of saying what waits: the endpoint
// to answer it on, whose descriptor stays open, and which hears of it
// meanwhile, a CANCEL as a request to handle and the asker's going as
// gone, each with that endpoint, whose of is the one given. unkeep lets
// go of it once the request is answered.
Endpoint* keep(int epoll, Request& r, void* of);
void unkeep(int epoll, Endpoint* e);

// handle answers a request; gone hears of an endpoint whose other end
// closed.
using Handle = void (*)(Endpoint& e, Request& r);
using Gone = void (*)(Endpoint& e);
using Accepted = void (*)(Endpoint& e, int connection);
// expire runs the service's timers that are due and is how long serve may
// wait for the next, in milliseconds, -1 for none.
using Expire = int (*)();

// Serves the endpoints of epoll while a device's channel or any endpoint
// is open, or until stopServing: when rootfs lets go of the devices, as
// the system ends, what is open of them is still served until it closes.
// Every request goes to handle, its descriptors closed after it but for
// those it set to -1; on a channel only OPEN, which devfs handed on, and
// handle answers with answerOpen. A request that brings nowhere to answer
// is a question, answered on the endpoint it came on. An endpoint whose
// other end closed goes to gone, then is closed.
// A socket that listens has its connections accepted, each to accepted.
// Before each wait, expire runs what is due and bounds the wait.
//
// An open file's endpoint takes CONNECT, which the loop answers itself: a
// call pair, whose asking end goes with the OK and whose serving end is a
// connection of the open file's. A request on a connection goes to handle
// with the open file's endpoint, and is answered on the connection, which
// closes when who asked lets go of it, and with its open file.
//
// A READ or a WRITE that waits is kept until handle answers it (keep);
// the CANCEL of a request that is not kept is dropped.
void serve(int epoll, Handle handle, Gone gone = nullptr, Accepted accepted = nullptr, Expire expire = nullptr);

// Ends serve once the events it has are handled.
void stopServing();
