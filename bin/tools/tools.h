#pragma once

// The tools of the system in one executable, as busybox's applets are in
// its: the services rootfs starts, devices, blk and gpt, and the commands
// mount and umount. Each is a link to /bin/tools in the image and runs by
// the name it is run by, or by the name after tools.
//
//     <tool> [arg...]
//     tools <tool> [arg...]

int blkMain(int argc, char** argv);
int devicesMain(int argc, char** argv);
int gptMain(int argc, char** argv);
int mountMain(int argc, char** argv);
int umountMain(int argc, char** argv);

// Runs the tool argv[0] names, or the one after "tools".
int runTool(int argc, char** argv);
