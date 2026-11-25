#pragma once

#include <lib/protocol/protocol.h>

// What the servers of the system share, the services of bin/tools and
// lib/fuse: registering a device at rootfs's /dev/init/ctl, answering a
// request, the -r descriptor they close once they are up, and the loop of
// their endpoints. The protocol is protocol.h's.

[[noreturn, gnu::format(printf, 1, 2)]] void fatal(const char* format, ...);

// A reply: the header, then data; a reader gone is its own loss.
void answer(int fd, uint32_t op, uint32_t arg, const void* data = nullptr, size_t size = 0);
void fail(int fd, int e);

// Tells the kernel, on an open file's endpoint, the events poll finds of
// it now (READY).
void tell(int fd, uint32_t events);


// A device registered: ours of the pair rootfs hands its opens on to, and
// its stat, as rootfs has its name.
struct Device {
    int end;
    Stat st;
};

// Registers a file of name and mode, S_IFCHR, S_IFBLK, S_IFSOCK or S_IFREG with
// the permissions, its owner uid and gid, on an endpoint of /dev/init/ctl:
// the far end of a pair goes with the request, the device's stat comes
// back on the endpoint. 0, or the errno.
int enroll(int registry, const char* name, uint32_t mode, uint32_t uid, uint32_t gid, Device& out);

// Registers a device of the service's at /dev/init/ctl, or ends the service.
Device enroll(const char* program, const char* name, uint32_t mode);

// Registers a socket of the service's at /dev/init/ctl, of name and the
// permissions of mode, and binds a socket of type to it, which listens:
// the listener, the name's end in registered, or the service ends. The
// name goes with the service, what is bound with the listener.
int enrollSocket(const char* program, const char* name, uint32_t mode, int type, Device& registered);

// The options before a service's arguments: -r fd, the descriptor it
// closes once it is up, -1 without. The arguments left start at
// argv[first].
int readyDescriptor(int argc, char** argv, int& first);

// What an event of the loop is of: ours of a device's pair, where its
// opens come, each with where to answer it; or the endpoint of an open
// file of it, with what its open lets it do and what the service keeps of
// it, where the open file's requests come, each with where to answer it;
// or a socket that listens, whose connections the loop accepts; or one
// whose requests are answered on it, answers, as a socket a program
// speaks the protocol on is; or where a request that is kept is answered
// (keep); or a descriptor of no protocol, raw, whose events go to the
// loop's raw handler, which reads it itself.
struct Endpoint {
    int fd;
    bool channel;
    bool reads;
    bool writes;
    void* of;
    bool listens = false;
    bool answers = false;
    bool raw = false;
    Endpoint* next = nullptr; // among those let go of, the loop's
    uint64_t pos = 0;         // an open file's position: READ's and WRITE's without ioAt
    bool append = false;      // an open file's O_APPEND and O_NONBLOCK, the OPEN's, then SETFL's
    bool nonblock = false;
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
    bool onEndpoint; // answered on the endpoint it came on, whose descriptor is the last

    int reply() const;
    int memfd() const;
    bool copy(size_t offset, void* out, size_t length) const;

    // A READ's or a WRITE's body, false for one that is not: just an Io,
    // whose flags are known.
    bool io(Io& out) const;

    template <typename T>
    bool at(size_t offset, T& out) const;
};

template <typename T>
bool Request::at(size_t offset, T& out) const {
    return copy(offset, &out, sizeof out);
}

// Where a READ or a WRITE of an open file goes: off with ioAt, else its
// position, or the file's end, size, for a WRITE that appends.
uint64_t offsetOf(const Endpoint& e, const Io& io, bool write, uint64_t size);

// The position a SEEK of an open file sets, from its position and the
// file's size, into out: 0, or the errno.
int seekOf(Endpoint& e, const Request& r, uint64_t size, uint64_t& out);

// Answers a SEEK of an open file of size.
void answerSeek(Endpoint& e, const Request& r, uint64_t size);

// Answers an OPEN with a new open file: a pair whose second end goes
// with the OK to who asked, the open file what says it is, the OPEN's
// flags its; the first, watched in epoll with of, to read and write as
// the flags let it, is the open file's endpoint, where its requests
// come. That endpoint, or none, the error answered.
Endpoint* answerOpen(int epoll, const Request& r, Opened what, void* of);

// Keeps a request to answer it later, of saying what waits: where it is
// answered becomes an endpoint of the loop's, its descriptor open, which
// hears of the request meanwhile, a CANCEL as a request to handle and the
// asker's going as gone, each with that endpoint, whose of is the one
// given. unkeep lets go of it once the request is answered: the close is
// the end of the answer.
Endpoint* keep(int epoll, Request& r, void* of);
void unkeep(int epoll, Endpoint* e);

// handle answers a request; gone hears of an endpoint whose other end
// closed.
using Handle = void (*)(Endpoint& e, Request& r);
using Gone = void (*)(Endpoint& e);
using Accepted = void (*)(Endpoint& e, int connection);
// expire runs the service's timers that are due and is how long serve may
// wait for the next, in milliseconds, -1 for none. raw hears of a raw
// endpoint's events.
using Expire = int (*)();
using Raw = void (*)(Endpoint& e);

// Serves the endpoints of epoll while a device's channel or any endpoint
// is open, or until stopServing: when rootfs lets go of the devices, as
// the system ends, what is open of them is still served until it closes.
// Every request goes to handle, its descriptors closed after it but for
// those it set to -1; on a channel only OPEN, which devfs handed on, and
// handle answers with answerOpen. A request that brings nowhere to answer
// is a question, answered on the endpoint it came on. An endpoint whose
// other end closed goes to gone, then is closed.
// A socket that listens has its connections accepted, each to accepted.
// Before each wait, expire runs what is due and bounds the wait. A raw
// endpoint's events go to raw, which reads it, the loop nothing of it.
//
// A request of an open file's comes on the file's endpoint and goes to
// handle with it; where it is answered is the last of its descriptors,
// and closing that ends the answer. GETFL and SETFL the loop answers:
// what the open lets the file do and the flags its endpoint keeps.
//
// A READ or a WRITE that waits is kept until handle answers it (keep).
void serve(int epoll, Handle handle, Gone gone = nullptr, Accepted accepted = nullptr, Expire expire = nullptr, Raw raw = nullptr);

// Ends serve once the events it has are handled.
void stopServing();
