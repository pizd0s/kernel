/*
 * An assertion that fails: its message, of the expression, the file, the
 * function and the line, in one write to the standard error, and abort.
 */
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>

_Noreturn void __kernel___assert_fail(const char* expression, const char* file, int line, const char* function) {
    char text[1024];
    int n = snprintf(text, sizeof text, "Assertion failed: %s (%s: %s: %d)\n", expression, file, function, line);

    write(STDERR_FILENO, text, n < (int)sizeof text ? n : (int)sizeof text - 1);
    abort();
}
