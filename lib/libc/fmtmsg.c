/*
 * fmtmsg: the message's label, severity and text on one line, joined by
 * ": ", its action ("TO FIX: ") and tag on a second. MM_PRINT writes to
 * standard error the parts MSGVERB names, all of them where it is unset,
 * empty or names another; MM_CONSOLE writes all of them to /dev/console.
 * A severity other than the four is none.
 */
#include <fcntl.h>
#include <fmtmsg.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

enum {
    parts = 5,
};

static const char* const words[parts] = {"label", "severity", "text", "action", "tag"};

static void shown(int use[parts]) {
    const char* verb = getenv("MSGVERB");

    for (int i = 0; i < parts; i++) {
        use[i] = !verb || !*verb;
    }

    if (!verb || !*verb) {
        return;
    }

    for (const char* word = verb;;) {
        size_t n = strcspn(word, ":");
        int found = -1;

        for (int i = 0; i < parts; i++) {
            if (strlen(words[i]) == n && memcmp(words[i], word, n) == 0) {
                found = i;
            }
        }

        if (found < 0) {
            for (int i = 0; i < parts; i++) {
                use[i] = 1;
            }

            return;
        }

        use[found] = 1;

        if (!word[n]) {
            return;
        }

        word += n + 1;
    }
}

static void add(char** at, const char* s) {
    size_t n = strlen(s);

    memcpy(*at, s, n);
    *at += n;
}

// The message of the parts used in text, which is large enough: its two
// lines, of which an empty one is left out, or nothing.
static size_t message(char* text, const char* part[parts], const int use[parts]) {
    char* at = text;
    int count = 0;

    for (int i = 0; i < 3; i++) {
        if (use[i]) {
            add(&at, count++ ? ": " : "");
            add(&at, part[i]);
        }
    }

    size_t line = at - text;
    char* second = at + 1;
    char* end = second;

    count = 0;

    for (int i = 3; i < parts; i++) {
        if (use[i]) {
            add(&end, count++ ? " " : "");
            add(&end, i == 3 ? "TO FIX: " : "");
            add(&end, part[i]);
        }
    }

    size_t rest = end - second;

    if (line && rest) {
        text[line] = '\n';
        at = end;
    } else if (rest) {
        memmove(text, second, rest);
        at = text + rest;
    } else if (!line) {
        return 0;
    }

    *at++ = '\n';

    return at - text;
}

static int written(int fd, const char* text, size_t n) {
    return n == 0 || write(fd, text, n) >= 0;
}

int __kernel_fmtmsg(long classification, const char* label, int severity, const char* text, const char* action, const char* tag) {
    static const char* const severities[] = {NULL, "HALT", "ERROR", "WARNING", "INFO"};
    const char* part[parts] = {label, severity > 0 && severity <= MM_INFO ? severities[severity] : NULL, text, action, tag};
    int given[parts];
    size_t size = 32;

    for (int i = 0; i < parts; i++) {
        given[i] = part[i] != NULL;
        size += part[i] ? strlen(part[i]) : 0;
    }

    char* out = malloc(size);

    if (!out) {
        return MM_NOTOK;
    }

    int result = MM_OK;

    if (classification & MM_PRINT) {
        int verb[parts], use[parts];

        shown(verb);

        for (int i = 0; i < parts; i++) {
            use[i] = given[i] && verb[i];
        }

        if (!written(2, out, message(out, part, use))) {
            result = MM_NOMSG;
        }
    }

    if (classification & MM_CONSOLE) {
        size_t n = message(out, part, given);

        if (n) {
            int console = open("/dev/console", O_WRONLY | O_NOCTTY);
            int ok = console >= 0 && written(console, out, n);

            if (console >= 0) {
                close(console);
            }

            if (!ok) {
                result = result == MM_NOMSG ? MM_NOTOK : MM_NOCON;
            }
        }
    }

    free(out);

    return result;
}
