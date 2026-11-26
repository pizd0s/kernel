#pragma once

// The file protocol of the system: three interfaces on one wire.
//
//   The VFS's: the calls with a path, libc's ctls (lib/libc/vfs.c, the
//   calls of calls.h), on a handle of rootfs's (bin/rootfs), the kernel's
//   to pass and not to read. rootfs is the VFS: it alone
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
// An open file is a pair the server makes as it opens, a seqpacket
// socketpair: it keeps the first end, the open file's endpoint, and the
// OPEN's OK brings the second to who asked, fds[0], with what it is, an
// Opened: a file, a terminal's, a handle of a directory or of what O_PATH
// opened. The kernel makes it that open file for its process. An OK with
// no Opened brings the end as it is, a socket to its opener. And attach
// (lib/c) makes a directory of an end of a pair: the root of the server
// behind it, which rootfs makes the root of the processes it starts so.
// A handle is named by its pair's id, the kernel's (fdid, unistd.h),
// which the server asks of its end and no one else gives.
//
// A call on an open file is a request sent on the open file's endpoint:
// a packet, its descriptors with it, the last of them where the request
// is answered, an object of the kernel's that takes one answer. The
// server writes the reply there, a packet as it would send it (sendmsg,
// its descriptors with it, or write), and closes the descriptor: the
// close is the end of the answer, and who asked wakes with what was
// written. A server may hand a request on, its descriptors with it, to
// another, which answers there: the descriptor is whose answers, so the
// server that handed it on closes its own. A server that is gone is EIO:
// its descriptors close with it, and a request closed with no answer
// written is EIO to who asked.
//
// An answer may be MOVE where it would be OK: fds[0] is what the open
// file is from then on, an end of a pair, a file of the kernel's with no
// server. The kernel puts it in the open file's place as the answer
// comes, for every descriptor of the open file, and whoever waits on the
// open file waits on it then. The server is done with the open file: it
// closes its endpoint, and a call on its way to it, lost with the
// endpoint, the kernel makes again on what took its place. An answer
// nobody is there to take moves nothing: the send fails, and the open
// file is the server's still.
//
// Who asked may take a signal while the request waits: where the request
// is answered is readable then, and a receive there gives CANCEL, a
// header with the ids of who asked. A server that keeps the request, a
// READ or a WRITE that waits as long as its file says, watches for it and
// answers the request at once, with the count so far or ERR EINTR; one
// that does not keep it never looks. When who asked is gone the receive
// gives the end, and an answer is EPIPE.
//
// A packet is a Header and a body; descriptors ride with it (SCM_RIGHTS).
//
//   Header { op u32, arg u32, uid u32, gid u32, pgrp u32, sid u32 }
//
// uid, gid, pgrp and sid are who asks: the kernel puts its process's ids,
// its process group and its session, rootfs and devfs those of the call
// they hand on. pgrp 0 is a call no job control holds back: the kernel's
// own, and one whose process may go on out of the foreground (BACKGROUND).
// A reply's are zero.
//
//   The VFS's, on a handle: a directory of the VFS's tree, or what O_PATH
//   opened; the working directory and the root of a process, which the
//   kernel keeps for it and names AT_FDCWD and AT_FDROOT, and its
//   directory descriptors are handles. A path walks from the handle the
//   CTL is of, the root itself where it is absolute, and never above the
//   root, its first descriptor: a handle of ours rootfs knows by its id,
//   the kernel's (fdid), so a process walks from what it holds and from
//   nothing else, its lib/c choosing. A call follows a last link as
//   Linux's does, and checks what Linux checks before the file system.
//   Its CTLs (calls.h), each call u32, then arg u32, then:
//     ctlOpen      flags   mode u32, path    OK 1, opened, fds[0] the open
//                                         file or the handle; of a FIFO an
//                                         opened of openedFifo and no
//                                         descriptor: the kernel makes its
//                                         pipe
//     ctlStat      follow  path              OK stat
//     ctlReadlink          path              OK target
//     ctlMkdir     mode    path
//     ctlMknod     mode    path              a FIFO, a socket or a regular
//                                         file
//     ctlSymlink           target 0 path
//     ctlUnlink    flags   path              AT_REMOVEDIR
//     ctlRename            old 0 new         new from fds[1], a handle
//     ctlLink      follow  old 0 new         the same
//     ctlChmod     mode    flags u32, path   AT_SYMLINK_NOFOLLOW
//     ctlChown             flags u32, uid u32, gid u32, path
//     ctlUtimes    when    flags u32, atime u64, mtime u64, path
//                                         when: 2 atime now, 4 mtime now,
//                                         8 atime kept, 16 mtime kept
//     ctlTruncate          size u64, path
//     ctlStatfs            path              OK statfs
//     ctlRealpath          path              OK the path from the root,
//                                         with no link, "." or ".."
//     ctlMount             target 0 source   a directory is mounted on
//                                         target too; with fds[1] a
//                                         connection to a file system's
//                                         socket, its root: the connection
//                                         is its channel
//     ctlUnmount   flags   path              UMOUNT_NOFOLLOW
//     ctlPath                                OK the handle's path from the
//                                         root
//   and an open file's calls but READ and WRITE. A FIFO and a socket are
//   names of the file systems' and nothing more: the kernel makes the pipe
//   a FIFO's openers share, and rootfs keeps what socket is bound to a
//   socket's node (a socket's calls, AF_UNIX's).
//
//   A file system's, on its channel, by node: a node is its inode number,
//   the root's in the stat its connection answered, 1 for a file system
//   rootfs starts itself. A name is no "." or "..", has no "/" and is
//   never followed. The file system checks who may, by the header's ids.
//   A call that brings no connection is a question of rootfs's, answered
//   on the channel itself, in the order the questions came; one rootfs
//   hands on brings the connection it came on, last among its
//   descriptors, where it is answered.
//     LOOKUP                node u64, name    OK stat: the name's; ".." the
//                                         parent, the root's the root
//     GETATTR               node u64          OK stat
//     READLINK              node u64          OK target
//     READDIR   arg room    node u64, after u64   an open file's READDIR's
//                                         reply: the entries after the
//                                         position after that room holds
//     OPEN      arg flags   node u64          OK, fds[0] the open file; a
//                                         FIFO's OK is its stat alone
//     CREATE    arg flags   node u64, mode u32, name   OK, fds[0] the open
//                                         file of the name, made when it is
//                                         not there, opened's made 1 then;
//                                         with O_EXCL EEXIST when it is
//     MKDIR     arg mode    node u64, name
//     MKNOD     arg mode    node u64, name    a FIFO, a socket or a regular
//                                         file
//     SYMLINK               node u64, target 0 name
//     LINK                  node u64, to u64, name   a name in to for node
//     UNLINK    arg flags   node u64, name    AT_REMOVEDIR. OK removed u64:
//                                         the node the name named
//     RENAME                node u64, to u64, old 0 new   OK moved u64,
//                                         replaced u64: what the old name
//                                         named and what the new one named,
//                                         0 for none; the two the same when
//                                         they were names of one node,
//                                         which nothing changes
//     CHMOD     arg mode    node u64
//     CHOWN                 node u64, uid u32, gid u32
//     UTIMES    arg when    node u64, atime u64, mtime u64
//     TRUNCATE              node u64, size u64
//     STATFS                                  OK statfs
//     PATH                  node u64          OK the node's path from the
//                                         file system's root
//     ROOT                                    OK stat: the root's, then
//                                         flags u32: fsNocache, of a file
//                                         system that changes by itself
//   and from a file system that changes by itself, on its channel, every
//   change of its names as it makes it, in order with its answers, which
//   rootfs's watchers of its directories hear: no answer.
//     NOTICE    arg kind    node u64, name    watchAdded or watchRemoved
//                                         of a name of the directory node,
//                                         noticeDirectory added to it of a
//                                         directory's name; noticeGone, no
//                                         name: the directory node is gone
//
//   An open file's, on a connection to it. The open file keeps its
//   position and its flags, the OPEN's, as an open file description does:
//   who shares the open file shares them.
//     READ      arg len     io            OK count u32; fds[0] a memfd of
//                                         the reader's memory, the bytes
//                                         go there. At the position, which
//                                         moves by the count, or at off
//     WRITE     arg len     io            OK count u32; fds[0] a memfd of
//                                         the writer's memory, the bytes
//                                         come from there. At the position, or
//                                         at the end the file has when it
//                                         appends (O_APPEND), the position
//                                         after it then; or at off
//     SEEK      arg whence  off u64       OK pos u64: the position set,
//                                         SEEK_SET, SEEK_CUR or SEEK_END;
//                                         EINVAL below 0, ESPIPE of what
//                                         has none. A directory's is an
//                                         entry's, READDIR's pos: READDIR
//                                         goes on after it
//     GETFL                               OK flags u32: O_ACCMODE, O_APPEND,
//                                         O_NONBLOCK, O_PATH
//     SETFL     arg flags                 O_APPEND and O_NONBLOCK of the
//                                         open file: one that does not wait
//                                         answers at once, EAGAIN where it
//                                         would
//     CTL                   call u32, its body   a call of the server's own,
//                                         a program's (ctl, unistd.h), fds
//                                         with it, which the kernel neither
//                                         makes nor reads: OK arg n, n
//                                         openeds, then the answer, fds the
//                                         answer's, the first n open files
//                                         of the server's as their openeds
//                                         say. The calls are calls.h's, the
//                                         servers' and lib/c's: a terminal's
//                                         below, rootfs's, mqueue's and
//                                         xsi's; ENOTTY of another
//     READDIR   arg room                  OK (ino u64, pos u64, type u8, name, 0)*:
//                                         the entries after the position,
//                                         as many as room bytes of struct
//                                         dirent64 hold, (19 + length + 1
//                                         + 7) & ~7 each; the position moves
//                                         to the last one's
//     FSTAT                               OK stat
//     FCHMOD    arg mode
//     FCHOWN                uid u32 gid u32
//     FUTIMES   arg when    atime u64 mtime u64
//     FTRUNCATE             size u64
//     FSYNC     arg data
//     FSTATFS                             OK statfs
//     CANCEL                              of the request the server
//                                         keeps while it waits
//   and from the server, on its end of an open file, the file's readiness
//   when it changes, which select, poll and epoll find: the OPEN's OK said
//   it first (opened's events), the last READY counts.
//     READY     arg events            events: POLLIN, POLLPRI, POLLOUT,
//                                     POLLERR and POLLHUP, Linux's bits
//
//   A terminal's, on a terminal's open file (openedTerminal): the server
//   keeps all of the terminal, its settings and its discipline
//   (bin/tools/pty.cpp). termios is lib/c's struct termios, winsize its
//   struct winsize; a master's are its slave's, as Linux's are.
//   The server keeps its job control too: which session the terminal
//   controls and which of the session's groups is in the foreground.
//   A call that a group out of the foreground may not make, on the
//   session's controlling terminal, a slave, is answered BACKGROUND with
//   the signal its group gets, and goes no further: a READ, SIGTTIN; a
//   WRITE while TOSTOP is set, and a CTL of ctlTcsetattr, ctlTcflush,
//   ctlTcflow, ctlTcdrain, ctlTcsbrk or ctlSetPgrp, SIGTTOU. What follows
//   is the kernel's, which knows the process: EIO, or the call again with
//   pgrp 0 where the process ignores the signal or blocks it, EIO where
//   its group is orphaned, else the signal to the group. Its CTLs:
//     ctlTcgetattr                        OK termios
//     ctlTcsetattr  when u32, termios     TCSANOW, TCSADRAIN, TCSAFLUSH; a
//                                         slave's that drains waits for the
//                                         output to go, and may hear CANCEL
//     ctlTcflush    queue u32             TCIFLUSH, TCOFLUSH, TCIOFLUSH
//     ctlTcflow     action u32            TCOOFF, TCOON, TCIOFF, TCION
//     ctlTcdrain                          waits for the output to go, and
//                                         may hear CANCEL
//     ctlTcsbrk                           a break, none on a pseudoterminal
//     ctlGetWinsize                       OK winsize
//     ctlSetWinsize winsize               SIGWINCH to the foreground group
//                                         when it changes
//     ctlInq                              OK count u32: the bytes a read
//                                         would find
//     ctlOutq                             OK count u32: the bytes written
//                                         that the other end has not read
//     ctlName       peer u32              OK the path of the terminal's
//                                         device; with peer a master's
//                                         slave's
//     ctlGetPgrp                          OK pgrp u32, sid u32: the
//                                         foreground group and the session
//                                         the terminal controls, 0 for none;
//                                         ENOTTY on a slave not the caller's
//                                         session's controlling terminal
//     ctlSetPgrp    pgrp u32              the foreground group, which lib/c
//                                         found in the caller's session;
//                                         ENOTTY as ctlGetPgrp, and on a
//                                         master
//   and the kernel's own:
//     SETCTTY   arg how                   on a slave, how its control of the
//                                         caller's session goes: cttyTake,
//                                         the slave controls it now, the
//                                         caller's group in the foreground,
//                                         EPERM when it controls another;
//                                         cttyGone, the session's leader went,
//                                         its foreground group hears SIGHUP;
//                                         cttyDropped, the leader gave it up
//                                         (TIOCNOTTY), SIGHUP and SIGCONT. It
//                                         controls no session then
//
//   A socket's. A socket is an open file of its server's, rootfs's of
//   AF_UNIX, sockmgr's and then a stack's of the network's families, and
//   an end of a pair is a socket with no server, the kernel's. What a
//   socket is asked but its bytes are CTLs (calls.h), a call's number,
//   its arg, then what it takes, which lib/c makes (lib/libc/sockets.c):
//   the socket's server answers them, the kernel those of an end of a
//   pair, and what is no socket serves none, ENOTTY, which lib/c says as
//   ENOTSOCK. A sockaddr is lib/c's struct sockaddr, len u32 before it.
//     ctlSocket    type    domain u32, protocol u32   of a handle of
//                                         rootfs's, a socket of AF_UNIX,
//                                         OK opened; of an open of
//                                         sockmgr's socket, what it is.
//                                         SOCK_NONBLOCK of type is the
//                                         socket's O_NONBLOCK
//     ctlBind      mode    len u32, sockaddr
//     ctlConnect           len u32, sockaddr EINPROGRESS of a socket of
//                                         the network's that does not wait
//     ctlListen    backlog
//     ctlAccept    flags                     OK, its arg 1, opened, the
//                                         peer's sockaddr, fds[0] the
//                                         connection's open file; or, its
//                                         arg 0, the peer's sockaddr,
//                                         fds[0] the connection as it is,
//                                         a file of the kernel's
//     ctlSockname  peer                      OK sockaddr: the socket's, with
//                                         peer its peer's
//     ctlGetsockopt        level u32, name u32   OK value
//     ctlSetsockopt        level u32, name u32, value
//     ctlShutdown  how
//     ctlSendto    flags   len u32, sockaddr, data   a datagram to an
//                                         address of AF_UNIX. OK count u32
//   An end of a pair is bound and connected: its ctlBind, ctlListen and
//   ctlAccept are EINVAL, its ctlConnect EISCONN, and its options and
//   its shutdown the kernel does.
//
//   A socket's bytes are the kernel's calls on its open file. A packet
//   is at most 65536 bytes: of a stream's send of more, the kernel sends
//   what fits, the send's count, and a datagram that does not fit is
//   EMSGSIZE. They are ENOTSOCK of what is no socket.
//     SEND      arg flags   len u32, sockaddr, data   to the sockaddr, none
//                                         for a connected socket; fds the
//                                         rights. OK count u32
//     RECV      arg flags   want u32      OK flags u32, len u32, sockaddr,
//                                         data: the sender's and at most
//                                         want bytes; fds the rights
//     SOCKTYPE                            OK type u32
//
//   A socket of AF_UNIX is rootfs's. Its address is a path of the VFS's:
//   a call with one brings the root of who asks and where the path
//   starts, its first descriptors, a send's rights after them. ctlBind
//   makes the path's node, a socket's of the mode; a socket's node
//   nobody is bound to is taken as it is, and another is EADDRINUSE. A
//   path of no name, or one that starts with a 0, is ENOENT. ctlConnect
//   finds the socket bound to the path's node: a datagram socket's peer
//   from then on, and of a stream's or a seqpacket's the answer is MOVE,
//   fds[0], an end of a pair, the socket from then on, the other end
//   waiting for the listener's ctlAccept. With nobody listening, or a
//   queue of more than the backlog and a half, ECONNREFUSED: a connect
//   waits for nothing. A connection is a pair of the kernel's, which no
//   server serves: a connected socket and an accepted one are its ends. A
//   socket that listens and a datagram socket are rootfs's for good: a
//   datagram waits there for its RECV, which gives its sender's sockaddr,
//   none of a sender bound to nothing, and a send to a socket with no
//   room is ENOBUFS. The names of AF_UNIX are nobody's to ask:
//   ctlSockname is EOPNOTSUPP, of a pair's end too. A stream connected to
//   nothing reads, writes and shuts down ENOTCONN.
//
//   socket(2) of a family of the network's opens sockmgr's socket
//   (bin/tools/sockmgr), a socket on no stack yet; its ctlBind,
//   ctlConnect, or SEND to an address, sockmgr hands on to the stack it
//   chooses for it, the socket with it: sockmgr's end of the socket's
//   channel, which the stack serves from then on, the call first. The
//   open file is the same to the kernel.
//
//   A card's stack's and sockmgr's, on sockmgr's connection to the
//   stack's socket, net/<card>/iface/stack, a request on it answered
//   where it says:
//     STACK                 text        the stack's state, a line each,
//                                         card <name>, inet <a>/<len>,
//                                         gateway <a>, dns <a>: at the
//                                         connection and at every change;
//                                         no answer
//     HAND      arg op      domain u32, type u32, protocol u32, nonblock
//                           u32, call's arg u32, bound len u32 and sockaddr,
//                           options u32, (level u32, name u32, len u32,
//                           value)*, the call's body   sockmgr's: a socket
//                           on no stack, of what sockmgr kept of it, its
//                           local address bound first when there is one,
//                           then op: ctlBind, ctlConnect or SEND, answered where
//                           the call is, the last of the descriptors, and
//                           before it the socket's open file, sockmgr's
//                           end of its channel, the stack's to serve from
//                           then on, a send's rights before that; ctlListen,
//                           its arg the backlog, answered OK opened, fds[0]
//                           the listener's open file, sockmgr's, on which
//                           READY tells of connections and ctlAccept takes
//                           one
//
//   A device's service's, on the channel devfs has of it, and on
//   /dev/init/ctl:
//     OPEN      arg flags   mode u32      OK, fds[0] the open file; an
//                                         OK of no body, fds[0] an open file
//                                         as it is, another server's, which
//                                         the opener gets so
//     OPEN of /dev/tty, pty's, is another open of the slave of the
//     terminal that controls the session the header says, ENXIO when none.
//     OPEN of /dev/console, console's, is another open of the slave of its
//     second pseudoterminal, handed on as it is; what that terminal's
//     master reads console writes to its descriptor 2.
//   /dev/init/ctl's open is a plain socket, an end of a pair devfs keeps the
//   other end of, which its opener speaks the protocol on itself: a
//   request on it is answered on it.
//     REGISTER  arg mode    name          on /dev/init/ctl. OK stat: a file
//                                         of that name in /dev, S_IFCHR,
//                                         S_IFBLK, S_IFSOCK or S_IFREG,
//                                         whose owner is who asks
//                                         (the header's ids, the service's
//                                         or whom it registers it for: a
//                                         pseudoterminal's slave is its
//                                         opener's). fds[0] is the end of a
//                                         pair whose other end the service
//                                         keeps, the channel: devfs hands
//                                         every open of the name on to it,
//                                         and the name goes when it closes.
//                                         A socket's is the service's to
//                                         bind(2) its socket to; only
//                                         REGISTER makes a name in /dev
//
//   rootfs's CTLs, of inotify and watchdir:
//     ctlWatchEvents    flags u32         on a handle: OK 1, an events
//                                         file, inotify's instance, of
//                                         O_NONBLOCK of the flags, whose
//                                         READ gives struct inotify_event's
//                                         as inotify(7) has them, as many as
//                                         fit, waiting for one but of
//                                         O_NONBLOCK, and whose ctlInq is
//                                         the bytes queued
//     ctlWatchDirectory flags u32         on a handle of a directory: OK 1,
//                                         once it listed it, the
//                                         directory's watcher, of O_NONBLOCK
//                                         of the flags, whose READ gives
//                                         records of its names, kind u8,
//                                         name, 0, as many as fit:
//                                         watchExisting of each name there,
//                                         then watchIdle once, then
//                                         watchAdded and watchRemoved, each
//                                         of what changed since the READ
//                                         before; a name gone and back is
//                                         another, removed and added. A READ
//                                         that finds none waits, but of
//                                         O_NONBLOCK; one of a directory
//                                         gone is 0
//     ctlWatchAdd       mask u32          on an events file, fds a
//                                         directory, a handle, which rootfs
//                                         knows by its id, the kernel's
//                                         (fdid): its events to the file's;
//                                         OK wd u32
//     ctlWatchRemove    wd u32            on an events file: the mark taken
//                                         away
//
//   mqueue's and xsi's (bin/tools), of /dev/ipc/mq and /dev/ipc/xsi, whose
//   opens devfs hands on, their calls CTLs. Of an open of ipc/mq:
//     ctlMqOpen     flags u32, mode u32, maxmsg u32, msgsize u32, name, 0
//                                         OK 1, a descriptor of the queue of
//                                         the name, made of O_CREAT, the
//                                         mode the umask's, limits of 0 the
//                                         defaults; of the flags' access and
//                                         O_NONBLOCK
//     ctlMqUnlink   name, 0               the name goes, the queue with its
//                                         last descriptor
//   of a descriptor of a queue, which tells its readiness (READY), POLLIN
//   of a message and POLLOUT of room; until is timed u32, sec i64, nsec
//   i64, of CLOCK_REALTIME, past which a call that waits is ETIMEDOUT:
//     ctlMqSend     prio u32, pid u32, uid u32, until, message
//                                         waits for room but of O_NONBLOCK,
//                                         EAGAIN
//     ctlMqReceive  room u32, until       OK prio u32, message: the first of
//                                         the highest priority, waiting as a
//                                         send does; EMSGSIZE of room short
//                                         of msgsize
//     ctlMqAttributes                     OK maxmsg u32, msgsize u32,
//                                         curmsgs u32, flags u32
//     ctlMqNotify   pid u32, how u32, signal u32, value u32
//                                         the process's notification, EBUSY
//                                         while another's lives
//     ctlMqUnnotify pid u32               its own taken away
//   Of an open of ipc/xsi, a process's, whose semops' adjustments of
//   SEM_UNDO it keeps and gives back as it closes, answered once done:
//     ctlMsgget     key i32, flags u32    OK id i32
//     ctlMsgsnd     id i32, flags u32, pid u32, mtype i32, text
//     ctlMsgrcv     id i32, room i32, mtype i32, flags u32, pid u32
//                                         OK mtype i32, text
//     ctlMsgctl     id i32, cmd u32, uid u32, gid u32, mode u32, qbytes u32
//                                         IPC_STAT's OK perm, stime i64,
//                                         rtime i64, ctime i64, qnum u32,
//                                         qbytes u32, lspid u32, lrpid u32,
//                                         cbytes u32
//     ctlSemget     key i32, nsems i32, flags u32   OK id i32
//     ctlSemop      id i32, nops u32, timed u32, sec i64, nsec i64, pid u32,
//                   (num u16, op i16, flg i16)*
//     ctlSemctl     id i32, num i32, cmd u32, pid u32, value i32, uid u32,
//                   gid u32, mode u32, SETALL's values u16*
//                                         OK a value i32, GETALL's u16*, or
//                                         IPC_STAT's perm, otime i64, ctime
//                                         i64, nsems u32
//   perm is key i32, uid u32, gid u32, cuid u32, cgid u32, mode u32, seq
//   i32; who calls the header's ids, and the pid the call says.
//
//   A file system is a socket of its own, SOCK_SEQPACKET, bound to a path:
//   mount(2) connects to it and mounts the connection, and every
//   connection the file system accepts is a channel of its calls.
//
//   rootfs keeps what a file system answers, a cache of names, as Linux's
//   dcache is, so a file system changes by the calls rootfs hands on to it
//   and by its open files, and nothing else, but for one whose ROOT says
//   fsNocache: rootfs keeps nothing of its answers, and the file system
//   tells it every change of its names (NOTICE). Its atime is none of
//   rootfs's: the system is noatime. A directory's watchers are rootfs's:
//   it asks a change of a watched directory's names, where it would hand
//   it on, and sees its answer, which it then answers with; a file system
//   that changes by itself tells it its changes.
//
//   replies: OK, ERR with arg the errno, BACKGROUND with arg the signal
//   of a call out of the foreground (a terminal's), MOVE as OK of a call
//   whose open file is fds[0] from then on
//   opened { kind u32, access u32, dev u64, ino u64, events u32, made u32 }
//          kind:
//          openedFile, an open file; openedTerminal, a terminal's, which
//          takes a terminal's calls too; openedDir, a handle of a directory
//          of the VFS's, which calls with a path go to; openedPath, a handle
//          of what O_PATH opened that is no directory; openedFifo, a FIFO's,
//          of no descriptor, which the kernel makes the pipe of, by its dev
//          and ino, events the node's mode, access the open's. An OK that
//          brings openeds says how many in its arg, a descriptor each but a
//          FIFO's, its descriptors' first. events are its
//          readiness as it opens, READY's events: POLLIN and POLLOUT of
//          what is always ready, a file's, a directory's, a device's; a
//          terminal's as it is, which READY tells when it changes. access
//          is what the open lets the file do, O_ACCMODE, and O_PATH of a
//          handle, the kernel's to know without asking, as the locks it
//          keeps check it: what O_PATH opens takes no locks. The open's
//          other flags are the open file's to keep (GETFL). dev and ino are the file's or
//          the directory's, as its stat has them, or a terminal's device's:
//          they name it to the kernel, which keeps the locks of flock and
//          fcntl by them, so the open files given one dev and ino are one
//          file to lock, and a file of ino 0 has no locks; two opens of a
//          terminal are one terminal by them. made is CREATE's: 1 when
//          it made the file
//   io { off u64, flags u32 }   flags: ioAt, at off as pread and
//          pwrite ask, ESPIPE from what cannot seek
//   stat { ino u64, size u64, mtime u64 nanoseconds, mode u32, nlink u32,
//          uid u32, gid u32, atime u64, dev u64, ctime u64 }
//   statfs { blocks u64, bfree u64, files u64, ffree u64, dev u64,
//            bsize u32, namemax u32, flags u32 }  flags: 1 read-only

#include <lib/protocol/calls.h>

#include <poll.h>
#include <stddef.h>
#include <stdint.h>
#include <sys/socket.h>
#include <sys/types.h>

struct iovec;

enum : uint32_t {
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
    fsRoot,
    fsNotice,
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
    opCancel,
    opReady,
    opSeek,
    opGetfl,
    opSetfl,
    opCtl,
    // A service's.
    opOpen = 96,
    opRegister,
    // A terminal's.
    opSetCtty = 112,
    // Replies.
    opOk = 128,
    opErr,
    opBackground,
    opMove,
    // A socket's.
    opSend = 144,
    opRecv,
    opSocktype,
    // A stack's and sockmgr's.
    opStack = 160,
    opHand,
};

// The most descriptors a packet brings: where it is answered, the last
// of them, and before it a READ's or WRITE's memfd, a ctl's descriptors
// or a send's rights.
constexpr size_t maxFds = 32;
constexpr uint32_t noId = 0xffffffff; // a uid or gid CHOWN keeps
constexpr uint32_t fsNocache = 1;     // ROOT's flags: the file system changes by itself
// SETCTTY's how.
constexpr uint32_t cttyTake = 1;
constexpr uint32_t cttyGone = 2;
constexpr uint32_t cttyDropped = 3;
// Io's flags.
constexpr uint32_t ioAt = 4;

// The kinds of a watcher's records, as <sys/watchdir.h> has them.
constexpr uint8_t watchExisting = 1;
constexpr uint8_t watchIdle = 2;
constexpr uint8_t watchAdded = 3;
constexpr uint8_t watchRemoved = 4;
// NOTICE's kind of a directory gone, past the records' kinds, and its
// flag of a name that is a directory's.
constexpr uint8_t noticeGone = 5;
constexpr uint8_t noticeDirectory = 0x80;

// The room an entry of a READDIR's takes of struct dirent64, of a name of
// length.
constexpr size_t direntRoom(size_t length) {
    return (19 + length + 1 + 7) & ~size_t(7);
}
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
    uint32_t pgrp;
    uint32_t sid;
};

// What the end an OPEN's OK brings is: the OK's body.
struct Opened {
    uint32_t kind;
    uint32_t access;
    uint64_t dev;
    uint64_t ino;
    uint32_t events;
    uint32_t made = 0;
};

constexpr uint32_t openedFile = 1;
constexpr uint32_t openedTerminal = 2;
constexpr uint32_t openedDir = 3;
constexpr uint32_t openedPath = 4;
constexpr uint32_t openedFifo = 5;

// A READ's and a WRITE's body.
struct [[gnu::packed]] Io {
    uint64_t off;
    uint32_t flags;
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

static_assert(sizeof(Header) == 24);
static_assert(sizeof(Opened) == 32);
static_assert(sizeof(Io) == 12);
static_assert(sizeof(Stat) == 64 && offsetof(Stat, atime) == 40);
static_assert(sizeof(Statfs) == 52);

// Sends the parts as one packet, the descriptors with it: what was sent,
// or -1 and errno. An endpoint nobody reads any more is EPIPE, no signal.
ssize_t sendPacket(int fd, const iovec* parts, size_t count, const int* fds, size_t fdCount, int flags = 0);

// Receives one packet into buffer, its descriptors into fds, close on
// exec: its length, 0 at the end of the endpoint, or -1 and errno. The
// kernel drops descriptors past maxFds.
ssize_t receivePacket(int fd, void* buffer, size_t size, int* fds, size_t& fdCount, int flags = 0);

// What an object answers to a request of the VFS's, an open file's or a
// terminal's it does not serve: the error POSIX has the call fail with on
// what is no such thing. Any call goes to any open file, and its server
// says what it is: one by a path from what is no directory ENOTDIR, a
// listing of what is no directory ENOTDIR, a terminal's call on what is
// no terminal ENOTTY, a socket's call on what is no socket ENOTSOCK, a
// change of size or a sync of what has none EINVAL, a seek of what has no
// position ESPIPE, a read or a write of what has none EBADF; another
// ENOSYS.
int unserved(uint32_t op);
