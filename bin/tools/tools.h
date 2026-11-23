#pragma once

// The tools of the system in one executable, as busybox's applets are in
// its: the file systems rootfs starts, erofs, devfs and tmpfs; the
// services of the devices, devices, blk and pty; those of the network,
// sockmgr, the sockets', and dns, the names'; console, which runs the
// shell on a pseudoterminal of the console; and the commands mount,
// umount, spawn and gencat. Each is a link to /bin/tools in the image and runs by
// the name it is run by, or by the name after tools.
//
//     <tool> [arg...]
//     tools <tool> [arg...]

int blkMain(int argc, char** argv);
int consoleMain(int argc, char** argv);
int devfsMain(int argc, char** argv);
int devicesMain(int argc, char** argv);
int dnsMain(int argc, char** argv);
int erofsMain(int argc, char** argv);
int gencatMain(int argc, char** argv);
int mountMain(int argc, char** argv);
int ptyMain(int argc, char** argv);
int sockmgrMain(int argc, char** argv);
int spawnMain(int argc, char** argv);
int tmpfsMain(int argc, char** argv);
int umountMain(int argc, char** argv);

// Runs the tool argv[0] names, or the one after "tools".
int runTool(int argc, char** argv);
