#pragma once

/*
 * The calls of CTL (protocol.h), the first u32 of a request, as the
 * servers serve them and lib/c's functions make them (lib/libc), C's for
 * both: a server answers one it does not serve ENOTTY.
 */
enum {
    // A terminal's (pty), and ctlInq an events file's too.
    ctlInq = 1,
    ctlOutq,
    ctlTcgetattr,
    ctlTcsetattr,
    ctlTcflush,
    ctlTcflow,
    ctlTcdrain,
    ctlTcsbrk,
    ctlGetWinsize,
    ctlSetWinsize,
    ctlName,
    ctlGetPgrp,
    ctlSetPgrp,
    // rootfs's: of a handle, ctlWatchEvents and ctlWatchDirectory; of an
    // events file, ctlWatchAdd and ctlWatchRemove.
    ctlWatchEvents = 32,
    ctlWatchDirectory,
    ctlWatchAdd,
    ctlWatchRemove,
    // mqueue's: of an open of ipc/mq, ctlMqOpen and ctlMqUnlink; of a
    // queue, the others.
    ctlMqOpen = 64,
    ctlMqUnlink,
    ctlMqSend,
    ctlMqReceive,
    ctlMqAttributes,
    ctlMqNotify,
    ctlMqUnnotify,
    // xsi's, of an open of ipc/xsi.
    ctlMsgget = 96,
    ctlMsgsnd,
    ctlMsgrcv,
    ctlMsgctl,
    ctlSemget,
    ctlSemop,
    ctlSemctl,
    // The VFS's, rootfs's, of a handle, a path's start, the root its
    // first descriptor (protocol.h).
    ctlOpen = 128,
    ctlStat,
    ctlReadlink,
    ctlMkdir,
    ctlMknod,
    ctlSymlink,
    ctlUnlink,
    ctlRename,
    ctlLink,
    ctlChmod,
    ctlChown,
    ctlUtimes,
    ctlTruncate,
    ctlStatfs,
    ctlRealpath,
    ctlMount,
    ctlUnmount,
    ctlPath,
};
