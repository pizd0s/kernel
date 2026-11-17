//@ env: LC_ALL=C
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "message catalogs: ok\n"

#include <assert.h>
#include <errno.h>
#include <libintl.h>
#include <locale.h>
#include <stdio.h>
#include <string.h>

int main() {
    // In the C locale a message is its own translation, a plural one by n.
    const char* message = "one file";
    assert(gettext(message) == message && dgettext("tools", message) == message);
    assert(dcgettext("tools", message, LC_MESSAGES) == message);
    const char* many = "%d files";
    assert(ngettext(message, many, 1) == message && ngettext(message, many, 0) == many);
    assert(ngettext(message, many, 2) == many && dngettext("tools", message, many, 1) == message);
    assert(dcngettext("tools", message, many, 5, LC_MESSAGES) == many);

    // The current domain is "messages" until one is named.
    assert(strcmp(textdomain(nullptr), "messages") == 0);
    assert(strcmp(textdomain("tools"), "tools") == 0 && strcmp(textdomain(nullptr), "tools") == 0);
    char longName[300];
    memset(longName, 'x', sizeof(longName) - 1);
    longName[sizeof(longName) - 1] = 0;
    errno = 0;
    assert(textdomain(longName) == nullptr && errno == EINVAL && strcmp(textdomain(nullptr), "tools") == 0);

    // A domain keeps the directory it was bound to; one not bound has none.
    assert(bindtextdomain("tools", nullptr) == nullptr);
    assert(strcmp(bindtextdomain("tools", "/tmp/locale"), "/tmp/locale") == 0);
    assert(strcmp(bindtextdomain("tools", nullptr), "/tmp/locale") == 0);
    assert(strcmp(bindtextdomain("tools", "/tmp/other"), "/tmp/other") == 0);
    assert(strcmp(bindtextdomain("tools", nullptr), "/tmp/other") == 0);
    assert(bindtextdomain("other", nullptr) == nullptr && bindtextdomain(nullptr, "/tmp") == nullptr);
    errno = 0;
    assert(bindtextdomain(longName, "/tmp") == nullptr && errno == EINVAL);

    // Messages come in UTF-8 and no other codeset.
    errno = 0;
    assert(bind_textdomain_codeset("tools", "UTF-8") == nullptr && errno == 0);
    assert(bind_textdomain_codeset("tools", nullptr) == nullptr && errno == 0);
    assert(bind_textdomain_codeset("tools", "KOI8-R") == nullptr && errno == EINVAL);

    assert(gettext(message) == message);
    puts("message catalogs: ok");
}
