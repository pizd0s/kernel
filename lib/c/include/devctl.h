#pragma once

#include <__base.h>

__BEGIN_DECLS

/*
 * posix_devctl, POSIX.1-2024's: a device's command is an ioctl request of
 * the device's, its data at most nbyte bytes; dev_info_ptr gets what the
 * ioctl returns.
 */
int posix_devctl(int, int, void* __restrict, size_t, int* __restrict);

__END_DECLS
