#pragma once

#include <__base.h>
#include <stdint.h>

__BEGIN_DECLS

/*
 * Ethernet addresses as text, the libcs': six hexadecimal bytes joined by
 * colons, each of as many digits as its value takes, written lower case
 * with two digits each;
 * the host names of /etc/ethers, a line an address and its name.
 */
#define ETHER_ADDR_LEN 6

struct ether_addr {
    uint8_t ether_addr_octet[ETHER_ADDR_LEN];
};

char* ether_ntoa(const struct ether_addr*);
char* ether_ntoa_r(const struct ether_addr*, char*);
struct ether_addr* ether_aton(const char*);
struct ether_addr* ether_aton_r(const char*, struct ether_addr*);
int ether_ntohost(char*, const struct ether_addr*);
int ether_hostton(const char*, struct ether_addr*);
int ether_line(const char*, struct ether_addr*, char*);

__END_DECLS
