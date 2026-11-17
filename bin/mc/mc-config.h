/* Midnight Commander's config.h for the system: what its configure finds
   of lib/c, wasm32, and the features bin/mc/make.py builds: S-Lang's
   screen, the subshell, the editor, the viewer of differences, operations
   in the background, character sets, GLib's regular expressions, and of
   the virtual file systems tar, cpio, extfs and sfs. No file(1): busybox
   has none, so a file's type is told by its name alone. */
#pragma once

#define _GNU_SOURCE 1

#define PACKAGE "mc"
#define PACKAGE_NAME "GNU Midnight Commander"
#define PACKAGE_VERSION "4.8.33"
#define PACKAGE_STRING "GNU Midnight Commander 4.8.33"
#define PACKAGE_TARNAME "mc"
#define PACKAGE_BUGREPORT "https://www.midnight-commander.org/wiki/NewTicket"
#define PACKAGE_URL "https://www.midnight-commander.org/"

#define ENABLE_BACKGROUND 1
#define ENABLE_SUBSHELL 1
#define ENABLE_VFS 1
#define ENABLE_VFS_CPIO 1
#define ENABLE_VFS_EXTFS 1
#define ENABLE_VFS_SFS 1
#define ENABLE_VFS_TAR 1
#define HAVE_CHARSET 1
#define HAVE_SLANG 1
#define SEARCH_TYPE_GLIB 1
#define USE_DIFF_VIEW 1
#define USE_INTERNAL_EDIT 1

#define HAVE_ARPA_INET_H 1
#define HAVE_DECL_ENVIRON 1
#define HAVE_DLFCN_H 1
#define HAVE_ENDMNTENT 1
#define HAVE_FCNTL_H 1
#define HAVE_FUNC_ATTRIBUTE_FALLTHROUGH 1
#define HAVE_FUNC_ATTRIBUTE_UNUSED 1
#define HAVE_FUNC_ATTRIBUTE_WEAK 1
#define HAVE_GETMNTENT 1
#define HAVE_GRANTPT 1
#define HAVE_HASMNTOPT 1
#define HAVE_INFOMOUNT_LIST 1
#define HAVE_INTTYPES_H 1
#define HAVE_LIMITS_H 1
#define HAVE_MALLOC_H 1
#define HAVE_MNTENT_H 1
#define HAVE_NETINET_IN_H 1
#define HAVE_OPENPTY 1
#define HAVE_POSIX_FALLOCATE 1
#define HAVE_POSIX_OPENPT 1
#define HAVE_PTY_H 1
#define HAVE_REALPATH 1
#define HAVE_SETLOCALE 1
#define HAVE_SETMNTENT 1
#define HAVE_SIGHANDLER_T 1
#define HAVE_STDINT_H 1
#define HAVE_STDIO_H 1
#define HAVE_STDLIB_H 1
#define HAVE_STRINGS_H 1
#define HAVE_STRING_H 1
#define HAVE_STRNCASECMP 1
#define HAVE_STRUCT_STAT_ST_BLKSIZE 1
#define HAVE_STRUCT_STAT_ST_BLOCKS 1
#define HAVE_STRUCT_STAT_ST_MTIM 1
#define HAVE_STRUCT_STAT_ST_RDEV 1
#define HAVE_STRVERSCMP 1
#define HAVE_ST_BLOCKS 1
#define HAVE_SYS_IOCTL_H 1
#define HAVE_SYS_MOUNT_H 1
#define HAVE_SYS_PARAM_H 1
#define HAVE_SYS_SELECT_H 1
#define HAVE_SYS_SOCKET_H 1
#define HAVE_SYS_STATFS_H 1
#define HAVE_SYS_STATVFS_H 1
#define HAVE_SYS_STAT_H 1
#define HAVE_SYS_TYPES_H 1
#define HAVE_UINTMAX_T 1
#define HAVE_UNISTD_H 1
#define HAVE_UNSIGNED_LONG_LONG_INT 1
#define HAVE_UTIMENSAT 1
#define HAVE_UTIME_H 1
#define HAVE_WCHAR_H 1

#define MAJOR_IN_SYSMACROS 1
#define MOUNTED_GETMNTENT1 1
#define PROMOTED_MODE_T mode_t
#define SIG_ATOMIC_VOLATILE_T volatile sig_atomic_t
#define SIZEOF_LONG 4
#define SIZEOF_OFF_T 8
#define SIZEOF_UINTMAX_T 8
#define STAT_STATVFS 1
#define major_t unsigned int
#define minor_t unsigned int
