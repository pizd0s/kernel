/*
 * The messages of errors and signals, strerror's and strsignal's, which
 * psignal and psiginfo write to the standard error, and the signals'
 * names, sig2str's and str2sig's: a signal's name without its SIG, RTMIN
 * and RTMAX, RTMIN+n of the real-time signals' first half and RTMAX-n of
 * the second, the number of one of none; str2sig takes IOT, CLD, POLL and
 * UNUSED too, and a number, of one in the signals' range. A message of an
 * error or signal of none, and of a real-time signal, is formatted, in a
 * room of its function's own.
 */
#include "sized.h"

#include <errno.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>
#include <sys/uio.h>
#include <unistd.h>

static const char* const errors[] = {
    [EPERM] = "Operation not permitted",
    [ENOENT] = "No such file or directory",
    [ESRCH] = "No such process",
    [EINTR] = "Interrupted system call",
    [EIO] = "Input/output error",
    [ENXIO] = "No such device or address",
    [E2BIG] = "Argument list too long",
    [ENOEXEC] = "Exec format error",
    [EBADF] = "Bad file descriptor",
    [ECHILD] = "No child processes",
    [EAGAIN] = "Resource temporarily unavailable",
    [ENOMEM] = "Cannot allocate memory",
    [EACCES] = "Permission denied",
    [EFAULT] = "Bad address",
    [ENOTBLK] = "Block device required",
    [EBUSY] = "Device or resource busy",
    [EEXIST] = "File exists",
    [EXDEV] = "Invalid cross-device link",
    [ENODEV] = "No such device",
    [ENOTDIR] = "Not a directory",
    [EISDIR] = "Is a directory",
    [EINVAL] = "Invalid argument",
    [ENFILE] = "Too many open files in system",
    [EMFILE] = "Too many open files",
    [ENOTTY] = "Inappropriate ioctl for device",
    [ETXTBSY] = "Text file busy",
    [EFBIG] = "File too large",
    [ENOSPC] = "No space left on device",
    [ESPIPE] = "Illegal seek",
    [EROFS] = "Read-only file system",
    [EMLINK] = "Too many links",
    [EPIPE] = "Broken pipe",
    [EDOM] = "Numerical argument out of domain",
    [ERANGE] = "Numerical result out of range",
    [EDEADLK] = "Resource deadlock avoided",
    [ENAMETOOLONG] = "File name too long",
    [ENOLCK] = "No locks available",
    [ENOSYS] = "Function not implemented",
    [ENOTEMPTY] = "Directory not empty",
    [ELOOP] = "Too many levels of symbolic links",
    [ENOMSG] = "No message of desired type",
    [EIDRM] = "Identifier removed",
    [ECHRNG] = "Channel number out of range",
    [EL2NSYNC] = "Level 2 not synchronized",
    [EL3HLT] = "Level 3 halted",
    [EL3RST] = "Level 3 reset",
    [ELNRNG] = "Link number out of range",
    [EUNATCH] = "Protocol driver not attached",
    [ENOCSI] = "No CSI structure available",
    [EL2HLT] = "Level 2 halted",
    [EBADE] = "Invalid exchange",
    [EBADR] = "Invalid request descriptor",
    [EXFULL] = "Exchange full",
    [ENOANO] = "No anode",
    [EBADRQC] = "Invalid request code",
    [EBADSLT] = "Invalid slot",
    [EBFONT] = "Bad font file format",
    [ENOSTR] = "Device not a stream",
    [ENODATA] = "No data available",
    [ETIME] = "Timer expired",
    [ENOSR] = "Out of streams resources",
    [ENONET] = "Machine is not on the network",
    [ENOPKG] = "Package not installed",
    [EREMOTE] = "Object is remote",
    [ENOLINK] = "Link has been severed",
    [EADV] = "Advertise error",
    [ESRMNT] = "Srmount error",
    [ECOMM] = "Communication error on send",
    [EPROTO] = "Protocol error",
    [EMULTIHOP] = "Multihop attempted",
    [EDOTDOT] = "RFS specific error",
    [EBADMSG] = "Bad message",
    [EOVERFLOW] = "Value too large for defined data type",
    [ENOTUNIQ] = "Name not unique on network",
    [EBADFD] = "File descriptor in bad state",
    [EREMCHG] = "Remote address changed",
    [ELIBACC] = "Can not access a needed shared library",
    [ELIBBAD] = "Accessing a corrupted shared library",
    [ELIBSCN] = ".lib section in a.out corrupted",
    [ELIBMAX] = "Attempting to link in too many shared libraries",
    [ELIBEXEC] = "Cannot exec a shared library directly",
    [EILSEQ] = "Invalid or incomplete multibyte or wide character",
    [ERESTART] = "Interrupted system call should be restarted",
    [ESTRPIPE] = "Streams pipe error",
    [EUSERS] = "Too many users",
    [ENOTSOCK] = "Socket operation on non-socket",
    [EDESTADDRREQ] = "Destination address required",
    [EMSGSIZE] = "Message too long",
    [EPROTOTYPE] = "Protocol wrong type for socket",
    [ENOPROTOOPT] = "Protocol not available",
    [EPROTONOSUPPORT] = "Protocol not supported",
    [ESOCKTNOSUPPORT] = "Socket type not supported",
    [EOPNOTSUPP] = "Operation not supported",
    [EPFNOSUPPORT] = "Protocol family not supported",
    [EAFNOSUPPORT] = "Address family not supported by protocol",
    [EADDRINUSE] = "Address already in use",
    [EADDRNOTAVAIL] = "Cannot assign requested address",
    [ENETDOWN] = "Network is down",
    [ENETUNREACH] = "Network is unreachable",
    [ENETRESET] = "Network dropped connection on reset",
    [ECONNABORTED] = "Software caused connection abort",
    [ECONNRESET] = "Connection reset by peer",
    [ENOBUFS] = "No buffer space available",
    [EISCONN] = "Transport endpoint is already connected",
    [ENOTCONN] = "Transport endpoint is not connected",
    [ESHUTDOWN] = "Cannot send after transport endpoint shutdown",
    [ETOOMANYREFS] = "Too many references: cannot splice",
    [ETIMEDOUT] = "Connection timed out",
    [ECONNREFUSED] = "Connection refused",
    [EHOSTDOWN] = "Host is down",
    [EHOSTUNREACH] = "No route to host",
    [EALREADY] = "Operation already in progress",
    [EINPROGRESS] = "Operation now in progress",
    [ESTALE] = "Stale file handle",
    [EUCLEAN] = "Structure needs cleaning",
    [ENOTNAM] = "Not a XENIX named type file",
    [ENAVAIL] = "No XENIX semaphores available",
    [EISNAM] = "Is a named type file",
    [EREMOTEIO] = "Remote I/O error",
    [EDQUOT] = "Disk quota exceeded",
    [ENOMEDIUM] = "No medium found",
    [EMEDIUMTYPE] = "Wrong medium type",
    [ECANCELED] = "Operation canceled",
    [ENOKEY] = "Required key not available",
    [EKEYEXPIRED] = "Key has expired",
    [EKEYREVOKED] = "Key has been revoked",
    [EKEYREJECTED] = "Key was rejected by service",
    [EOWNERDEAD] = "Owner died",
    [ENOTRECOVERABLE] = "State not recoverable",
    [ERFKILL] = "Operation not possible due to RF-kill",
};

static const char* const messages[] = {
    [SIGHUP] = "Hangup",
    [SIGINT] = "Interrupt",
    [SIGQUIT] = "Quit",
    [SIGILL] = "Illegal instruction",
    [SIGTRAP] = "Trace/breakpoint trap",
    [SIGABRT] = "Aborted",
    [SIGBUS] = "Bus error",
    [SIGFPE] = "Floating point exception",
    [SIGKILL] = "Killed",
    [SIGUSR1] = "User defined signal 1",
    [SIGSEGV] = "Segmentation fault",
    [SIGUSR2] = "User defined signal 2",
    [SIGPIPE] = "Broken pipe",
    [SIGALRM] = "Alarm clock",
    [SIGTERM] = "Terminated",
    [SIGSTKFLT] = "Stack fault",
    [SIGCHLD] = "Child exited",
    [SIGCONT] = "Continued",
    [SIGSTOP] = "Stopped (signal)",
    [SIGTSTP] = "Stopped",
    [SIGTTIN] = "Stopped (tty input)",
    [SIGTTOU] = "Stopped (tty output)",
    [SIGURG] = "Urgent I/O condition",
    [SIGXCPU] = "CPU time limit exceeded",
    [SIGXFSZ] = "File size limit exceeded",
    [SIGVTALRM] = "Virtual timer expired",
    [SIGPROF] = "Profiling timer expired",
    [SIGWINCH] = "Window changed",
    [SIGIO] = "I/O possible",
    [SIGPWR] = "Power failure",
    [SIGSYS] = "Bad system call",
};

static const char* const names[] = {
    [SIGHUP] = "HUP",
    [SIGINT] = "INT",
    [SIGQUIT] = "QUIT",
    [SIGILL] = "ILL",
    [SIGTRAP] = "TRAP",
    [SIGABRT] = "ABRT",
    [SIGBUS] = "BUS",
    [SIGFPE] = "FPE",
    [SIGKILL] = "KILL",
    [SIGUSR1] = "USR1",
    [SIGSEGV] = "SEGV",
    [SIGUSR2] = "USR2",
    [SIGPIPE] = "PIPE",
    [SIGALRM] = "ALRM",
    [SIGTERM] = "TERM",
    [SIGSTKFLT] = "STKFLT",
    [SIGCHLD] = "CHLD",
    [SIGCONT] = "CONT",
    [SIGSTOP] = "STOP",
    [SIGTSTP] = "TSTP",
    [SIGTTIN] = "TTIN",
    [SIGTTOU] = "TTOU",
    [SIGURG] = "URG",
    [SIGXCPU] = "XCPU",
    [SIGXFSZ] = "XFSZ",
    [SIGVTALRM] = "VTALRM",
    [SIGPROF] = "PROF",
    [SIGWINCH] = "WINCH",
    [SIGIO] = "IO",
    [SIGPWR] = "PWR",
    [SIGSYS] = "SYS",
};

#define COUNT(a) (sizeof(a) / sizeof((a)[0]))

static const char* errorMessage(int e, char* room, size_t size) {
    if (!e) {
        return "No error information";
    }

    if (e > 0 && (size_t)e < COUNT(errors) && errors[e]) {
        return errors[e];
    }

    snprintf(room, size, "Unknown error %d", e);

    return room;
}

static const char* signalMessage(int sig, char* room, size_t size) {
    if (sig >= SIGRTMIN && sig <= SIGRTMAX) {
        snprintf(room, size, "Real-time signal %d", sig - SIGRTMIN);
    } else if (sig > 0 && (size_t)sig < COUNT(messages) && messages[sig]) {
        return messages[sig];
    } else {
        snprintf(room, size, "Unknown signal %d", sig);
    }

    return room;
}

char* __kernel_strerror(int e) {
    static char room[32];

    return (char*)errorMessage(e, room, sizeof room);
}

int __kernel_strerror_r(int e, char* buf, size_t size) {
    char room[32];
    const char* message = errorMessage(e, room, sizeof room);
    size_t n = strlen(message);

    if (n >= size) {
        if (size) {
            memcpy(buf, message, size - 1);
            buf[size - 1] = 0;
        }

        return ERANGE;
    }

    memcpy(buf, message, n + 1);

    return 0;
}

char* __kernel_strsignal(int sig) {
    static char room[32];

    return (char*)signalMessage(sig, room, sizeof room);
}

// The prefix and its ": ", where there is one, then the message, in one
// write.
static void complain(const char* prefix, const char* message) {
    int named = prefix && *prefix;
    struct iovec parts[] = {
        {(void*)prefix, named ? strlen(prefix) : 0},
        {": ", named ? 2 : 0},
        {(void*)message, strlen(message)},
        {"\n", 1},
    };

    writev(STDERR_FILENO, parts, 4);
}

void __kernel_psignal(int sig, const char* prefix) {
    char room[32];

    complain(prefix, signalMessage(sig, room, sizeof room));
}

void __kernel_psiginfo(const siginfo_t* info, size_t size, const char* prefix) {
    sized("psiginfo", size, sizeof(siginfo_t));
    __kernel_psignal(info->si_signo, prefix);
}

int __kernel_sig2str(int sig, char* out) {
    if (sig == SIGRTMIN) {
        strcpy(out, "RTMIN");
    } else if (sig == SIGRTMAX) {
        strcpy(out, "RTMAX");
    } else if (sig > SIGRTMIN && sig <= (SIGRTMIN + SIGRTMAX) / 2) {
        snprintf(out, SIG2STR_MAX, "RTMIN+%d", sig - SIGRTMIN);
    } else if (sig > SIGRTMIN && sig < SIGRTMAX) {
        snprintf(out, SIG2STR_MAX, "RTMAX-%d", SIGRTMAX - sig);
    } else if (sig < 1 || sig >= NSIG) {
        return -1;
    } else if ((size_t)sig < COUNT(names) && names[sig]) {
        strcpy(out, names[sig]);
    } else {
        snprintf(out, SIG2STR_MAX, "%d", sig);
    }

    return 0;
}

// The number of a string of digits only, below limit, or -1.
static int number(const char* s, int limit) {
    int n = 0;

    if (!*s) {
        return -1;
    }

    for (; *s; s++) {
        if (*s < '0' || *s > '9' || (n = 10 * n + (*s - '0')) >= limit) {
            return -1;
        }
    }

    return n;
}

int __kernel_str2sig(const char* restrict name, int* restrict out) {
    static const struct {
        const char* name;
        int sig;
    } aliases[] = {{"IOT", SIGABRT}, {"CLD", SIGCHLD}, {"POLL", SIGIO}, {"UNUSED", SIGSYS}};
    char text[SIG2STR_MAX];
    int n;

    for (int sig = 1; sig < NSIG; sig++) {
        if (__kernel_sig2str(sig, text) == 0 && strcmp(text, name) == 0) {
            *out = sig;

            return 0;
        }
    }

    for (size_t i = 0; i < COUNT(aliases); i++) {
        if (strcmp(aliases[i].name, name) == 0) {
            *out = aliases[i].sig;

            return 0;
        }
    }

    if (strncmp(name, "RTMIN+", 6) == 0 || strncmp(name, "RTMAX-", 6) == 0) {
        if ((n = number(name + 6, SIGRTMAX - SIGRTMIN)) < 1) {
            return -1;
        }

        *out = name[3] == 'I' ? SIGRTMIN + n : SIGRTMAX - n;

        return 0;
    }

    if ((n = number(name, NSIG)) < 1) {
        return -1;
    }

    *out = n;

    return 0;
}
