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

const char* toolName = "tools";

static const Tool tools[] = {
    {"blk", blkMain},
    {"console", consoleMain},
    {"devfs", devfsMain},
    {"devices", devicesMain},
    {"dns", dnsMain},
    {"erofs", erofsMain},
    {"gencat", gencatMain},
    {"mount", mountMain},
    {"mqueue", mqueueMain},
    {"pty", ptyMain},
    {"sockmgr", sockmgrMain},
    {"spawn", spawnMain},
    {"tmpfs", tmpfsMain},
    {"umount", umountMain},
    {"xsi", xsiMain},
};

int runTool(int argc, char** argv) {
    for (; argc > 0; --argc, ++argv) {
        const char* slash = strrchr(argv[0], '/');
        const char* name = slash ? slash + 1 : argv[0];
        for (const Tool& tool : tools) {
            if (strcmp(tool.name, name) == 0) {
                toolName = tool.name;

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
