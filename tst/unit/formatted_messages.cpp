//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "formatted messages: ok\n"
//@ stderr: "XSI:cat: HALT: on the console\nTO FIX: nothing XSI:cat:002\n"

// fmtmsg, as POSIX.1-2024 has it and its examples show: "label: severity:
// text", then "TO FIX: action tag" on a line of its own, the separators
// only between the components given; MSGVERB selects those standard error
// gets, in the message's order whatever its own, all of them where it is
// unset, empty or has a word of no component; the console's message has
// them all. Not on the host: musl leaves a space after an action with no
// tag and runs a tag into the text with no action between.

#include <assert.h>
#include <fcntl.h>
#include <fmtmsg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static char said[1024];

// What fmtmsg writes to standard error, and what it returns.
static int stderrOf(long classification, const char* label, int severity, const char* text, const char* action, const char* tag) {
    int pipes[2];
    assert(pipe(pipes) == 0);
    int saved = dup(2);
    assert(saved >= 0 && dup2(pipes[1], 2) == 2 && close(pipes[1]) == 0);
    int r = fmtmsg(classification, label, severity, text, action, tag);
    assert(dup2(saved, 2) == 2 && close(saved) == 0);
    ssize_t n = read(pipes[0], said, sizeof said - 1);
    assert(n >= 0 && close(pipes[0]) == 0);
    said[n] = 0;
    return r;
}

static void is(const char* want) {
    if (strcmp(said, want) != 0) {
        fprintf(stdout, "fmtmsg wrote \"%s\", not \"%s\"\n", said, want);
        abort();
    }
}

int main() {
    const char* manual = "refer to cat in user's reference manual";
    unsetenv("MSGVERB");
    assert(stderrOf(MM_PRINT, "XSI:cat", MM_ERROR, "illegal option", manual, "XSI:cat:001") == MM_OK);
    is("XSI:cat: ERROR: illegal option\nTO FIX: refer to cat in user's reference manual XSI:cat:001\n");
    setenv("MSGVERB", "severity:text:action", 1);
    assert(stderrOf(MM_PRINT, "XSI:cat", MM_ERROR, "illegal option", manual, "XSI:cat:001") == MM_OK);
    is("ERROR: illegal option\nTO FIX: refer to cat in user's reference manual\n");
    setenv("MSGVERB", "tag:text", 1);
    assert(stderrOf(MM_PRINT, "XSI:cat", MM_ERROR, "illegal option", manual, "XSI:cat:001") == MM_OK);
    is("illegal option\nXSI:cat:001\n");
    setenv("MSGVERB", "text:bogus", 1);
    assert(stderrOf(MM_PRINT, "UX:ls", MM_WARNING, "odd", MM_NULLACT, MM_NULLTAG) == MM_OK);
    is("UX:ls: WARNING: odd\n");
    setenv("MSGVERB", "", 1);
    assert(stderrOf(MM_PRINT, MM_NULLLBL, MM_NOSEV, "just text", MM_NULLACT, MM_NULLTAG) == MM_OK);
    is("just text\n");
    assert(stderrOf(MM_PRINT, MM_NULLLBL, MM_INFO, MM_NULLTXT, "do it", MM_NULLTAG) == MM_OK);
    is("INFO\nTO FIX: do it\n");
    assert(stderrOf(MM_SOFT | MM_APPL | MM_RECOVER, "UX:ls", MM_HALT, "not printed", MM_NULLACT, MM_NULLTAG) == MM_OK);
    is("");

    unsetenv("MSGVERB");
    int closed = dup(2);
    assert(closed >= 0 && close(2) == 0);
    assert(fmtmsg(MM_PRINT, "UX:ls", MM_ERROR, "lost", MM_NULLACT, MM_NULLTAG) == MM_NOMSG);
    assert(dup2(closed, 2) == 2 && close(closed) == 0);
    setenv("MSGVERB", "text", 1);
    assert(fmtmsg(MM_CONSOLE, "XSI:cat", MM_HALT, "on the console", "nothing", "XSI:cat:002") == MM_OK);
    printf("formatted messages: ok\n");
}
