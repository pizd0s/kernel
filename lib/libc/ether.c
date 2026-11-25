/*
 * Ethernet addresses: six bytes as text of hex numbers joined by colons,
 * and /etc/ethers, of lines of such an address and a host's name, a #
 * beginning a comment.
 */
#include <net/ethernet.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int hex(char c) {
    return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F');
}

// The address text begins with, and where it ends: at its end or a blank.
static const char* parse(const char* s, struct ether_addr* addr) {
    for (int i = 0; i < ETHER_ADDR_LEN; i++) {
        unsigned value = 0;
        int n = 0;

        for (; hex(s[n]); n++) {
            value = value * 16 + (s[n] <= '9' ? s[n] - '0' : (s[n] | 32) - 'a' + 10);

            if (value > 255) {
                return NULL;
            }
        }

        if (n == 0) {
            return NULL;
        }

        addr->ether_addr_octet[i] = value;
        s += n;

        if (i < ETHER_ADDR_LEN - 1) {
            if (*s != ':') {
                return NULL;
            }

            s++;
        }
    }

    return !*s || *s == ' ' || *s == '\t' || *s == '\n' ? s : NULL;
}

struct ether_addr* __kernel_ether_aton_r(const char* text, struct ether_addr* addr) {
    struct ether_addr a;

    if (!parse(text, &a)) {
        return NULL;
    }

    *addr = a;

    return addr;
}

struct ether_addr* __kernel_ether_aton(const char* text) {
    static struct ether_addr addr;

    return __kernel_ether_aton_r(text, &addr);
}

char* __kernel_ether_ntoa_r(const struct ether_addr* addr, char* buf) {
    const unsigned char* o = addr->ether_addr_octet;

    sprintf(buf, "%02x:%02x:%02x:%02x:%02x:%02x", o[0], o[1], o[2], o[3], o[4], o[5]);

    return buf;
}

char* __kernel_ether_ntoa(const struct ether_addr* addr) {
    static char text[18];

    return __kernel_ether_ntoa_r(addr, text);
}

// A line's address and name, its first two fields: their places in line,
// which ends each, or null.
static char* fields(char* line, struct ether_addr* addr) {
    static const char blank[] = " \t\n\r\f\v";

    line[strcspn(line, "#")] = 0;

    char* first = line + strspn(line, blank);
    char* end = first + strcspn(first, blank);
    char* name = end + strspn(end, blank);

    if (end == first || !*name) {
        return NULL;
    }

    name[strcspn(name, blank)] = 0;
    *end = 0;

    return parse(first, addr) ? name : NULL;
}

int __kernel_ether_line(const char* line, struct ether_addr* addr, char* host) {
    char* copy = strdup(line);
    struct ether_addr a;
    char* name = copy ? fields(copy, &a) : NULL;

    if (name) {
        *addr = a;
        strcpy(host, name);
    }

    free(copy);

    return name ? 0 : -1;
}

// Finds the line of /etc/ethers of the address or the name: 0, with both
// in addr and host, or -1.
static int find(struct ether_addr* addr, char* host, int byName) {
    FILE* f = fopen("/etc/ethers", "re");
    char* line = NULL;
    size_t size = 0;
    int found = -1;

    while (f && found < 0 && getline(&line, &size, f) >= 0) {
        struct ether_addr a;
        char* name = fields(line, &a);

        if (name && (byName ? strcmp(name, host) == 0 : memcmp(&a, addr, sizeof a) == 0)) {
            *addr = a;
            strcpy(host, name);
            found = 0;
        }
    }

    free(line);

    if (f) {
        fclose(f);
    }

    return found;
}

int __kernel_ether_ntohost(char* host, const struct ether_addr* addr) {
    struct ether_addr a = *addr;

    return find(&a, host, 0);
}

int __kernel_ether_hostton(const char* host, struct ether_addr* addr) {
    char name[strlen(host) + 1];

    strcpy(name, host);

    return find(addr, name, 1);
}
