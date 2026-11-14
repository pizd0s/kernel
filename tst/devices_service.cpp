// A service of one device for tst/test_devices.py: it registers name at
// rootfs's /dev/init with the mode given in octal, says so, and serves the
// device in a child whose pid is /tmp/<name>.pid: the device's bytes are
// the text given, read at any offset into the reader's memory, through the
// memfd a READ brings. ro registers on an endpoint of /dev/init open to
// read only.
//
// copy reads n bytes of a device at off into a memfd of 16 bytes of its
// memory, at woff, with copy_file_range, and prints the count, the offsets
// after it and the bytes; put writes a text from a memfd of its memory to
// a device with copy_file_range and prints the count.
//
//     devices_service <name> <mode> <text> [ro]
//     devices_service copy <device> <off> <woff> <n>
//     devices_service put <device> <text>

#include <lib/protocol/service.h>

#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/epoll.h>
#include <sys/mman.h>
#include <sys/param.h>
#include <unistd.h>

namespace {
    static const char* text;
    static size_t length;
    static Device device;

    static void readText(const Endpoint& e, const Request& r) {
        uint64_t off;
        uint32_t woff;
        if (!e.reads) {
            return fail(r.reply(), EBADF);
        }
        if (r.window() < 0 || !r.at(0, off) || !r.at(8, woff)) {
            return fail(r.reply(), EINVAL);
        }
        const size_t part = off < length ? MIN(size_t(r.h.arg), length - size_t(off)) : 0;
        pwrite(r.window(), text + MIN(off, uint64_t(length)), part, woff);
        const uint32_t done = uint32_t(part);
        answer(r.reply(), opOk, 0, &done, sizeof done);
    }

    static bool handle(Endpoint& e, Request& r) {
        if (e.channel) {
            answer(r.reply(), opOk, 0, &device.st, sizeof device.st);
            return true;
        }
        switch (r.h.op) {
            case opRead:
                readText(e, r);
                break;
            case opFstat:
                answer(r.reply(), opOk, 0, &device.st, sizeof device.st);
                break;
            default:
                fail(r.reply(), EINVAL);
        }
        return false;
    }

    static int copy(const char* path, long long off, long long woff, long long n) {
        char bytes[16];
        memset(bytes, '.', sizeof bytes);
        const int source = open(path, O_RDONLY);
        const int window = memfd_create(bytes, sizeof bytes, O_RDWR);
        off_t from = off;
        off_t to = woff;
        const ssize_t got = copy_file_range(source, &from, window, &to, size_t(n), 0);
        printf("%zd %lld %lld %.16s\n", got, (long long)from, (long long)to, bytes);
        return 0;
    }

    static int put(const char* path, const char* bytes) {
        const int target = open(path, O_WRONLY);
        const int window = memfd_create(const_cast<char*>(bytes), strlen(bytes), O_RDONLY);
        printf("%zd\n", copy_file_range(window, nullptr, target, nullptr, strlen(bytes), 0));
        return 0;
    }
}

int main(int argc, char** argv) {
    if (argc == 6 && strcmp(argv[1], "copy") == 0) {
        return copy(argv[2], atoll(argv[3]), atoll(argv[4]), atoll(argv[5]));
    }
    if (argc == 4 && strcmp(argv[1], "put") == 0) {
        return put(argv[2], argv[3]);
    }
    if (argc < 4) {
        dprintf(STDERR_FILENO, "usage: devices_service <name> <mode> <text> [ro]\n");
        return 2;
    }
    const char* name = argv[1];
    text = argv[3];
    length = strlen(text);
    const int registry = open("/dev/init", argc > 4 ? O_RDONLY : O_RDWR);
    const int e = registry < 0 ? errno : enroll(registry, name, uint32_t(strtoul(argv[2], nullptr, 8)), device);
    if (e != 0) {
        printf("register %s: %s\n", name, strerror(e));
        return 1;
    }
    printf("registered %s\n", name);
    fflush(stdout);
    const pid_t child = fork();
    if (child != 0) {
        char path[64];
        snprintf(path, sizeof path, "/tmp/%s.pid", name);
        FILE* f = fopen(path, "w");
        fprintf(f, "%d", int(child));
        fclose(f);
        return 0;
    }
    const int epoll = epoll_create1(0);
    watch(epoll, {device.end, true, false, false, nullptr});
    serve(epoll, handle);
    return 0;
}
