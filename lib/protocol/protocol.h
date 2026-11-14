#pragma once

// The file protocol of the system: three interfaces on one wire.
//
//   The VFS's: the kernel's calls with a path (bin/vm/kernel_vfs.go),
//   on a handle of rootfs's (bin/rootfs). rootfs is the VFS: it alone
//   walks a path, follows symbolic links, knows the mounts and where ".."
//   goes, and makes the last step of a call as Linux makes it.
//
//   A file system's: rootfs's calls by node, on the channel of a file
//   system it mounted, which knows its own nodes and nothing of paths,
//   mounts or roots: libfuse's (lib/fuse), and erofs's, the image at the
//   root, tmpfs's and devfs's, which rootfs starts (bin/tools).
//
//   An open file's: the kernel's calls on the endpoint of an open file, a
//   file system's or a device's (bin/tools); a handle takes them too.
//
// An endpoint is one end of a seqpacket pair: whose holds the other end
// answers on it. Every request brings a fresh endpoint, last among its
// descriptors: the reply goes on it, and an open's stays the open file's.
// A server may hand a request on, its descriptors with it, to another,
// which answers on that endpoint and keeps it: the one that handed it on
// is out of the way from then on. A server that is gone is EIO.
//
// A packet is a Header and a body; descriptors ride with it (SCM_RIGHTS).
//
//   Header { op u32, arg u32, uid u32, gid u32 }
//
// uid and gid are who asks: the kernel puts its process's ids, rootfs
// the ids of the call it makes a file system's for. A reply's are zero.
//
//   The VFS's, on a handle: a directory of the VFS's tree, or what O_PATH
//   opened; the root, the working directory and the directory descriptors
//   of a process are handles. A path walks from the handle, or from the
//   root when it is absolute: root is the id of the process's root, which
//   its handle's open gave. A call follows a last link as Linux's does,
//   and checks what Linux checks before the file system.
//     OPEN      arg flags   root u64, mode u32, path
//                                         OK stat [id u64]: the reply's
//                                         endpoint is the open file, or
//                                         with id a handle; fds[0] an
//                                         object: the file is it. A FIFO
//                                         is its stat: the kernel makes
//                                         its pipe
//     STAT      arg follow  root u64, path    OK stat
//     READLINK              root u64, path    OK target
//     MKDIR     arg mode    root u64, path
//     MKNOD     arg mode    root u64, path    a FIFO or a regular file
//     SYMLINK               root u64, target 0 path
//     UNLINK    arg flags   root u64, path    AT_REMOVEDIR
//     RENAME                root u64, to u64, old 0 new   new from the
//                                         handle of id to
//     LINK      arg follow  root u64, to u64, old 0 new
//     CHMOD     arg mode    root u64, flags u32, path   AT_SYMLINK_NOFOLLOW
//     CHOWN                 root u64, flags u32, uid u32, gid u32, path
//     UTIMES    arg when    root u64, flags u32, atime u64, mtime u64, path
//                                         when: 2 atime now, 4 mtime now,
//                                         8 atime kept, 16 mtime kept
//     TRUNCATE              root u64, size u64, path
//     STATFS                root u64, path    OK statfs
//     REALPATH              root u64, path    OK the path from the root,
//                                         with no link, "." or ".."
//     MOUNT                 root u64, target 0 source   a directory is
//                                         mounted on target too; with
//                                         fds[0] a connection to a file
//                                         system's socket, its root: the
//                                         connection is its channel
//     UNMOUNT   arg flags   root u64, path    UMOUNT_NOFOLLOW
//     PATH                  root u64          OK the handle's path from the root
//     BIND      arg mode    root u64, path    fds[0] a listener of the
//                                         kernel's: a socket node of the
//                                         last name holds it, made as MKNOD
//                                         makes a name
//     CONNECT               root u64, path    OK, fds[0] the listener the
//                                         socket node holds
//   and an open file's calls but READ and WRITE: a handle's FSTAT has its
//   id after the stat.
//
//   A file system's, on its channel, by node: a node is its inode number,
//   the root's in the stat its connection answered, 1 for a file system
//   rootfs starts itself. A name is no "." or "..", has no "/" and is
//   never followed. The file system checks who may, by the header's ids.
//   A call that brings no endpoint is a question of rootfs's, answered on
//   the channel itself, in the order the questions came.
//     LOOKUP                node u64, name    OK stat: the name's; ".." the
//                                         parent, the root's the root
//     GETATTR               node u64          OK stat
//     READLINK              node u64          OK target
//     READDIR               node u64, after u64   as an open file's
//     OPEN      arg flags   node u64          OK stat: the reply's endpoint
//                                         is the open file, but a FIFO's
//     CREATE    arg flags   node u64, mode u32, name   OK stat: the reply's
//                                         endpoint is the open file of the
//                                         name, made when it is not there;
//                                         with O_EXCL EEXIST when it is
//     MKDIR     arg mode    node u64, name
//     MKNOD     arg mode    node u64, name    a FIFO or a regular file
//     SYMLINK               node u64, target 0 name
//     LINK                  node u64, to u64, name   a name in to for node
//     UNLINK    arg flags   node u64, name    AT_REMOVEDIR
//     RENAME                node u64, to u64, old 0 new
//     CHMOD     arg mode    node u64
//     CHOWN                 node u64, uid u32, gid u32
//     UTIMES    arg when    node u64, atime u64, mtime u64
//     TRUNCATE              node u64, size u64
//     STATFS                                  OK statfs
//     PATH                  node u64          OK the node's path from the
//                                         file system's root
//     BIND      arg mode    node u64, name    fds[0] a listener: a socket
//                                         node of the name holds it
//     CONNECT               node u64          OK, fds[0] the listener a
//                                         socket node holds: who asks may
//                                         write it
//     ROOT                                    OK stat: the root's
//
//   An open file's, on its endpoint:
//     READ      arg len     off u64 woff u32
//                                         OK count u32; fds[0] a memfd of
//                                         the reader's memory, the bytes
//                                         go there from its byte woff on
//     WRITE     arg append  off u64 len u32 woff u32
//                                         OK count u32 at u64, where an
//                                         append went; fds[0] a memfd of
//                                         the writer's memory, the bytes
//                                         come from there from its byte
//                                         woff on
//     READDIR               after u64     OK (ino u64, pos u64, type u8, name, 0)*
//     FSTAT                               OK stat
//     FCHMOD    arg mode
//     FCHOWN                uid u32 gid u32
//     FUTIMES   arg when    atime u64 mtime u64
//     FTRUNCATE             size u64
//     FSYNC     arg data
//     FSTATFS                             OK statfs
//     FLOCK     arg op                    LOCK_SH, LOCK_EX, LOCK_UN
//     LOCK      arg get     type u32 start u64 len u64
//                                         OK; with get, the lock in the
//                                         way, F_UNLCK for none: type u32
//                                         start u64 len u64
//
//   A device's service's, on the channel devfs has of it, and on
//   /dev/init:
//     OPEN      arg flags   mode u32      the reply's endpoint is the open
//                                         file
//     REGISTER  arg mode    name          on /dev/init. OK stat: a device,
//                                         S_IFCHR or S_IFBLK, of that name
//                                         in /dev. fds[0] is the end of a
//                                         pair whose other end the service
//                                         keeps, the channel: devfs hands
//                                         every open of the name on to it,
//                                         and the name goes when it closes
//
//   A file system is a socket of its own, SOCK_SEQPACKET, bound to a path
//   (BIND): mount(2) connects to it and mounts the connection, and every
//   connection the file system accepts is a channel of its calls.
//
//   replies: OK, ERR with arg the errno
//   stat { ino u64, size u64, mtime u64 nanoseconds, mode u32, nlink u32,
//          uid u32, gid u32, atime u64, dev u64, ctime u64 }
//   statfs { blocks u64, bfree u64, files u64, ffree u64, dev u64,
//            bsize u32, namemax u32, flags u32 }  flags: 1 read-only

#include <stddef.h>
#include <stdint.h>
#include <sys/types.h>

struct iovec;

enum : uint32_t {
    // The VFS's.
    vfsOpen = 1,
    vfsStat,
    vfsReadlink,
    vfsMkdir,
    vfsMknod,
    vfsSymlink,
    vfsUnlink,
    vfsRename,
    vfsLink,
    vfsChmod,
    vfsChown,
    vfsUtimes,
    vfsTruncate,
    vfsStatfs,
    vfsRealpath,
    vfsMount,
    vfsUnmount,
    vfsPath,
    vfsBind,
    vfsConnect,
    // A file system's.
    fsLookup = 32,
    fsGetattr,
    fsReadlink,
    fsReaddir,
    fsOpen,
    fsCreate,
    fsMkdir,
    fsMknod,
    fsSymlink,
    fsLink,
    fsUnlink,
    fsRename,
    fsChmod,
    fsChown,
    fsUtimes,
    fsTruncate,
    fsStatfs,
    fsPath,
    fsBind,
    fsConnect,
    fsRoot,
    // An open file's.
    opRead = 64,
    opWrite,
    opReaddir,
    opFstat,
    opFchmod,
    opFchown,
    opFutimes,
    opFtruncate,
    opFsync,
    opFstatfs,
    opFlock,
    opLock,
    // A service's.
    opOpen = 96,
    opRegister,
    // Replies.
    opOk = 128,
    opErr,
};

// The most descriptors a packet brings: a READ's or WRITE's memfd and its
// endpoint, a REGISTER's end and its endpoint.
constexpr size_t maxFds = 4;
constexpr uint32_t noId = 0xffffffff; // a uid or gid CHOWN keeps
// UTIMES's when.
constexpr uint32_t whenAtimeNow = 2;
constexpr uint32_t whenMtimeNow = 4;
constexpr uint32_t whenAtimeKept = 8;
constexpr uint32_t whenMtimeKept = 16;

struct Header {
    uint32_t op;
    uint32_t arg;
    uint32_t uid;
    uint32_t gid;
};

struct Stat {
    uint64_t ino;
    uint64_t size;
    uint64_t mtime;
    uint32_t mode;
    uint32_t nlink;
    uint32_t uid;
    uint32_t gid;
    uint64_t atime;
    uint64_t dev;
    uint64_t ctime;
};

struct [[gnu::packed]] Statfs {
    uint64_t blocks;
    uint64_t bfree;
    uint64_t files;
    uint64_t ffree;
    uint64_t dev;
    uint32_t bsize;
    uint32_t namemax;
    uint32_t flags;
};

static_assert(sizeof(Header) == 16);
static_assert(sizeof(Stat) == 64 && offsetof(Stat, atime) == 40);
static_assert(sizeof(Statfs) == 52);

// Sends the parts as one packet, the descriptors with it: what was sent,
// or -1 and errno. An endpoint nobody reads any more is EPIPE, no signal.
ssize_t sendPacket(int fd, const iovec* parts, size_t count, const int* fds, size_t fdCount, int flags = 0);

// Receives one packet into buffer, its descriptors into fds, close on
// exec: its length, 0 at the end of the endpoint, or -1 and errno. The
// kernel drops descriptors past maxFds.
ssize_t receivePacket(int fd, void* buffer, size_t size, int* fds, size_t& fdCount, int flags = 0);
