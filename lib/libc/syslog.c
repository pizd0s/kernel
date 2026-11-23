#include "kernel.h"

#include <stdarg.h>
#include <stdio.h>
#include <syslog.h>

void __kernel_vsyslog(int priority, const char* format, va_list args) {
    char message[1024];

    vsnprintf(message, sizeof message, format, args);
    __kernel_log_message(priority, message);
}

// syslog takes its va_list, as a program's forwarder passes it.
void __kernel_syslog(int priority, const char* format, va_list args) {
    __kernel_vsyslog(priority, format, args);
}
