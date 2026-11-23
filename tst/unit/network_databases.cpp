//@ root: own
//@ host: false
//@ compile-flags: -g
//@ stdout: "network databases: ok\n"
//@ stderr: "netdb: Host not found\nUnknown error\n"

#include <arpa/inet.h>
#include <assert.h>
#include <netdb.h>
#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <time.h>
#include <unistd.h>

// The databases of the files in /etc: hosts, services, protocols and
// networks, an entry a line with its aliases; their enumerations; the
// lookups by name, alias, number and address; the reentrant forms, which
// write an entry's strings and lists into the buffer given, aligned, and
// return ERANGE where it is too small and a null result where there is no
// entry; getaddrinfo and getnameinfo over them.
//
// h_errno and herror are an earlier lib/c's: the place of the host
// functions' error, which the program's start gave the kernel, and the
// kernel's herror, which reads it. The program starts once more as that
// lib/c did, with such a place of its own.
extern "C" void earlierStart(int*, char***, int*, char***, char**, long*, int*, int*)
    __attribute__((import_module("kernel"), import_name("__kernel_start")));
extern "C" void herror(const char*) __attribute__((import_module("kernel"), import_name("herror")));

static int h_errno;

static void fixture(const char* path, const char* text) {
    FILE* file = fopen(path, "w");
    assert(file && fputs(text, file) >= 0 && fclose(file) == 0);
}

int main() {
    int count = 0;
    char** arguments = nullptr;
    earlierStart(&errno, &environ, &count, &arguments, tzname, &timezone, &daylight, &h_errno);
    assert(count == 1 && arguments[0]);
    assert(mkdir("/etc", 0700) == 0);
    fixture("/etc/hosts", "# isolated test database\nbad\nnot-an-ip invalid\n192.0.2.10 alpha.test alias\n2001:db8::10 beta.test beta6\n");
    fixture("/etc/services", "# isolated services\nbad\ninvalid x/tcp\npizdos-stream 4101/tcp stream-alias\npizdos-dgram 4102/udp dgram-alias\n");
    sethostent(1);
    hostent* host = gethostent();
    assert(host && strcmp(host->h_name, "alpha.test") == 0 && host->h_addrtype == AF_INET && host->h_length == 4);
    assert(strcmp(host->h_aliases[0], "alias") == 0 && !host->h_aliases[1]);
    in_addr address{};
    memcpy(&address, host->h_addr, 4);
    assert(ntohl(address.s_addr) == 0xc000020a);
    host = gethostent();
    assert(host && strcmp(host->h_name, "beta.test") == 0 && host->h_addrtype == AF_INET6 && host->h_length == 16);
    assert(gethostent() == nullptr);
    endhostent();
    assert(gethostent() != nullptr);
    sethostent(0);
    assert(gethostent() != nullptr);
    host = gethostbyname("alpha.test");
    assert(host && host->h_addrtype == AF_INET && memcmp(host->h_addr, &address, 4) == 0);
    host = gethostbyname2("beta6", AF_INET6);
    assert(host && host->h_addrtype == AF_INET6 && host->h_length == 16);
    in6_addr address6{};
    assert(inet_pton(AF_INET6, "2001:db8::10", &address6) == 1 && memcmp(host->h_addr, &address6, 16) == 0);
    host = gethostbyaddr(&address, 4, AF_INET);
    assert(host && strcmp(host->h_name, "alpha.test") == 0);
    host = gethostbyaddr(&address6, 16, AF_INET6);
    assert(host && strcmp(host->h_name, "beta.test") == 0);
    assert(gethostbyaddr(&address, 1, AF_INET) == nullptr && h_errno == NO_RECOVERY);
    assert(gethostbyaddr(&address, 4, 999) == nullptr && h_errno == NO_RECOVERY);
    assert(gethostbyname2("alpha.test", AF_INET6) == nullptr && h_errno == HOST_NOT_FOUND);
    assert(gethostbyname2("::1", AF_INET) == nullptr && h_errno == HOST_NOT_FOUND);
    servent* service = getservbyname("pizdos-stream", "tcp");
    assert(service && strcmp(service->s_name, "pizdos-stream") == 0 && ntohs(service->s_port) == 4101);
    service = getservbyname("stream-alias", nullptr);
    assert(service && ntohs(service->s_port) == 4101 && strcmp(service->s_proto, "tcp") == 0);
    service = getservbyport(htons(4102), "udp");
    assert(service && strcmp(service->s_name, "pizdos-dgram") == 0);
    assert(getservbyport(htons(4101), nullptr) != nullptr);
    assert(getservbyname("missing-service", nullptr) == nullptr);
    assert(getservbyport(htons(1), "tcp") == nullptr);
    protoent* protocol = getprotobyname("tcp");
    assert(protocol && protocol->p_proto == IPPROTO_TCP);
    protocol = getprotobynumber(IPPROTO_UDP);
    assert(protocol && strcmp(protocol->p_name, "udp") == 0);
    assert(getprotobyname("missing-protocol") == nullptr && getprotobynumber(99999) == nullptr);
    assert(getnetbyname("missing-network") == nullptr && getnetbyaddr(0, AF_INET) == nullptr);
    assert(hstrerror(HOST_NOT_FOUND)[0] && hstrerror(NO_DATA)[0] && hstrerror(999)[0]);
    assert(gai_strerror(EAI_BADFLAGS)[0] && gai_strerror(EAI_NONAME)[0] && gai_strerror(999)[0]);
    h_errno = HOST_NOT_FOUND;
    herror("netdb");
    h_errno = 99;
    herror(nullptr);

    addrinfo hints{}, *result = nullptr;
    hints.ai_family = AF_INET;
    hints.ai_socktype = SOCK_STREAM;
    hints.ai_flags = AI_CANONNAME;
    assert(getaddrinfo("alpha.test", "pizdos-stream", &hints, &result) == 0 && result);
    assert(result->ai_family == AF_INET && result->ai_protocol == IPPROTO_TCP);
    const auto* socket4 = reinterpret_cast<const sockaddr_in*>(result->ai_addr);
    assert(socket4->sin_addr.s_addr == address.s_addr && ntohs(socket4->sin_port) == 4101);
    assert(strcmp(result->ai_canonname, "alpha.test") == 0);
    freeaddrinfo(result);
    hints.ai_socktype = SOCK_DGRAM;
    assert(getaddrinfo("alpha.test", "dgram-alias", &hints, &result) == 0 && result->ai_protocol == IPPROTO_UDP);
    freeaddrinfo(result);
    assert(getaddrinfo("alpha.test", "missing-service", &hints, &result) == EAI_SERVICE);
    hints.ai_flags = AI_NUMERICSERV;
    assert(getaddrinfo("alpha.test", "pizdos-stream", &hints, &result) == EAI_NONAME);
    hints.ai_flags = AI_NUMERICHOST;
    assert(getaddrinfo("alpha.test", "0", &hints, &result) == EAI_NONAME);
    assert(getaddrinfo(nullptr, nullptr, &hints, &result) == EAI_NONAME);
    hints.ai_flags = 1 << 30;
    assert(getaddrinfo("127.0.0.1", "0", &hints, &result) == EAI_BADFLAGS);
    hints = {};
    hints.ai_family = 999;
    assert(getaddrinfo("127.0.0.1", "0", &hints, &result) == EAI_FAMILY);
    hints.ai_family = AF_INET;
    hints.ai_socktype = 999;
    assert(getaddrinfo("127.0.0.1", "0", &hints, &result) == EAI_SOCKTYPE);
    hints = {};
    hints.ai_family = AF_INET6;
    hints.ai_flags = AI_V4MAPPED | AI_ALL;
    assert(getaddrinfo("192.0.2.10", "0", &hints, &result) == 0);
    assert(IN6_IS_ADDR_V4MAPPED(&reinterpret_cast<sockaddr_in6*>(result->ai_addr)->sin6_addr));
    freeaddrinfo(result);
    hints = {};
    hints.ai_flags = AI_PASSIVE;
    assert(getaddrinfo(nullptr, "0", &hints, &result) == 0);
    for (addrinfo* entry = result; entry; entry = entry->ai_next) {
        if (entry->ai_family == AF_INET) {
            assert(reinterpret_cast<sockaddr_in*>(entry->ai_addr)->sin_addr.s_addr == INADDR_ANY);
        } else {
            assert(entry->ai_family == AF_INET6 && IN6_IS_ADDR_UNSPECIFIED(&reinterpret_cast<sockaddr_in6*>(entry->ai_addr)->sin6_addr));
        }
    }
    freeaddrinfo(result);
    hints.ai_family = AF_INET6;
    hints.ai_flags = 0;
    assert(getaddrinfo(nullptr, "0", &hints, &result) == 0);
    assert(IN6_IS_ADDR_LOOPBACK(&reinterpret_cast<sockaddr_in6*>(result->ai_addr)->sin6_addr));
    freeaddrinfo(result);
    hints.ai_flags = AI_NUMERICHOST;
    assert(getaddrinfo("fe80::1%17", "0", &hints, &result) == 0);
    assert(reinterpret_cast<sockaddr_in6*>(result->ai_addr)->sin6_scope_id == 17);
    freeaddrinfo(result);
    assert(getaddrinfo("fe80::1%pizdos-noif", "0", &hints, &result) == EAI_NONAME);
    hints.ai_family = AF_INET;
    assert(getaddrinfo("::1", "0", &hints, &result) == EAI_NONAME);
    // AI_ADDRCONFIG takes the families the system has an address of: it
    // has no network, so none, as musl's.
    hints.ai_flags = AI_NUMERICHOST | AI_ADDRCONFIG;
    assert(getaddrinfo("127.0.0.1", "0", &hints, &result) == EAI_NONAME);

    char hostname[128], servicename[32];
    sockaddr_in endpoint{AF_INET, htons(4101), address, {}};
    auto* socket = reinterpret_cast<sockaddr*>(&endpoint);
    assert(getnameinfo(socket, sizeof(endpoint), hostname, sizeof(hostname), servicename, sizeof(servicename), 0) == 0);
    assert(strcmp(hostname, "alpha.test") == 0 && strcmp(servicename, "pizdos-stream") == 0);
    assert(getnameinfo(socket, sizeof(endpoint), hostname, sizeof(hostname), nullptr, 0, NI_NOFQDN) == 0 && strcmp(hostname, "alpha") == 0);
    assert(getnameinfo(socket, sizeof(endpoint), hostname, 1, nullptr, 0, NI_NUMERICHOST) == EAI_OVERFLOW);
    assert(getnameinfo(socket, sizeof(endpoint), nullptr, 0, servicename, 1, NI_NUMERICSERV) == EAI_OVERFLOW);
    endpoint.sin_port = htons(4102);
    assert(getnameinfo(socket, sizeof(endpoint), nullptr, 0, servicename, sizeof(servicename), NI_DGRAM) == 0 && strcmp(servicename, "pizdos-dgram") == 0);
    assert(getnameinfo(socket, 1, nullptr, 0, nullptr, 0, 0) == EAI_FAMILY);
    assert(getnameinfo(socket, 15, nullptr, 0, nullptr, 0, 0) == EAI_FAMILY);
    endpoint.sin_family = 999;
    assert(getnameinfo(socket, sizeof(endpoint), nullptr, 0, nullptr, 0, 0) == EAI_FAMILY);
    sockaddr_in6 endpoint6{};
    endpoint6.sin6_family = AF_INET6;
    endpoint6.sin6_port = htons(4102);
    endpoint6.sin6_scope_id = 17;
    assert(inet_pton(AF_INET6, "fe80::1", &endpoint6.sin6_addr) == 1);
    socket = reinterpret_cast<sockaddr*>(&endpoint6);
    assert(getnameinfo(socket, 27, nullptr, 0, nullptr, 0, 0) == EAI_FAMILY);
    assert(getnameinfo(socket, sizeof(endpoint6), hostname, sizeof(hostname), servicename, sizeof(servicename), NI_NUMERICHOST | NI_NUMERICSERV) == 0);
    assert(strcmp(hostname, "fe80::1%17") == 0 && strcmp(servicename, "4102") == 0);

    host = gethostbyname("alias");
    assert(host && strcmp(host->h_name, "alpha.test") == 0);
    service = getservbyname("stream-alias", "tcp");
    assert(service && strcmp(service->s_name, "pizdos-stream") == 0);
    hints = {};
    hints.ai_family = AF_INET;
    hints.ai_socktype = SOCK_STREAM;
    hints.ai_flags = AI_CANONNAME;
    assert(getaddrinfo("alias", "stream-alias", &hints, &result) == 0);
    assert(result->ai_canonname && strcmp(result->ai_canonname, "alpha.test") == 0);
    freeaddrinfo(result);

    fixture("/etc/protocols", "# isolated protocols\nbad\nip 0 IP\ntcp 6 TCP\nudp 17 UDP\n");
    fixture("/etc/networks", "# isolated networks\nbad x\nloopback 127\nlocal-net 10.1 lan\n");
    setservent(1);
    service = getservent();
    assert(service && strcmp(service->s_name, "pizdos-stream") == 0 && strcmp(service->s_proto, "tcp") == 0);
    assert(strcmp(service->s_aliases[0], "stream-alias") == 0 && !service->s_aliases[1]);
    service = getservent();
    assert(service && strcmp(service->s_name, "pizdos-dgram") == 0 && ntohs(service->s_port) == 4102 && strcmp(service->s_proto, "udp") == 0);
    assert(getservent() == nullptr);
    endservent();
    service = getservent();
    assert(service && strcmp(service->s_name, "pizdos-stream") == 0);
    setprotoent(0);
    protocol = getprotoent();
    assert(protocol && strcmp(protocol->p_name, "ip") == 0 && protocol->p_proto == 0 && strcmp(protocol->p_aliases[0], "IP") == 0);
    assert(getprotoent() && getprotoent() && getprotoent() == nullptr);
    endprotoent();
    protocol = getprotobyname("TCP");
    assert(protocol && strcmp(protocol->p_name, "tcp") == 0 && protocol->p_proto == 6);
    setnetent(1);
    netent* network = getnetent();
    assert(network && strcmp(network->n_name, "loopback") == 0 && network->n_net == 127 && network->n_addrtype == AF_INET);
    assert(!network->n_aliases[0]);
    network = getnetent();
    assert(network && strcmp(network->n_name, "local-net") == 0 && network->n_net == 0x0a01 && strcmp(network->n_aliases[0], "lan") == 0);
    assert(getnetent() == nullptr);
    endnetent();
    network = getnetbyname("lan");
    assert(network && strcmp(network->n_name, "local-net") == 0);
    network = getnetbyaddr(127, AF_INET);
    assert(network && strcmp(network->n_name, "loopback") == 0);
    assert(getnetbyaddr(127, AF_INET6) == nullptr);

    alignas(8) char buf[512];
    hostent he{}, *hr = nullptr;
    int herr = 0;
    assert(gethostbyname_r("alias", &he, buf, sizeof buf, &hr, &herr) == 0 && hr == &he);
    assert(strcmp(he.h_name, "alpha.test") == 0 && he.h_length == 4 && memcmp(he.h_addr, &address, 4) == 0 && !he.h_addr_list[1]);
    assert(gethostbyname_r("alias", &he, buf, 8, &hr, &herr) == ERANGE && hr == nullptr);
    assert(gethostbyname2_r("beta6", AF_INET6, &he, buf + 1, sizeof buf - 1, &hr, &herr) == 0 && hr == &he);
    assert(he.h_length == 16 && memcmp(he.h_addr, &address6, 16) == 0 && uintptr_t(he.h_addr_list) % 4 == 0);
    assert(gethostbyname2_r("alpha.test", AF_INET6, &he, buf, sizeof buf, &hr, &herr) == 0 && hr == nullptr && herr == HOST_NOT_FOUND);
    assert(gethostbyname2_r("alpha.test", 999, &he, buf, sizeof buf, &hr, &herr) != 0 && hr == nullptr && herr == NO_RECOVERY);
    assert(gethostbyaddr_r(&address, 4, AF_INET, &he, buf, sizeof buf, &hr, &herr) == 0 && hr == &he);
    assert(strcmp(he.h_name, "alpha.test") == 0);
    assert(gethostbyaddr_r(&address, 1, AF_INET, &he, buf, sizeof buf, &hr, &herr) != 0 && hr == nullptr && herr == NO_RECOVERY);
    servent se{}, *sr = nullptr;
    assert(getservbyname_r("dgram-alias", "udp", &se, buf + 3, sizeof buf - 3, &sr) == 0 && sr == &se);
    assert(strcmp(se.s_name, "pizdos-dgram") == 0 && strcmp(se.s_aliases[0], "dgram-alias") == 0 && uintptr_t(se.s_aliases) % 4 == 0);
    assert(ntohs(se.s_port) == 4102 && strcmp(se.s_proto, "udp") == 0);
    assert(getservbyname_r("dgram-alias", "udp", &se, buf, 4, &sr) == ERANGE && sr == nullptr);
    assert(getservbyname_r("missing-service", nullptr, &se, buf, sizeof buf, &sr) == 0 && sr == nullptr);
    assert(getservbyport_r(htons(4101), "tcp", &se, buf, sizeof buf, &sr) == 0 && sr == &se && strcmp(se.s_name, "pizdos-stream") == 0);
    assert(getservbyport_r(htons(1), "tcp", &se, buf, sizeof buf, &sr) == 0 && sr == nullptr);
    puts("network databases: ok");
}
