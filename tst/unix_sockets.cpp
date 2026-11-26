// The AF_UNIX sockets of a process of the system's for
// tst/test_unix_sockets.py: SOCK_SEQPACKET sockets bound to a path of the
// VFS's, which a socket node of the path's file system names, and connected
// to through it. It prints what each call says, in order. With a name, it
// registers a socket of that name in /dev first (REGISTER), its own while
// it runs, as a service does before it binds one there, and at the end
// binds to a name of /dev nobody registered.
//
//     unix_sockets <path> [<name in /dev>]

#include <lib/protocol/service.h>

#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <sys/wait.h>
#include <unistd.h>

static sockaddr_un addressOf(const char* path) {
    sockaddr_un a{};
    a.sun_family = AF_UNIX;
    strncpy(a.sun_path, path, sizeof a.sun_path - 1);
    return a;
}

static int bound(int s, const char* path) {
    const sockaddr_un a = addressOf(path);
    return bind(s, reinterpret_cast<const sockaddr*>(&a), sizeof a);
}

static int connected(int s, const char* path) {
    const sockaddr_un a = addressOf(path);
    return connect(s, reinterpret_cast<const sockaddr*>(&a), sizeof a);
}

static void says(const char* what, int result) {
    printf("%s %s\n", what, result == 0 ? "ok" : strerror(errno));
    fflush(stdout);
}

static int socketOf() {
    return socket(AF_UNIX, SOCK_SEQPACKET, 0);
}

int main(int argc, char** argv) {
    if (argc != 2 && argc != 3) {
        dprintf(STDERR_FILENO, "usage: unix_sockets <path> [<name in /dev>]\n");
        return 2;
    }
    const char* path = argv[1];
    if (argc == 3) {
        enroll("unix_sockets", argv[2], S_IFSOCK | 0666);
    }
    const int listening = socketOf();
    says("bind", bound(listening, path));
    struct stat st;
    says("stat", stat(path, &st));
    printf("socket %d\n", S_ISSOCK(st.st_mode) ? 1 : 0);
    says("bind again", bound(socketOf(), path));
    says("connect before listen", connected(socketOf(), path));
    says("listen", listen(listening, 4));
    const pid_t child = fork();
    if (child == 0) {
        const int s = socketOf();
        const int r = connected(s, path);
        send(s, "a message", 10, 0);
        char back[16] = {};
        recv(s, back, sizeof back, 0);
        printf("child: connect %s, back %s\n", r == 0 ? "ok" : strerror(errno), back);
        fflush(stdout);
        _exit(0);
    }
    const int accepted = accept(listening, nullptr, nullptr);
    char got[16] = {};
    const ssize_t n = recv(accepted, got, sizeof got, MSG_TRUNC);
    printf("accept %s, %zd bytes: %s\n", accepted >= 0 ? "ok" : strerror(errno), n, got);
    fflush(stdout);
    send(accepted, "back", 5, 0);
    waitpid(child, nullptr, 0);
    close(listening);
    says("connect after close", connected(socketOf(), path));
    says("unlink", unlink(path));
    says("connect after unlink", connected(socketOf(), path));
    says("connect to a file", connected(socketOf(), "/etc/motd"));
    says("bind in the image", bound(socketOf(), "/etc/socket"));
    if (argc == 3) {
        says("bind of no name in /dev", bound(socketOf(), "/dev/none"));
    }
    return 0;
}
