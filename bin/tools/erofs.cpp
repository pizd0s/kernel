// erofs: the image of the system, read-only: a file system of the VFS's
// (lib/protocol/protocol.h), which rootfs starts first, from the image
// itself, and mounts on the root, with a memfd of the image, rootfs's
// memory, and the channel it gives it. rootfs walks the VFS's paths and
// asks it by node, its root node 1, one name at a time; an open file is an
// endpoint of erofs's, which the kernel reads. It ends when rootfs has let
// go of its channel and the last open file is closed.
//
//     erofs <image> <channel>
//
// The image is bin/rootfs/image.py's layout: a header, a node table, a
// table of the directories' entries, sorted by name in each, their names,
// then the bytes of the files and links. A node's inode is its place in
// the table plus one, a name is found by a binary search, and an entry's
// position, which a READDIR gives the entries after, is its place among
// its directory's. erofs reads what is before the bytes into its memory as
// it starts, checked to be where the header says, and nothing else of the
// image: a file's bytes go from there straight to the memory of the
// process that reads them, through the memfd of it a READ brings, one copy
// (copy_file_range), and a link's target is read when it is asked for. dev
// is the image's: the number of a pair erofs makes as it starts, which no
// other pair in the kernel has.
//
// The image lets who asks do what its modes let them: root everything but
// executing what nobody may, the owner what the owner's bits say, the
// group of gid the group's, everyone else the others'; a name is looked up
// in a directory who asks may search. Nothing of it changes: a call that
// would is EROFS, after what Linux finds first. The locks of flock and
// fcntl are the kernel's, kept by the dev and ino an open file is made
// with.
//
// Memory. A request is received into one buffer, packet, and answered
// before the next: its names are read where they are in it. What a reply
// assembles, the entries of a directory, a path, a link's target, goes
// into one buffer of its own, out.

#include <bin/tools/lib.h>
#include <bin/tools/tools.h>
#include <lib/protocol/protocol.h>

#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/param.h>
#include <sys/stat.h>
#include <sys/statvfs.h>
#include <unistd.h>

using namespace tools;

// ---- the image ------------------------------------------------------------------

namespace {
    // A node of the image.
    struct Node {
        u32 mode;
        u32 nlink;
        u32 uid;
        u32 gid;
        u32 size;   // a file's bytes, a link's target's, a directory's entries
        u32 data;   // where a file's or a link's bytes are, a directory's first entry
        u32 parent; // a directory's parent, the root's itself
        u32 entry;  // a directory's entry in its parent's, the root's none
    };

    // An entry of a directory: its name, in the names, and its node.
    struct Entry {
        u32 name;
        u32 length;
        u32 node;
    };

    // The image's header and tables, in erofs's memory, and the memfd of
    // the whole.
    struct Image {
        int fd = -1;
        const Node* nodes = nullptr;
        u32 count = 0;
        const Entry* entries = nullptr;
        u32 entryCount = 0;
        const char* names = nullptr;
        u64 dev = 0;
        u64 used = 0; // the bytes of its files
    };
}

constexpr u32 noEntry = 0xffffffff;

static Image image;

static u64 inoOf(const Node* n) {
    return u64(n - image.nodes) + 1;
}

// The node of an inode, or none.
static const Node* nodeOf(u64 ino) {
    return ino >= 1 && ino <= image.count ? &image.nodes[ino - 1] : nullptr;
}

static bool isDirNode(const Node* n) {
    return S_ISDIR(n->mode);
}

static Name nameOf(const Entry& e) {
    return {image.names + e.name, e.length};
}

static const Node* parentOf(const Node* n) {
    return &image.nodes[n->parent];
}

static int compare(Name a, Name b) {
    const int c = memcmp(a.at, b.at, MIN(a.length, b.length));
    if (c != 0) {
        return c;
    }
    return a.length < b.length ? -1 : a.length > b.length ? 1 : 0;
}

// The node a name of a directory names, or none: its entries are
// sorted, a binary search finds it.
static const Node* childOf(const Node* dir, Name name) {
    u32 lo = dir->data;
    u32 hi = dir->data + dir->size;
    while (lo < hi) {
        const u32 mid = lo + (hi - lo) / 2;
        const int c = compare(nameOf(image.entries[mid]), name);
        if (c == 0) {
            return &image.nodes[image.entries[mid].node];
        }
        if (c < 0) {
            lo = mid + 1;
        } else {
            hi = mid;
        }
    }
    return nullptr;
}

// Whether who asks may read (4), write (2), and search or execute (1)
// the node.
static bool may(const Node* n, const Header& h, u32 want) {
    if (h.uid == 0) {
        return (want & 1) == 0 || isDirNode(n) || (n->mode & 0111) != 0;
    }
    const u32 bits = h.uid == n->uid ? n->mode >> 6 : h.gid == n->gid ? n->mode >> 3 : n->mode;
    return (bits & want) == want;
}

static u8 dtype(const Node* n) {
    return u8((n->mode >> 12) & 017);
}

// Why a name cannot be made in a directory of the image: a name that
// is there is EEXIST before anything else, as mkdir -p asks for each
// directory, and the image makes nothing, EROFS.
static int creates(const Node* c, Name name, const Header& h) {
    if (const int e = badName(name)) {
        return e;
    }
    if (!isDirNode(c)) {
        return ENOTDIR;
    }
    if (!may(c, h, 1)) {
        return EACCES;
    }
    return childOf(c, name) ? EEXIST : EROFS;
}

// A directory's links: its name, its ".", and the ".." of each
// directory in it; another's: its names.
static Stat statOf(const Node* n) {
    Stat st{};
    st.ino = inoOf(n);
    st.mode = n->mode;
    st.nlink = n->nlink;
    st.uid = n->uid;
    st.gid = n->gid;
    st.dev = image.dev;
    if (!isDirNode(n)) {
        st.size = n->size;
    }
    return st;
}

// What the image holds, nothing left: it is read-only.
static Statfs statfsOf() {
    Statfs st{};
    st.bsize = u32(blockSize);
    st.namemax = NAME_MAX;
    st.dev = image.dev;
    st.blocks = (image.used + blockSize - 1) / blockSize;
    st.files = image.count;
    st.flags = ST_RDONLY;
    return st;
}

// ---- the calls of the image, by node ------------------------------------------
//
// The image changes nothing: a call that would is EROFS, after what
// Linux finds before it.

// UNLINK and RMDIR of a name.
static int unlinkError(const Node* c, Name name, const Header& h) {
    if (const int e = badName(name)) {
        return e;
    }
    if (!isDirNode(c)) {
        return ENOTDIR;
    }
    if (!may(c, h, 1)) {
        return EACCES;
    }
    return EROFS;
}

// RENAME of a name of a directory to a name of another.
static int renameError(const Node* src, Name oldName, const Node* dst, Name newName, const Header& h) {
    if (const int e = badName(oldName)) {
        return e;
    }
    if (const int e = badName(newName)) {
        return e;
    }
    if (!isDirNode(dst)) {
        return ENOENT;
    }
    if (!isDirNode(src)) {
        return ENOTDIR;
    }
    if (!may(src, h, 1) || !may(dst, h, 1)) {
        return EACCES;
    }
    return EROFS;
}

// LINK: a new name in a directory.
static int linkError(const Node* dst, Name newName, const Header& h) {
    if (!isDirNode(dst)) {
        return ENOENT;
    }
    return creates(dst, newName, h);
}

// ---- calls on a node ---------------------------------------------------------

// A READ of a file of the image: its bytes at the offset, as many as
// asked up to its end, from the image into the window.
static void readNode(Endpoint* ep, const Header& h, Body body, int fd) {
    Io io;
    if (!ioOf(h, body, io) || io.off > INT64_MAX) {
        return error(fd, EINVAL);
    }
    const u64 off = io.off;
    const u32 woff = io.woff;
    const auto* node = static_cast<const Node*>(ep->node);
    const int w = window();
    if (w < 0) {
        return error(fd, EINVAL);
    }
    if (!S_ISREG(node->mode)) {
        return error(fd, EINVAL);
    }
    const usize count = off >= node->size ? 0 : MIN(usize(h.arg), usize(node->size - off));
    off_t from = off_t(node->data + off);
    off_t to = off_t(woff);
    for (usize left = count; left > 0;) {
        const ssize_t n = copy_file_range(image.fd, &from, w, &to, left, 0);
        if (n <= 0) {
            return error(fd, EIO);
        }
        left -= usize(n);
    }
    return readCount(fd, count);
}

// A link's target, read from the image.
static void readlinkNode(const Node* n, int fd) {
    if (!S_ISLNK(n->mode)) {
        return error(fd, EINVAL);
    }
    if (n->size > sizeof out || pread(image.fd, out, n->size, off_t(n->data)) != ssize_t(n->size)) {
        return error(fd, EIO);
    }
    ok(fd, out, n->size);
}

// An entry of a READDIR's reply, at a position.
static bool addEntry(Assembly& a, const Node* n, u64 pos, const char* name, usize length) {
    if (!a.fits(17 + length + 1)) {
        return false;
    }
    a.add(inoOf(n));
    a.add(pos);
    a.add(dtype(n));
    a.add(name, length);
    a.add(u8(0));
    return true;
}

// The entries after a position, in order, as many as one reply holds:
// "." at 1, ".." at 2, the root's its own, and then the directory's, an
// entry's position its place among them plus three.
static void readdirNode(const Node* node, Body body, int fd) {
    if (!isDirNode(node)) {
        return error(fd, ENOTDIR);
    }
    const u64 after = body.take<u64>();
    Assembly a;
    if (after < 1) {
        addEntry(a, node, 1, ".", 1);
    }
    if (after < 2) {
        addEntry(a, parentOf(node), 2, "..", 2);
    }
    for (u64 i = MIN(after < 2 ? 0 : after - 2, u64(node->size)); i < node->size; ++i) {
        const Entry& e = image.entries[node->data + i];
        if (!addEntry(a, &image.nodes[e.node], i + 3, image.names + e.name, e.length)) {
            break;
        }
    }
    ok(fd, out, a.size);
}

// The path of a directory from the image's root, made from its end, at
// the end of out: none for what is no directory.
static void pathOf(const Node* n, int fd) {
    if (!isDirNode(n)) {
        return error(fd, ENOENT);
    }
    usize start = sizeof out;
    for (; n->entry != noEntry; n = parentOf(n)) {
        const Name name = nameOf(image.entries[n->entry]);
        if (start < name.length + 1) {
            return error(fd, ENAMETOOLONG);
        }
        start -= name.length;
        memcpy(out + start, name.at, name.length);
        out[--start] = '/';
    }
    if (start == sizeof out) {
        ok(fd, "/", 1);
    } else {
        ok(fd, out + start, sizeof out - start);
    }
}

// ---- rootfs's calls, by node ------------------------------------------------------

// An open of a node, answered where its request says with a new open
// file: read as its access mode says and who asks may, never written,
// EROFS. A link is ELOOP, ENOTDIR with O_DIRECTORY; a directory is
// rootfs's to open, EISDIR here.
static void openNode(const Node* n, u32 flags, const Header& h, int fd) {
    const u32 access = flags & O_ACCMODE;
    const bool reads = access != O_WRONLY;
    const bool writes = access != O_RDONLY;
    const bool truncates = (flags & O_TRUNC) != 0;
    if (S_ISLNK(n->mode)) {
        return error(fd, (flags & O_DIRECTORY) != 0 ? ENOTDIR : ELOOP);
    }
    if ((flags & O_DIRECTORY) != 0 && !isDirNode(n)) {
        return error(fd, ENOTDIR);
    }
    if (!may(n, h, (reads ? 4 : 0) | (writes || truncates ? 2 : 0))) {
        return error(fd, EACCES);
    }
    if (isDirNode(n)) {
        return error(fd, EISDIR);
    }
    if (writes || truncates) {
        return error(fd, EROFS);
    }
    openFile(fd, flags, const_cast<Node*>(n), image.dev, inoOf(n), true, false);
}

// A call of rootfs's on its channel, by node, answered on fd.
static void fsCall(const Header& h, Body body, int fd) {
    if (h.op == fsRoot) {
        const Stat st = statOf(&image.nodes[0]);
        ok(fd, &st, sizeof st);
        return;
    }
    if (body.size() < 8) {
        return error(fd, EINVAL);
    }
    const Node* n = nodeOf(body.take<u64>());
    if (!n) {
        return error(fd, ENOENT);
    }
    switch (h.op) {
        case fsLookup: {
            const Name name = body.rest();
            if (!isDirNode(n)) {
                return error(fd, ENOTDIR);
            }
            if (!may(n, h, 1)) {
                return error(fd, EACCES);
            }
            const Node* found = name.is("..") ? parentOf(n) : childOf(n, name);
            if (!found) {
                return error(fd, ENOENT);
            }
            const Stat st = statOf(found);
            ok(fd, &st, sizeof st);
            return;
        }
        case fsGetattr: {
            const Stat st = statOf(n);
            ok(fd, &st, sizeof st);
            return;
        }
        case fsReadlink:
            return readlinkNode(n, fd);
        case fsReaddir:
            return readdirNode(n, body, fd);
        case fsOpen:
            return openNode(n, h.arg, h, fd);
        case fsCreate: {
            body.take<u32>(); // the mode of what it would make
            const Name name = body.rest();
            if (const Node* c = isDirNode(n) ? childOf(n, name) : nullptr) {
                if ((h.arg & O_EXCL) != 0) {
                    return error(fd, EEXIST);
                }
                return openNode(c, h.arg, h, fd);
            }
            return error(fd, creates(n, name, h));
        }
        case fsMkdir:
        case fsMknod:
        case fsBind:
            return error(fd, creates(n, body.rest(), h));
        case fsSymlink:
            body.string(); // the target
            return error(fd, creates(n, body.rest(), h));
        case fsLink:
        case fsRename: {
            const Node* to = nodeOf(body.take<u64>());
            if (!to) {
                return error(fd, ENOENT);
            }
            if (h.op == fsLink) {
                return error(fd, linkError(to, body.rest(), h));
            }
            const Name old = body.string();
            return error(fd, renameError(n, old, to, body.rest(), h));
        }
        case fsUnlink:
            return error(fd, unlinkError(n, body.rest(), h));
        case fsChmod:
        case fsChown:
        case fsUtimes:
        case fsTruncate:
            return error(fd, EROFS);
        case fsStatfs: {
            const Statfs st = statfsOf();
            ok(fd, &st, sizeof st);
            return;
        }
        case fsPath:
            return pathOf(n, fd);
        case fsConnect:
            return error(fd, ECONNREFUSED); // the image holds no socket
        default:
            return error(fd, ENOSYS);
    }
}

// A call on an open file, answered on fd. What the image opens is open
// to read only, and changes nothing.
static void fileCall(Endpoint* ep, const Header& h, Body body, int fd) {
    const auto* node = static_cast<const Node*>(ep->node);
    switch (h.op) {
        case opRead:
            return readNode(ep, h, body, fd);
        case opWrite:
            return error(fd, EBADF);
        case opReaddir:
            return error(fd, ENOTDIR);
        case opFstat: {
            const Stat st = statOf(node);
            ok(fd, &st, sizeof st);
            return;
        }
        case opFstatfs: {
            const Statfs st = statfsOf();
            ok(fd, &st, sizeof st);
            return;
        }
        case opFsync:
            return answer(fd, 0);
        case opFchmod:
        case opFchown:
        case opFutimes:
        case opFtruncate:
            return error(fd, EROFS);
        default:
            return error(fd, ENOSYS);
    }
}

// An endpoint that closes: nothing is kept of it.
static void closing(Endpoint*) {
}

// What is before the image's bytes, its header and tables, read from
// the memfd into memory and checked to be where the header says,
// inside what was read: a node's entries and parent, an entry's name
// and node; a file's and a link's bytes, inside the image.
static void openImage(int fd) {
    u32 header[11];
    struct stat st;
    if (fstat(fd, &st) != 0 || pread(fd, header, sizeof header, 0) != ssize_t(sizeof header)) {
        fatal("erofs: no image: errno %d", errno);
    }
    const u32 count = header[2];
    const u32 entries = header[3];
    const u64 tables = header[7];
    const u64 size = header[8];
    if (memcmp(header, "PZIM", 4) != 0 || header[1] != 2 || count == 0 || size != u64(st.st_size) || tables > size || tables < sizeof header || (header[4] | header[5]) % 4 != 0 || u64(header[4]) + u64(count) * sizeof(Node) > tables || u64(header[5]) + u64(entries) * sizeof(Entry) > tables || header[6] > tables) {
        fatal("erofs: the image is none of image.py's");
    }
    auto* bytes = static_cast<u8*>(allocate(tables));
    if (pread(fd, bytes, tables, 0) != ssize_t(tables)) {
        fatal("erofs: the image's tables: errno %d", errno);
    }
    image.fd = fd;
    image.nodes = reinterpret_cast<const Node*>(bytes + header[4]);
    image.count = count;
    image.entries = reinterpret_cast<const Entry*>(bytes + header[5]);
    image.entryCount = entries;
    image.names = reinterpret_cast<const char*>(bytes + header[6]);
    if (!isDirNode(&image.nodes[0])) {
        fatal("erofs: the image's root is no directory");
    }
    for (u32 i = 0; i < count; ++i) {
        const Node& n = image.nodes[i];
        const bool dir = S_ISDIR(n.mode);
        const bool fits = dir ? u64(n.data) + n.size <= entries && n.parent < count : u64(n.data) + n.size <= size;
        if (!fits || (dir && n.entry != noEntry && n.entry >= entries)) {
            fatal("erofs: node %u of the image is out of it", i);
        }
        if (S_ISREG(n.mode)) {
            image.used += n.size;
        }
    }
    for (u32 i = 0; i < entries; ++i) {
        const Entry& e = image.entries[i];
        if (e.node >= count || u64(header[6]) + e.name + e.length > tables) {
            fatal("erofs: entry %u of the image is out of it", i);
        }
    }
    image.dev = device();
}

int erofsMain(int argc, char** argv) {
    if (argc != 3) {
        dprintf(STDERR_FILENO, "usage: erofs <image> <channel>\n");
        return 2;
    }
    const int fd = atoi(argv[1]);
    const int channel = atoi(argv[2]);
    if (fcntl(fd, F_SETFD, FD_CLOEXEC) != 0 || fcntl(channel, F_SETFD, FD_CLOEXEC) != 0) {
        fatal("erofs: errno %d", errno);
    }
    openImage(fd);
    listen(channel, Role::Channel, nullptr, true, true);
    serve(fsCall, fileCall, closing);
    return 0;
}
