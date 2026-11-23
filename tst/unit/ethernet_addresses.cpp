//@ root: own
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "ethernet addresses: ok\n"

// Ethernet addresses as text: ether_aton and ether_aton_r take six bytes
// of hexadecimal digits joined by colons, of either case, each a byte's
// value, as musl's and FreeBSD's do, and refuse what is not; ether_ntoa and ether_ntoa_r write them back as six
// hexadecimal fields, which ether_aton reads as the same bytes. lib/c's
// alone, as musl's are stubs: ether_line reads a line of /etc/ethers, an
// address and a host name, and ether_ntohost and ether_hostton look the
// file up either way.

#include <assert.h>
#include <ctype.h>
#include <netinet/ether.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>

static bool sameBytes(const ether_addr* a, const unsigned char* want) {
    return memcmp(a->ether_addr_octet, want, 6) == 0;
}

// Six fields of one or two hexadecimal digits joined by colons.
static bool sixFields(const char* s) {
    for (int i = 0; i < 6; i++) {
        int digits = 0;
        while (isxdigit((unsigned char)*s)) {
            s++;
            digits++;
        }
        if (digits < 1 || digits > 2 || (i < 5 && *s++ != ':')) {
            return false;
        }
    }
    return *s == 0;
}

int main() {
    const unsigned char bytes[6] = {0x02, 0x00, 0x5e, 0x10, 0xab, 0xff};
    ether_addr* a = ether_aton("2:0:5e:10:AB:ff");
    assert(a && sameBytes(a, bytes));
    ether_addr b = {}, back = {};
    assert(ether_aton_r("02:00:5E:10:ab:FF", &b) == &b && sameBytes(&b, bytes));
    assert(!ether_aton("02:00:5e:10:ab") && !ether_aton("02:00:5e:10:ab:ff:01") && !ether_aton("0g:00:5e:10:ab:ff"));
    assert(ether_aton_r("002:00:5e:10:ab:ff", &back) && sameBytes(&back, bytes));
    assert(!ether_aton("100:00:5e:10:ab:ff") && !ether_aton(""));
    char* text = ether_ntoa(&b);
    assert(text && sixFields(text));
    assert(ether_aton_r(text, &back) && sameBytes(&back, bytes));
    char buf[32];
    assert(ether_ntoa_r(&b, buf) == buf && sixFields(buf) && ether_aton_r(buf, &back) && sameBytes(&back, bytes));

#ifdef __wasm__
    assert(strcmp(buf, "02:00:5e:10:ab:ff") == 0);
    char host[256] = {};
    assert(ether_line("02:00:5e:10:ab:ff  router  # the gateway", &back, host) == 0 && sameBytes(&back, bytes) && strcmp(host, "router") == 0);
    assert(ether_line("# only a comment", &back, host) == -1 && ether_line("bad router", &back, host) == -1);
    assert(mkdir("/etc", 0755) == 0);
    FILE* ethers = fopen("/etc/ethers", "w");
    assert(ethers && fputs("# test\n02:00:5e:10:ab:ff router\nbad line here\n0:1:2:3:4:5 printer\n", ethers) >= 0 && fclose(ethers) == 0);
    assert(ether_ntohost(host, &b) == 0 && strcmp(host, "router") == 0);
    ether_addr printer = {};
    const unsigned char printed[6] = {0, 1, 2, 3, 4, 5};
    assert(ether_hostton("printer", &printer) == 0 && sameBytes(&printer, printed));
    assert(ether_hostton("missing", &printer) == -1);
    ether_addr none = {{9, 9, 9, 9, 9, 9}};
    assert(ether_ntohost(host, &none) == -1);
#endif
    printf("ethernet addresses: ok\n");
}
