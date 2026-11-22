//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "file tree walks: ok\n"

// nftw, POSIX.1-2024's, and ftw, POSIX.1-2017's (2024 removed it): a
// tree of files, directories and links, its every name reported once
// whatever fd_limit, each directory before what is in it or, with
// FTW_DEPTH, after; with FTW_PHYS links reported as FTW_SL, without it
// followed, a dangling one FTW_SLN and a directory met again under itself
// reported but not entered, with FTW_DEPTH not reported; FTW_CHDIR's
// working directory, the directory of the name reported, and the
// caller's back at the end; FTW_MOUNT and FTW_XDEV at another file system
// bound in the tree; fn's value ending the walk; the errors of the path.
// ftw is nftw with FTW_PHYS, as stat() or lstat() would, which 2017
// allows, calling fn with its three arguments. host: false: the host
// musl's nftw treats fd_limit as a depth and stops there, ignores
// FTW_CHDIR and has no FTW_XDEV.

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <ftw.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <unistd.h>

namespace {
    struct Report {
        char path[256];
        int type;
        int level;
        int base;
        int order;
        dev_t dev;
    };

    Report reports[8192];
    int count;
    int home = -1;
    int stopAt = -1;
    bool chdirChecked;

    int record(const char* path, const struct stat* st, int type, struct FTW* ftw) {
        assert(count < 8192 && strlen(path) < sizeof reports[0].path);
        Report& r = reports[count];
        strcpy(r.path, path);
        r.type = type;
        r.level = ftw->level;
        r.base = ftw->base;
        r.order = count++;
        r.dev = st->st_dev;
        const char* slash = strrchr(path, '/');
        assert(ftw->base == (slash && slash[1] ? slash - path + 1 : 0));
        if (type == FTW_D || type == FTW_DP) {
            assert(S_ISDIR(st->st_mode));
        } else if (type == FTW_SL || type == FTW_SLN) {
            assert(S_ISLNK(st->st_mode));
        } else if (type == FTW_F) {
            assert(!S_ISDIR(st->st_mode));
        }
        if (chdirChecked && ftw->level > 0) {
            // The working directory is the reported name's directory.
            char parent[256];
            memcpy(parent, path, ftw->base - 1);
            parent[ftw->base - 1] = 0;
            struct stat here;
            struct stat there;
            assert(stat(".", &here) == 0 && fstatat(home, parent, &there, 0) == 0);
            assert(here.st_dev == there.st_dev && here.st_ino == there.st_ino);
            struct stat self;
            assert(fstatat(AT_FDCWD, path + ftw->base, &self, AT_SYMLINK_NOFOLLOW) == 0);
        }
        return count - 1 == stopAt ? 7 : 0;
    }

    const Report* find(const char* path) {
        for (int i = 0; i < count; i++) {
            if (strcmp(reports[i].path, path) == 0) {
                return &reports[i];
            }
        }
        return nullptr;
    }

    // Each path once, of its type, and nothing else.
    void expect(const char* const* paths, const int* types, int n) {
        assert(count == n);
        for (int i = 0; i < n; i++) {
            const Report* r = find(paths[i]);
            if (!r || r->type != types[i]) {
                printf("%s: %d, expected %d\n", paths[i], r ? r->type : -1, types[i]);
                assert(false);
            }
        }
    }

    // Directories before or, with depth, after what is in them.
    void ordered(bool depth) {
        for (int i = 0; i < count; i++) {
            const Report& child = reports[i];
            for (int j = 0; j < count; j++) {
                const Report& dir = reports[j];
                size_t n = strlen(dir.path);
                if (i != j && strncmp(child.path, dir.path, n) == 0 && child.path[n] == '/') {
                    assert(depth ? dir.order > child.order : dir.order < child.order);
                    assert(child.level > dir.level);
                }
            }
        }
    }

    // ftw's function: its level is the slashes of a path under "tree".
    int simple(const char* path, const struct stat* st, int type) {
        const char* slash = strrchr(path, '/');
        struct FTW ftw = {slash && slash[1] ? (int)(slash - path + 1) : 0, 0};
        for (const char* c = path; *c; c++) {
            ftw.level += *c == '/';
        }
        return record(path, st, type, &ftw);
    }

    void make() {
        assert(mkdir("tree", 0755) == 0 && mkdir("tree/b", 0755) == 0 && mkdir("tree/b/d", 0755) == 0);
        assert(mkdir("tree/b/d/e", 0755) == 0);
        const char* files[] = {"tree/a", "tree/b/c", "tree/b/d/e/f"};
        for (const char* path : files) {
            int fd = open(path, O_CREAT | O_WRONLY, 0644);
            assert(fd >= 0 && close(fd) == 0);
        }
        assert(symlink("a", "tree/l") == 0 && symlink("b", "tree/m") == 0 && symlink("none", "tree/n") == 0);
        assert(symlink(".", "tree/o") == 0);
    }
}

int main() {
    make();
    home = open(".", O_RDONLY | O_DIRECTORY);
    assert(home >= 0);

    const char* physical[] = {"tree", "tree/a", "tree/b", "tree/b/c", "tree/b/d", "tree/b/d/e", "tree/b/d/e/f",
                              "tree/l", "tree/m", "tree/n", "tree/o"};
    const int physicalTypes[] = {FTW_D, FTW_F, FTW_D, FTW_F, FTW_D, FTW_D, FTW_F, FTW_SL, FTW_SL, FTW_SL, FTW_SL};
    const int limits[] = {1, 2, 20};
    for (int limit : limits) {
        count = 0;
        assert(nftw("tree", record, limit, FTW_PHYS) == 0);
        expect(physical, physicalTypes, 11);
        ordered(false);
        assert(find("tree")->level == 0 && find("tree/b/d/e/f")->level == 4 && find("tree/b/c")->base == 7);
    }

    const int depthTypes[] = {FTW_DP, FTW_F, FTW_DP, FTW_F, FTW_DP, FTW_DP, FTW_F, FTW_SL, FTW_SL, FTW_SL, FTW_SL};
    count = 0;
    assert(nftw("tree/", record, 1, FTW_PHYS | FTW_DEPTH) == 0);
    assert(count == 11 && find("tree/") && find("tree/b/d/e/f") && find("tree/")->base == 0);
    for (int i = 0; i < 11; i++) {
        const Report* r = find(i ? physical[i] : "tree/");
        assert(r && r->type == depthTypes[i]);
    }
    ordered(true);

    // Links followed: a file's is the file, a directory's is entered, a
    // dangling one is FTW_SLN, one back to the top is not entered.
    const char* followed[] = {"tree", "tree/a", "tree/b", "tree/b/c", "tree/b/d", "tree/b/d/e", "tree/b/d/e/f",
                              "tree/l", "tree/m", "tree/m/c", "tree/m/d", "tree/m/d/e", "tree/m/d/e/f", "tree/n",
                              "tree/o"};
    const int followedTypes[] = {FTW_D, FTW_F, FTW_D, FTW_F, FTW_D, FTW_D, FTW_F, FTW_F,
                                 FTW_D, FTW_F, FTW_D, FTW_D, FTW_F, FTW_SLN, FTW_D};
    count = 0;
    assert(nftw("tree", record, 3, 0) == 0);
    expect(followed, followedTypes, 15);
    ordered(false);
    count = 0;
    assert(nftw("tree", record, 3, FTW_DEPTH) == 0);
    assert(count == 14 && !find("tree/o") && find("tree/m")->type == FTW_DP && find("tree")->type == FTW_DP);
    ordered(true);

    // FTW_CHDIR: each name reported from its directory, the walk's own
    // working directory back at the end, with streams kept or reread.
    for (int limit : limits) {
        count = 0;
        chdirChecked = true;
        assert(nftw("tree", record, limit, FTW_PHYS | FTW_CHDIR) == 0);
        chdirChecked = false;
        expect(physical, physicalTypes, 11);
        struct stat here;
        struct stat start;
        assert(stat(".", &here) == 0 && fstat(home, &start) == 0 && here.st_ino == start.st_ino);
    }
    char absolute[256];
    assert(getcwd(absolute, sizeof absolute - 8));
    strcat(absolute, "/tree");
    count = 0;
    chdirChecked = true;
    assert(nftw(absolute, record, 1, FTW_CHDIR | FTW_DEPTH) == 0 && count == 14);
    chdirChecked = false;

    // fn's value ends the walk; the path's errors.
    count = 0;
    stopAt = 3;
    assert(nftw("tree", record, 4, FTW_PHYS) == 7 && count == 4);
    stopAt = -1;
    errno = 0;
    assert(nftw("none", record, 4, 0) == -1 && errno == ENOENT);
    assert(nftw("", record, 4, 0) == -1 && errno == ENOENT);
    assert(nftw("tree/a/x", record, 4, 0) == -1 && errno == ENOTDIR);
    count = 0;
    assert(nftw("tree/a", record, 4, 0) == 0 && count == 1 && reports[0].type == FTW_F && reports[0].base == 5);

    // ftw: a link is FTW_SL, as lstat() would have it, the whole tree
    // whatever ndirs.
    count = 0;
    assert(ftw("tree", simple, 1) == 0);
    expect(physical, physicalTypes, 11);
    ordered(false);

    // Another file system in the tree, /dev's bound at tree/x: FTW_XDEV
    // reports it and nothing in it, FTW_MOUNT not even it.
    struct stat tree;
    struct stat bound;
    assert(mkdir("tree/x", 0755) == 0 && mount("/dev", "tree/x", nullptr, 0, nullptr) == 0);
    assert(stat("tree", &tree) == 0 && stat("tree/x", &bound) == 0 && tree.st_dev != bound.st_dev);
    count = 0;
    assert(nftw("tree", record, 4, FTW_PHYS) == 0 && count > 12 && find("tree/x"));
    int entered = count;
    count = 0;
    assert(nftw("tree", record, 4, FTW_PHYS | FTW_XDEV) == 0 && count == 12 && find("tree/x"));
    count = 0;
    assert(nftw("tree", record, 4, FTW_PHYS | FTW_MOUNT) == 0 && count == 11 && !find("tree/x"));
    count = 0;
    assert(nftw("tree", record, 4, FTW_PHYS | FTW_MOUNT | FTW_XDEV) == 0 && count == 11);
    assert(umount("tree/x") == 0 && entered > 12);

    printf("file tree walks: ok\n");
}
