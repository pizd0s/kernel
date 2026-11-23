// test_libc_link's program: ndbm's dbm_open and dbm_close, which libc.wasm
// defines, functions of its own through a table (make.py's), and a trap on
// its line.
#include <fcntl.h>
#include <ndbm.h>

extern int (*const functions[])(int);
extern const int count;

int main(int argc, char** argv) {
    DBM* db = dbm_open("/no/such/place", O_RDONLY, 0);
    int sum = 0;

    if (db) {
        dbm_close(db);
    }

    for (int i = 0; i < count; i++) {
        sum += functions[i](argc);
    }

    if (sum != -1) {
        __builtin_trap();
    }

    return argv[0][0];
}
