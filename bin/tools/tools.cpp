#include "tools.h"

#include <stdio.h>
#include <string.h>
#include <unistd.h>

namespace {
    struct Tool {
        const char* name;
        int (*main)(int, char**);
    };
}

static const Tool tools[] = {
    {"blk", blkMain},
    {"console", consoleMain},
    {"devfs", devfsMain},
    {"devices", devicesMain},
    {"erofs", erofsMain},
    {"mount", mountMain},
    {"pty", ptyMain},
    {"spawn", spawnMain},
    {"tmpfs", tmpfsMain},
    {"umount", umountMain},
};

int runTool(int argc, char** argv) {
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
