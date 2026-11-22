//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "standard 2024 additions: ok\n"
//@ host: false

// Functions POSIX.1-2024 added, as it has them; musl 1.2.5 has none of
// them. The waits by a clock time out against CLOCK_REALTIME or
// CLOCK_MONOTONIC (a lock the thread holds itself may be EDEADLK, as
// POSIX lets it) and refuse other clocks. wcslcpy and wcslcat copy what
// fits and return the lengths they would make. sig2str names signals
// without SIG, the real-time ones by RTMIN and RTMAX, and str2sig reads
// back every name it gives and numbers. tcgetwinsize and tcsetwinsize are
// a terminal's window size. A locale object has its categories' names,
// which getlocalename_l gives, LC_ALL's as setlocale takes them;
// uselocale switches the thread's locale. The gettext _l forms give the
// message, there being no catalogs. posix_getdents reads a directory's
// entries from its offset, EINVAL where the next one does not fit, the
// offset then kept. posix_devctl is a device's ioctl with its data's size,
// returning the error.

#include <assert.h>
#include <devctl.h>
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <libintl.h>
#include <locale.h>
#include <pthread.h>
#include <pty.h>
#include <semaphore.h>
#include <set>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <string>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <termios.h>
#include <time.h>
#include <unistd.h>
#include <wchar.h>

static timespec after(clockid_t clock, long ms) {
    timespec t;
    assert(clock_gettime(clock, &t) == 0);
    t.tv_nsec += ms * 1000000;
    t.tv_sec += t.tv_nsec / 1000000000;
    t.tv_nsec %= 1000000000;
    return t;
}

static void clockWaits() {
    pthread_mutex_t mutex = PTHREAD_MUTEX_INITIALIZER;
    timespec soon = after(CLOCK_MONOTONIC, 10);
    assert(pthread_mutex_clocklock(&mutex, CLOCK_MONOTONIC, &soon) == 0);
    soon = after(CLOCK_MONOTONIC, 10);
    assert(pthread_mutex_clocklock(&mutex, CLOCK_MONOTONIC, &soon) == ETIMEDOUT);
    soon = after(CLOCK_REALTIME, 10);
    assert(pthread_mutex_clocklock(&mutex, CLOCK_REALTIME, &soon) == ETIMEDOUT);
    assert(pthread_mutex_clocklock(&mutex, CLOCK_PROCESS_CPUTIME_ID, &soon) == EINVAL);

    pthread_cond_t cond = PTHREAD_COND_INITIALIZER;
    soon = after(CLOCK_MONOTONIC, 10);
    assert(pthread_cond_clockwait(&cond, &mutex, CLOCK_MONOTONIC, &soon) == ETIMEDOUT);
    assert(pthread_mutex_trylock(&mutex) == EBUSY);
    soon = after(CLOCK_REALTIME, 10);
    assert(pthread_cond_clockwait(&cond, &mutex, CLOCK_REALTIME, &soon) == ETIMEDOUT);
    assert(pthread_cond_clockwait(&cond, &mutex, CLOCK_THREAD_CPUTIME_ID, &soon) == EINVAL);
    assert(pthread_mutex_unlock(&mutex) == 0);

    pthread_rwlock_t lock = PTHREAD_RWLOCK_INITIALIZER;
    soon = after(CLOCK_MONOTONIC, 10);
    assert(pthread_rwlock_clockrdlock(&lock, CLOCK_MONOTONIC, &soon) == 0);
    int r = pthread_rwlock_clockwrlock(&lock, CLOCK_MONOTONIC, &soon);
    assert(r == ETIMEDOUT || r == EDEADLK);
    assert(pthread_rwlock_unlock(&lock) == 0);
    soon = after(CLOCK_REALTIME, 10);
    assert(pthread_rwlock_clockwrlock(&lock, CLOCK_REALTIME, &soon) == 0);
    r = pthread_rwlock_clockrdlock(&lock, CLOCK_REALTIME, &soon);
    assert(r == ETIMEDOUT || r == EDEADLK);
    assert(pthread_rwlock_clockrdlock(&lock, CLOCK_PROCESS_CPUTIME_ID, &soon) == EINVAL);
    assert(pthread_rwlock_unlock(&lock) == 0);

    sem_t sem;
    assert(sem_init(&sem, 0, 1) == 0);
    soon = after(CLOCK_MONOTONIC, 10);
    assert(sem_clockwait(&sem, CLOCK_MONOTONIC, &soon) == 0);
    soon = after(CLOCK_MONOTONIC, 10);
    assert(sem_clockwait(&sem, CLOCK_MONOTONIC, &soon) == -1 && errno == ETIMEDOUT);
    assert(sem_clockwait(&sem, CLOCK_PROCESS_CPUTIME_ID, &soon) == -1 && errno == EINVAL);
    assert(sem_destroy(&sem) == 0);
}

static void wideCopies() {
    wchar_t d[8];
    errno = EBADF;
    assert(wcslcpy(d, L"abcdef", 4) == 6 && wcscmp(d, L"abc") == 0);
    assert(wcslcpy(d, L"xy", 8) == 2 && wcscmp(d, L"xy") == 0);
    d[0] = L'q';
    assert(wcslcpy(d, L"long", 0) == 4 && d[0] == L'q');
    wcscpy(d, L"ab");
    assert(wcslcat(d, L"cdefgh", 6) == 8 && wcscmp(d, L"abcde") == 0);
    assert(wcslcat(d, L"zz", 3) == 5 && wcscmp(d, L"abcde") == 0);
    wcscpy(d, L"ab");
    assert(wcslcat(d, L"cd", 8) == 4 && wcscmp(d, L"abcd") == 0);
    assert(errno == EBADF);
}

static void signalNames() {
    char name[SIG2STR_MAX];
    int sig = 0;
    assert(sig2str(SIGINT, name) == 0 && strcmp(name, "INT") == 0);
    assert(sig2str(SIGKILL, name) == 0 && strcmp(name, "KILL") == 0);
    assert(sig2str(SIGRTMIN, name) == 0 && strcmp(name, "RTMIN") == 0);
    assert(sig2str(SIGRTMAX, name) == 0 && strcmp(name, "RTMAX") == 0);
    assert(sig2str(SIGRTMIN + 1, name) == 0 && strcmp(name, "RTMIN+1") == 0);
    assert(sig2str((SIGRTMIN + SIGRTMAX) / 2, name) == 0 && strcmp(name, "RTMIN+15") == 0);
    assert(sig2str(SIGRTMAX - 1, name) == 0 && (strcmp(name, "RTMAX-1") == 0 || strcmp(name, "RTMIN+29") == 0));
    for (int s = 1; s < NSIG; s++) {
        assert(sig2str(s, name) == 0 && strlen(name) < SIG2STR_MAX && strncmp(name, "SIG", 3) != 0);
        assert(str2sig(name, &sig) == 0 && sig == s);
        char number[8];
        snprintf(number, sizeof number, "%d", s);
        assert(str2sig(number, &sig) == 0 && sig == s);
    }
    assert(sig2str(-1, name) == -1 && sig2str(NSIG, name) == -1);
    assert(str2sig("TERM", &sig) == 0 && sig == SIGTERM);
    assert(str2sig("RTMIN+3", &sig) == 0 && sig == SIGRTMIN + 3);
    assert(str2sig("RTMAX-2", &sig) == 0 && sig == SIGRTMAX - 2);
    sig = 77;
    assert(str2sig("bogus", &sig) == -1 && str2sig("RTMIN+99", &sig) == -1 && str2sig("999", &sig) == -1 && sig == 77);
}

static void windowSizes() {
    int master, slave;
    assert(openpty(&master, &slave, nullptr, nullptr, nullptr) == 0);
    winsize set = {30, 100, 0, 0}, got = {};
    assert(tcsetwinsize(slave, &set) == 0);
    assert(tcgetwinsize(master, &got) == 0 && got.ws_row == 30 && got.ws_col == 100);

    got = {};
    int info = -1;
    assert(posix_devctl(slave, TIOCGWINSZ, &got, sizeof got, &info) == 0 && got.ws_row == 30 && got.ws_col == 100 && info == 0);
    errno = EBADF;
    assert(posix_devctl(slave, TIOCGWINSZ, &got, 4, nullptr) == EINVAL);
    int queue = TCIFLUSH;
    assert(posix_devctl(slave, TCFLSH, &queue, sizeof queue, nullptr) == 0);
    assert(posix_devctl(-1, TIOCGWINSZ, &got, sizeof got, nullptr) == EBADF);
    int file = open("window-file", O_RDWR | O_CREAT | O_TRUNC, 0644);
    assert(file >= 0 && posix_devctl(file, TIOCGWINSZ, &got, sizeof got, nullptr) == ENOTTY && errno == EBADF);

    assert(tcgetwinsize(file, &got) == -1 && errno == ENOTTY);
    assert(tcgetwinsize(-1, &got) == -1 && errno == EBADF);
    assert(close(file) == 0 && unlink("window-file") == 0 && close(slave) == 0 && close(master) == 0);
}

static void localeNames() {
    locale_t l = newlocale(LC_ALL_MASK, "C", nullptr);
    assert(l && strcmp(getlocalename_l(LC_CTYPE, l), "C") == 0 && strcmp(getlocalename_l(LC_ALL, l), "C") == 0);
    assert(newlocale(LC_CTYPE_MASK, "C.UTF-8", l) == l);
    assert(strcmp(getlocalename_l(LC_CTYPE, l), "C.UTF-8") == 0 && strcmp(getlocalename_l(LC_NUMERIC, l), "C") == 0);
    std::string all = getlocalename_l(LC_ALL, l);
    locale_t copy = duplocale(l);
    assert(copy && copy != l && all == getlocalename_l(LC_ALL, copy));
    assert(getlocalename_l(LC_ALL + 1, l) == nullptr);

    std::string global = setlocale(LC_ALL, nullptr);
    assert(global == getlocalename_l(LC_ALL, LC_GLOBAL_LOCALE));
    assert(setlocale(LC_ALL, all.c_str()) && strcmp(setlocale(LC_CTYPE, nullptr), "C.UTF-8") == 0);
    assert(strcmp(setlocale(LC_NUMERIC, nullptr), "C") == 0 && all == getlocalename_l(LC_ALL, LC_GLOBAL_LOCALE));
    assert(setlocale(LC_ALL, global.c_str()));

    assert(uselocale(nullptr) == LC_GLOBAL_LOCALE && uselocale(l) == LC_GLOBAL_LOCALE && uselocale(nullptr) == l);
    assert(uselocale(LC_GLOBAL_LOCALE) == l && uselocale(nullptr) == LC_GLOBAL_LOCALE);
    errno = 0;
    assert(!newlocale(LC_ALL_MASK, "en_US.ISO-8859-1", nullptr) && errno == ENOENT);
    errno = 0;
    assert(!newlocale(LC_ALL_MASK, nullptr, nullptr) && errno == EINVAL);
    freelocale(copy);
    freelocale(l);

    l = newlocale(LC_ALL_MASK, "C.UTF-8", nullptr);
    const char* one = "file";
    const char* many = "files";
    assert(gettext_l(one, l) == one && dgettext_l("domain", one, l) == one && dcgettext_l("domain", one, LC_MESSAGES, l) == one);
    assert(ngettext_l(one, many, 1, l) == one && ngettext_l(one, many, 2, l) == many);
    assert(dngettext_l("domain", one, many, 0, l) == many && dcngettext_l("domain", one, many, 1, LC_MESSAGES, l) == one);
    freelocale(l);
}

static std::set<std::string> entries(int fd, size_t room) {
    std::set<std::string> names;
    alignas(8) char buf[4096];
    for (;;) {
        ssize_t n = posix_getdents(fd, buf, room, 0);
        assert(n >= 0);
        if (n == 0) {
            return names;
        }
        for (ssize_t at = 0; at < n;) {
            posix_dent* e = (posix_dent*)(buf + at);
            assert(e->d_reclen % 8 == 0 && e->d_reclen > 0 && at + e->d_reclen <= n);
            std::string name = e->d_name;
            assert(!name.empty() && names.insert(name).second);
            struct stat st;
            if (name != "." && name != "..") {
                assert(fstatat(fd, e->d_name, &st, AT_SYMLINK_NOFOLLOW) == 0 && st.st_ino == e->d_ino);
                assert(e->d_type == (S_ISDIR(st.st_mode) ? DT_DIR : DT_REG));
            }
            at += e->d_reclen;
        }
    }
}

static void directoryEntries() {
    char work[] = "entries-XXXXXX";
    assert(mkdtemp(work) == work && chdir(work) == 0);
    for (const char* name : {"a", "bb", "ccc"}) {
        int fd = open(name, O_WRONLY | O_CREAT, 0644);
        assert(fd >= 0 && close(fd) == 0);
    }
    assert(mkdir("sub", 0755) == 0);
    int dir = open(".", O_RDONLY | O_DIRECTORY);
    assert(dir >= 0);
    alignas(8) char small[16];
    assert(posix_getdents(dir, small, sizeof small, 0) == -1 && errno == EINVAL);
    assert(posix_getdents(dir, small, sizeof small, 1) == -1 && errno == EINVAL);
    std::set<std::string> names = entries(dir, 4096);
    assert(names.count(".") == names.count(".."));
    names.erase(".");
    names.erase("..");
    assert(names == (std::set<std::string>{"a", "bb", "ccc", "sub"}));
    assert(posix_getdents(dir, small, sizeof small, 0) == 0);
    assert(lseek(dir, 0, SEEK_SET) == 0);
    std::set<std::string> again = entries(dir, 32);
    again.erase(".");
    again.erase("..");
    assert(again == names);

    int file = open("a", O_RDONLY);
    assert(file >= 0 && posix_getdents(file, small, sizeof small, 0) == -1 && errno == ENOTDIR);
    int path = open(".", O_PATH);
    assert(path >= 0 && posix_getdents(path, small, sizeof small, 0) == -1 && errno == EBADF);
    assert(posix_getdents(-1, small, sizeof small, 0) == -1 && errno == EBADF);
    assert(close(path) == 0 && close(file) == 0 && close(dir) == 0);
}

int main() {
    clockWaits();
    wideCopies();
    signalNames();
    windowSizes();
    localeNames();
    directoryEntries();
    printf("standard 2024 additions: ok\n");
}
