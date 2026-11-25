/*
 * The network interfaces of net/if.h and ifaddrs.h over the system's
 * cards (interfaces.h). getifaddrs gives each card's link address,
 * AF_PACKET's, with the broadcast one of Ethernet where it has one, then
 * each IPv4 address with its mask and broadcast address, all in one block
 * of the heap, as if_nameindex's list is.
 */
#include "sized.h"

#include "interfaces.h"

#include <arpa/inet.h>
#include <ctype.h>
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <ifaddrs.h>
#include <net/if.h>
#include <net/if_arp.h>
#include <netpacket/packet.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <unistd.h>

// A file's text, at most of the size, NUL-ended in buf: 0, or -1 where it
// cannot be opened.
static int small(const char* path, char* buf, size_t size) {
    int fd = open(path, O_RDONLY | O_CLOEXEC);
    size_t n = 0;

    buf[0] = 0;

    if (fd < 0) {
        return -1;
    }

    for (ssize_t got; n + 1 < size && (got = read(fd, buf + n, size - 1 - n)) != 0;) {
        if (got < 0 && errno != EINTR) {
            break;
        }

        n += got > 0 ? got : 0;
    }

    close(fd);
    buf[n] = 0;

    return 0;
}

static int cardOrder(const void* a, const void* b) {
    const char* x = ((const struct Interface*)a)->name;
    const char* y = ((const struct Interface*)b)->name;

    return strcmp(x, "lo") == 0 ? -(strcmp(y, "lo") != 0) : strcmp(y, "lo") == 0 ? 1 : strcmp(x, y);
}

// An Ethernet address's bytes, as many as its text has before it stops
// being one.
static void ethernet(const char* s, unsigned char mac[6]) {
    for (int i = 0; i < 6; i++) {
        unsigned v = 0;
        int n = 0;

        for (; isxdigit((unsigned char)s[n]); n++) {
            v = v * 16 + (isdigit((unsigned char)s[n]) ? s[n] - '0' : (s[n] | 32) - 'a' + 10);

            if (v > 255) {
                return;
            }
        }

        if (n == 0) {
            return;
        }

        mac[i] = v;
        s += n;

        if (i < 5 && *s++ != ':') {
            return;
        }
    }
}

// A card's IPv4 address and mask of "a.b.c.d/bits".
static int prefix(const char* text, struct in_addr* addr, struct in_addr* mask) {
    char copy[32];
    const char* slash = strchr(text, '/');
    char* end;

    if (!slash || slash - text >= (long)sizeof copy || !isdigit((unsigned char)slash[1])) {
        return 0;
    }

    memcpy(copy, text, slash - text);
    copy[slash - text] = 0;

    long bits = strtol(slash + 1, &end, 10);

    if (*end || bits > 32 || (slash[1] == '0' && slash[2]) || inet_pton(AF_INET, copy, addr) != 1) {
        return 0;
    }

    mask->s_addr = htonl(bits ? ~0u << (32 - bits) : 0);

    return 1;
}

int __interfaces(struct Interface** list) {
    DIR* dir = opendir("/dev/net");
    struct Interface* cards = NULL;
    size_t count = 0, room = 0;

    *list = NULL;

    if (!dir) {
        return errno == ENOMEM ? -1 : 0;
    }

    for (struct dirent* d; (d = readdir(dir));) {
        if (strcmp(d->d_name, ".") == 0 || strcmp(d->d_name, "..") == 0 || strlen(d->d_name) >= sizeof cards->name) {
            continue;
        }

        if (count == room) {
            struct Interface* more = realloc(cards, (room = room ? 2 * room : 8) * sizeof *cards);

            if (!more) {
                free(cards);
                closedir(dir);
                errno = ENOMEM;

                return -1;
            }

            cards = more;
        }

        memset(&cards[count], 0, sizeof *cards);
        strcpy(cards[count++].name, d->d_name);
    }

    closedir(dir);
    qsort(cards, count, sizeof *cards, cardOrder);

    size_t kept = 0;

    for (size_t i = 0; i < count; i++) {
        struct Interface it = cards[i];
        char path[128], ctl[4096], text[4096];

        snprintf(path, sizeof path, "/dev/net/%s/ctl", it.name);

        if (small(path, ctl, sizeof ctl) < 0) {
            continue;
        }

        it.index = kept + 1;
        it.flags = IFF_BROADCAST | IFF_MULTICAST;

        if (strcmp(it.name, "lo") == 0) {
            it.flags = IFF_LOOPBACK;
        } else {
            snprintf(path, sizeof path, "/dev/net/%s/addr", it.name);

            if (small(path, text, 64) == 0) {
                ethernet(text + strspn(text, " \t\n\v\f\r"), it.mac);
            }
        }

        char* word = ctl + strspn(ctl, " \t\n\v\f\r");
        size_t n = strcspn(word, " \t\n\v\f\r");
        char* second = word + n + strspn(word + n, " \t\n\v\f\r");
        size_t m = strcspn(second, " \t\n\v\f\r");

        if (n == 5 && strncmp(word, "stack", 5) == 0 && m > 0 && !(m == 3 && strncmp(second, "off", 3) == 0)) {
            it.flags |= IFF_UP | IFF_RUNNING;
        }

        snprintf(path, sizeof path, "/dev/net/%s/iface/status", it.name);
        small(path, text, sizeof text);

        for (char *line = text, *end; line; line = end ? end + 1 : NULL) {
            struct in_addr a, mask;

            end = strchr(line, '\n');

            if (end) {
                *end = 0;
            }

            if (strncmp(line, "inet ", 5) == 0 && prefix(line + 5, &a, &mask)) {
                it.inet = 1;
                it.addr = a;
                it.mask = mask;
            }

            if (strncmp(line, "dns ", 4) == 0 && inet_aton(line + 4, &a)) {
                it.named = 1;
                it.dns = a;
            }
        }

        cards[kept++] = it;
    }

    *list = cards;

    return kept;
}

unsigned __kernel_if_nametoindex(const char* name) {
    struct Interface* cards;
    int count = __interfaces(&cards);
    unsigned index = 0;

    for (int i = 0; i < count && !index; i++) {
        index = strcmp(cards[i].name, name) == 0 ? cards[i].index : 0;
    }

    free(cards);

    if (!index) {
        errno = ENXIO;
    }

    return index;
}

char* __kernel_if_indextoname(unsigned index, char* buf) {
    struct Interface* cards;
    int count = __interfaces(&cards);
    char* name = NULL;

    for (int i = 0; i < count && !name; i++) {
        name = cards[i].index == index ? strcpy(buf, cards[i].name) : NULL;
    }

    free(cards);

    if (!name) {
        errno = ENXIO;
    }

    return name;
}

struct if_nameindex* __kernel_if_nameindex(size_t size) {
    sized("if_nameindex", size, sizeof(struct if_nameindex));

    struct Interface* cards;
    int count = __interfaces(&cards);
    size_t room = sizeof(struct if_nameindex) * (count + 1);

    for (int i = 0; i < count; i++) {
        room += strlen(cards[i].name) + 1;
    }

    struct if_nameindex* list = count < 0 ? NULL : malloc(room);

    if (!list) {
        free(cards);
        errno = ENOBUFS;

        return NULL;
    }

    char* names = (char*)(list + count + 1);

    for (int i = 0; i < count; i++) {
        list[i] = (struct if_nameindex){cards[i].index, strcpy(names, cards[i].name)};
        names += strlen(names) + 1;
    }

    list[count] = (struct if_nameindex){0, NULL};
    free(cards);

    return list;
}

void __kernel_if_freenameindex(struct if_nameindex* list, size_t size) {
    sized("if_freenameindex", size, sizeof(struct if_nameindex));

    free(list);
}

static struct sockaddr* linkAddress(char** data, const struct Interface* it, const unsigned char mac[6]) {
    struct sockaddr_ll* l = (struct sockaddr_ll*)*data;

    memset(l, 0, sizeof *l);
    l->sll_family = AF_PACKET;
    l->sll_ifindex = it->index;
    l->sll_hatype = it->flags & IFF_LOOPBACK ? ARPHRD_LOOPBACK : ARPHRD_ETHER;
    l->sll_halen = 6;
    memcpy(l->sll_addr, mac, 6);
    *data += sizeof *l;

    return (struct sockaddr*)l;
}

static struct sockaddr* inetAddress(char** data, struct in_addr a) {
    struct sockaddr_in* in = (struct sockaddr_in*)*data;

    memset(in, 0, sizeof *in);
    in->sin_family = AF_INET;
    in->sin_addr = a;
    *data += sizeof *in;

    return (struct sockaddr*)in;
}

int __kernel_getifaddrs(struct ifaddrs** out, size_t size) {
    sized("getifaddrs", size, sizeof(struct ifaddrs));

    struct Interface* cards;
    int count = __interfaces(&cards);
    size_t entries = 0, room = 0;

    if (count < 0) {
        return -1;
    }

    for (int i = 0; i < count; i++) {
        entries += 1 + cards[i].inet;
        room += 2 * sizeof(struct sockaddr_ll) + ((strlen(cards[i].name) + 4) & ~3u) + (cards[i].inet ? 3 * sizeof(struct sockaddr_in) : 0);
    }

    if (!entries) {
        free(cards);
        *out = NULL;

        return 0;
    }

    struct ifaddrs* list = malloc(entries * sizeof *list + room);

    if (!list) {
        free(cards);
        errno = ENOMEM;

        return -1;
    }

    char* data = (char*)(list + entries);
    char* names[count];
    size_t at = 0;

    for (int i = 0; i < count; i++) {
        names[i] = strcpy(data, cards[i].name);
        data += (strlen(cards[i].name) + 4) & ~3u;
    }

    for (int i = 0; i < count; i++) {
        static const unsigned char none[6], all[6] = {0xff, 0xff, 0xff, 0xff, 0xff, 0xff};
        struct sockaddr* addr = linkAddress(&data, &cards[i], cards[i].mac);
        struct sockaddr* broad = linkAddress(&data, &cards[i], cards[i].flags & IFF_BROADCAST ? all : none);

        list[at] = (struct ifaddrs){NULL, names[i], cards[i].flags, addr, NULL, {broad}, NULL};
        at++;
    }

    for (int i = 0; i < count; i++) {
        if (!cards[i].inet) {
            continue;
        }

        struct sockaddr* addr = inetAddress(&data, cards[i].addr);
        struct sockaddr* mask = inetAddress(&data, cards[i].mask);
        struct sockaddr* broad = NULL;

        if (cards[i].flags & IFF_BROADCAST) {
            broad = inetAddress(&data, (struct in_addr){cards[i].addr.s_addr | ~cards[i].mask.s_addr});
        }

        list[at++] = (struct ifaddrs){NULL, names[i], cards[i].flags, addr, mask, {broad}, NULL};
    }

    for (size_t i = 0; i + 1 < entries; i++) {
        list[i].ifa_next = &list[i + 1];
    }

    free(cards);
    *out = list;

    return 0;
}

void __kernel_freeifaddrs(struct ifaddrs* list) {
    free(list);
}
