#pragma once

#include <lib/protocol/service.h>

// The stack's sockets (lib/protocol/protocol.h, a socket's calls): each
// the open file of a pair whose other end is the kernel's, or sockmgr's
// of a listener. A HAND of sockmgr's makes one, of what sockmgr kept of
// a socket on no stack, and answers the call it brings attached; its
// calls come on its endpoint after. The sockets are F-Stack's, which never
// wait: a call that would waits kept, but of a socket that does not wait
// (O_NONBLOCK of its open file), until the stack's kqueue finds it ready,
// or a CANCEL.
//
// socketsStart makes the kqueue, of the loop's epoll; socketsHand answers
// a HAND; socketsHandle and socketsGone are serve's handle and gone of the
// sockets' endpoints and of the calls they keep, false of another's;
// socketsTurn, after each turn of the stack, answers what is ready and
// tells the kernel the sockets' readiness.
void socketsStart(int epoll);
void socketsHand(Request& r);
bool socketsHandle(Endpoint& e, Request& r);
bool socketsGone(Endpoint& e);
void socketsTurn();
