// tools: the programs of the system in one executable, as busybox's are
// in its: the services rootfs starts, devices, blk, gpt and ext4
// (service.h), and the commands mount and umount. Each is a link to
// /bin/tools in the image, and runs by the name it is run by, or by the
// name after tools.
//
//     <tool> [arg...]
//     tools <tool> [arg...]

#include <stdio.h>
#include <string.h>
#include <unistd.h>

int blkMain(int, char**);
int devicesMain(int, char**);
int ext4Main(int, char**);
int gptMain(int, char**);
int mountMain(int, char**);
int umountMain(int, char**);

namespace {
    struct Tool {
        const char* name;
        int (*main)(int, char**);
    };

    const Tool tools[] = {
        {"blk", blkMain},
        {"devices", devicesMain},
        {"ext4", ext4Main},
        {"gpt", gptMain},
        {"mount", mountMain},
        {"umount", umountMain},
    };
}

int main(int argc, char** argv) {
    for (; argc > 0; --argc, ++argv) {
        const char* slash = strrchr(argv[0], '/');
        const char* name = slash ? slash + 1 : argv[0];
        for (const Tool& tool : tools) {
            if (strcmp(tool.name, name) == 0) {
                return tool.main(argc, argv);
            }
        }
        if (strcmp(name, "tools") != 0) {
            break;
        }
    }
    dprintf(STDERR_FILENO, "usage: tools <tool> [arg...]\ntools:");
    for (const Tool& tool : tools) {
        dprintf(STDERR_FILENO, " %s", tool.name);
    }
    dprintf(STDERR_FILENO, "\n");
    return 2;
}
