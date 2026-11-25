/*
 * The system's log: a message is a line of openlog's identity, with
 * LOG_PID the process's id in brackets, ": " and the message, written to
 * /dev/console and with LOG_PERROR to standard error too; one of a
 * priority setlogmask's mask leaves out is not written. The identity is
 * at most 31 bytes, empty before openlog.
 */
#include <errno.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdio.h>
#include <string.h>
#include <syslog.h>
#include <unistd.h>

static char identity[32];
static int options;
static int masked;

void __kernel_openlog(const char* ident, int option, int facility) {
    snprintf(identity, sizeof identity, "%s", ident ? ident : "");
    options = option;
}

void __kernel_closelog(void) {
}

int __kernel_setlogmask(int mask) {
    int old = ~masked & 0xff;

    if (mask) {
        masked = ~mask & 0xff;
    }

    return old;
}

void __kernel_vsyslog(int priority, const char* format, va_list args) {
    char message[1024], line[1100];
    int saved = errno;

    if (masked & 1 << (priority & 7)) {
        return;
    }

    vsnprintf(message, sizeof message, format, args);

    int n = options & LOG_PID ? snprintf(line, sizeof line - 1, "%s[%d]: %s", identity, (int)getpid(), message)
                              : snprintf(line, sizeof line - 1, "%s: %s", identity, message);

    n = n < (int)sizeof line - 2 ? n : (int)sizeof line - 2;

    if (n == 0 || line[n - 1] != '\n') {
        line[n++] = '\n';
    }

    int console = open("/dev/console", O_WRONLY | O_NOCTTY | O_CLOEXEC);

    if (console >= 0) {
        write(console, line, n);
        close(console);
    }

    if (options & LOG_PERROR) {
        write(2, line, n);
    }

    errno = saved;
}

// syslog takes its va_list, as a program's forwarder passes it.
void __kernel_syslog(int priority, const char* format, va_list args) {
    __kernel_vsyslog(priority, format, args);
}
