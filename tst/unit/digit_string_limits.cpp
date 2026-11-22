//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE
//@ stdout: "digit string limits: ok\n"

// ecvt and fcvt at their limits, SUSv3's (legacy): ndigit 0 is no
// digits; a request past a double's precision is cut to it, 15 digits,
// fcvt's of a huge number too; fcvt's negative ndigit is 0 places, and a
// rounding that adds a digit before the radix keeps ndigit after it; an
// infinity or a NaN, whose string SUSv3 leaves unspecified, is its name.
// host: false: the host musl's ecvt makes 15 digits of ndigit 0 and of a
// negative one and reads past its buffer for an infinity or a NaN; its
// fcvt gives 9.96 at 1 place as 996 with the radix after 9, and overflows
// its buffer for a huge number with many places.

#include <assert.h>
#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main() {
    int decpt;
    int sign;
    assert(strcmp(ecvt(3.75, 0, &decpt, &sign), "") == 0 && sign == 0);
    assert(strcmp(ecvt(-3.75, -2, &decpt, &sign), "") == 0 && sign == 1);
    assert(strlen(ecvt(1.0 / 3, 99, &decpt, &sign)) == 15 && decpt == 0);
    assert(strcmp(ecvt(INFINITY, 4, &decpt, &sign), "inf") == 0 && sign == 0);
    assert(strcmp(ecvt(-INFINITY, 4, &decpt, &sign), "inf") == 0 && sign == 1);
    assert(strcmp(ecvt(NAN, 4, &decpt, &sign), "nan") == 0);
    assert(strcmp(fcvt(INFINITY, 2, &decpt, &sign), "inf") == 0);

    char* huge = fcvt(1e300, 1400, &decpt, &sign);
    assert(strlen(huge) == 15 && strncmp(huge, "100000000000000", 15) == 0 && decpt == 301);
    assert(strcmp(fcvt(1234.5678, -2, &decpt, &sign), "1235") == 0 && decpt == 4);
    assert(strcmp(fcvt(-9.96, 1, &decpt, &sign), "100") == 0 && decpt == 2 && sign == 1);
    assert(strcmp(fcvt(99.96, 1, &decpt, &sign), "1000") == 0 && decpt == 3);
    char* tiny = fcvt(4.9e-324, 400, &decpt, &sign);
    assert(strncmp(tiny, "4940656", 7) == 0 && decpt == -323);

    printf("digit string limits: ok\n");
}
