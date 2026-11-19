#pragma once

#include <lib/protocol/protocol.h>

#include <stddef.h>
#include <stdint.h>
#include <string.h>
#include <sys/uio.h>

// What the file systems of the tools share, erofs's and tmpfs's: memory
// that is there or ends the program, bin/aot/aot.cpp's containers over
// malloc, the names and bodies of requests, their replies, and the loop
// of their endpoints, rootfs's channel and the open files, which hands
// what comes on them to the file system's calls. A file system's own are
// its nodes and those calls.
//
// Memory. A request is received into one buffer and answered before the
// next: its names are read where they are in it. What a reply assembles
// goes into one buffer of its own, out.

// Placement new expressions are lowered directly by Clang; no runtime
// call uses this overload. Its declaration supplies the freestanding ABI.
void* operator new(size_t, void*) noexcept;

namespace tools {
    using u8 = uint8_t;
    using u32 = uint32_t;
    using u64 = uint64_t;
    using i64 = int64_t;
    using usize = size_t;

    constexpr usize maxPacket = 65536;
    constexpr u64 blockSize = 4096;

    [[noreturn, gnu::format(printf, 1, 2)]] void fatal(const char* format, ...);

    // Sizes that do not overflow and memory that is there, or the program
    // ends.
    usize checkedAdd(usize a, usize b);
    usize checkedMultiply(usize a, usize b);
    void* allocate(usize size);
    void* reallocate(void* p, usize size);

    // ---- containers, bin/aot/aot.cpp's over malloc ----------------------------------

    constexpr usize objectAlignment = 16;

    struct Buffer {
        struct alignas(objectAlignment) Header {
            usize used;
            usize allocation;
        };

        constexpr Buffer();
        ~Buffer();
        Buffer(const Buffer&) = delete;
        Buffer& operator=(const Buffer&) = delete;
        void* data();
        const void* data() const;
        usize size() const;
        usize capacity() const;
        void reserve(usize size);
        void grow(usize size);
        void resize(usize size);
        void append(const void* data, usize size);
        void clear();
        void swap(Buffer& other);

        Header* header() const;
        void* storage;
    };

    // What a buffer of no bytes and no memory points past.
    extern const Buffer::Header emptyBuffer;

    template <typename T>
    struct SmallVec {
        static_assert(__is_trivially_copyable(T));
        static_assert(__is_trivially_destructible(T));
        static_assert(alignof(T) <= objectAlignment);

        constexpr SmallVec() = default;
        SmallVec(const SmallVec&) = delete;
        SmallVec& operator=(const SmallVec&) = delete;

        T* data();
        const T* data() const;
        usize size() const;
        T& operator[](usize index);
        const T& operator[](usize index) const;
        void resize(usize count);
        void clear();
        void pushBack(T value);

        // Keeps the elements keep takes, in their order.
        template <typename F>
        void keepIf(F keep);

        Buffer buffer;
    };

    // ---- names and bodies ------------------------------------------------------------

    // Bytes of a packet, of the image or of a node, where they are.
    struct Name {
        const char* at = nullptr;
        usize length = 0;

        bool empty() const;
        bool is(const char* text) const;
        bool has(char c) const;
    };

    // A request's body, read from its start.
    struct Body {
        const u8* at;
        const u8* end;

        usize size() const;
        template <typename T>
        T take();

        // A string up to its NUL, or the end, and past the NUL.
        Name string();
        Name rest();
    };

    // A READ's or a WRITE's body, false for one that is not: just an Io,
    // whose flags are the op's.
    bool ioOf(const Header& h, Body body, Io& out);

    // Why a name is none a call takes, 0 for one it does: "." and ".." and
    // names with "/" are a walk's; a name past NAME_MAX is too long.
    int badName(Name name);

    // ---- replies ------------------------------------------------------------------------

    // The descriptors of the request the loop handles, a memfd of a READ's
    // or a WRITE's first, where it is answered last; one a call keeps it
    // sets to -1. And the bytes a reply assembles: one of each for the
    // process, whose loop takes a request and is done with it before the
    // next, and sends each reply whole.
    extern int packetFds[maxFds];
    extern usize packetFdCount;
    extern u8 out[maxPacket - sizeof(Header)];

    // A reply: the header, then the parts where they are, and descriptors.
    void reply(int fd, u32 op, u32 arg, const iovec* parts, usize count, const int* fds = nullptr, usize fdCount = 0);
    iovec part(const void* data, usize size);
    void ok(int fd, const void* data = nullptr, usize size = 0);

    // A reply of an error.
    void error(int fd, int e);

    // The reply of a READ: how many bytes went into its window.
    void readCount(int fd, usize count);

    // The reply of a call that changes something: OK, or the error.
    void answer(int fd, int e);

    // The memfd of the memory a READ fills or a WRITE takes from: the
    // first of the request's descriptors, before where it is answered; -1
    // when it brought none.
    int window();

    // What a reply assembles in out.
    struct Assembly {
        usize size = 0;

        bool fits(usize n) const;
        void add(const void* data, usize n);
        template <typename T>
        void add(T value);
    };

    // ---- what the file systems' calls share ----------------------------------------

    // A device for a file system's files: the number of a pair, which no
    // other pair in the kernel has, as it never gives a number twice.
    u64 device();

    // ---- the loop's endpoints ----------------------------------------------------------

    // What an endpoint is to the loop.
    enum class Role : u8 {
        File,    // an open file: its requests come on it
        Channel, // rootfs's channel: its calls by node come on it
    };

    struct Endpoint {
        int fd;
        bool reads; // what its open lets it do
        bool writes;
        Role role;
        bool dead;       // closed, while a batch of events may still name it
        void* node;      // what a File is open on: the file system's
        Endpoint* next;  // among the free ones
        Endpoint* after; // among the live ones
        Endpoint* before;
    };

    // A file system's calls: rootfs's on its channel, by node, and those
    // on an open file, each answered on fd; and what is the file system's
    // of an endpoint that closes.
    using FsCall = void (*)(const Header& h, Body body, int fd);
    using FileCall = void (*)(Endpoint* ep, const Header& h, Body body, int fd);
    using Closing = void (*)(Endpoint* ep);

    // An endpoint of a role on a descriptor of ours, an open file's open on
    // a node to read, to write or both.
    Endpoint* listen(int fd, Role role, void* node, bool reads, bool writes);

    // Answers an open on fd, of flags, with a new open file of node: a
    // pair whose second end goes with the OK to who asked, an openedFile
    // of the file's dev and ino, which the kernel keeps its locks by; the
    // first, an endpoint of the loop's, to read, to write or both, is the
    // open file's, where its requests come. That endpoint, or none, the
    // error answered.
    Endpoint* openFile(int fd, u32 flags, void* node, u64 dev, u64 ino, bool reads, bool writes);

    // Serves the endpoints until the last of them closes: rootfs's channel,
    // and the open files after it. A question of rootfs's, which brings
    // nowhere to answer, is answered on its channel, in order. Every
    // other request brings where it is answered, the last of its
    // descriptors: a call rootfs hands on, whose open's open file goes
    // there (openFile), and a request of an open file's, which comes on
    // the file's endpoint. The descriptors of a request close after it,
    // but for those the call set to -1; the close of the last is the end
    // of the answer.
    void serve(FsCall fsCall, FileCall fileCall, Closing closing);
}

constexpr tools::Buffer::Buffer()
    : storage(const_cast<Header*>(&emptyBuffer) + 1)
{
}

inline void* tools::Buffer::data() {
    return storage;
}

inline const void* tools::Buffer::data() const {
    return storage;
}

inline tools::usize tools::Buffer::size() const {
    return header()->used;
}

inline tools::Buffer::Header* tools::Buffer::header() const {
    return static_cast<Header*>(storage) - 1;
}

template <typename T>
inline T* tools::SmallVec<T>::data() {
    return static_cast<T*>(buffer.data());
}

template <typename T>
inline const T* tools::SmallVec<T>::data() const {
    return static_cast<const T*>(buffer.data());
}

template <typename T>
inline tools::usize tools::SmallVec<T>::size() const {
    return buffer.size() / sizeof(T);
}

template <typename T>
inline T& tools::SmallVec<T>::operator[](usize index) {
    return data()[index];
}

template <typename T>
inline const T& tools::SmallVec<T>::operator[](usize index) const {
    return data()[index];
}

template <typename T>
inline void tools::SmallVec<T>::resize(usize count) {
    buffer.resize(checkedMultiply(count, sizeof(T)));
}

template <typename T>
inline void tools::SmallVec<T>::clear() {
    buffer.resize(0);
}

template <typename T>
inline void tools::SmallVec<T>::pushBack(T value) {
    const usize at = size();
    resize(checkedAdd(at, 1));
    data()[at] = value;
}

template <typename T>
template <typename F>
inline void tools::SmallVec<T>::keepIf(F keep) {
    usize kept = 0;
    for (usize i = 0; i < size(); ++i) {
        if (keep(data()[i])) {
            data()[kept++] = data()[i];
        }
    }
    resize(kept);
}

inline bool tools::Name::empty() const {
    return length == 0;
}

inline tools::usize tools::Body::size() const {
    return usize(end - at);
}

template <typename T>
inline T tools::Body::take() {
    if (size() < sizeof(T)) {
        fatal("a short packet");
    }
    T value;
    memcpy(&value, at, sizeof value);
    at += sizeof value;
    return value;
}

inline bool tools::Assembly::fits(usize n) const {
    return n <= sizeof out - size;
}

template <typename T>
inline void tools::Assembly::add(T value) {
    add(&value, sizeof value);
}
