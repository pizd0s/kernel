/* test_fpcast's program: functions called through pointers of other types,
   each result a line. The pointers are volatile, so that the compiler
   knows no call's target. */
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static intptr_t seen;

static void one(void* p) {
    seen = (intptr_t)p;
}

static int three(int a, int b, int c) {
    return a * 100 + b * 10 + c;
}

static void nothing(int a) {
    seen = a;
}

static int seven(void) {
    seen = 7;

    return 7;
}

static long long wide(long long v) {
    return v;
}

static double twice(double d) {
    return d * 2;
}

static float next(float f) {
    return f + 1;
}

static int descending(const void* a, const void* b) {
    return *(const int*)b - *(const int*)a;
}

typedef void (*two)(void*, void*);
typedef int (*int_of_int)(int);
typedef void (*of_nothing)(void);
typedef long long (*wide_of_int)(int);
typedef int (*int_of_wide)(long long);
typedef long long (*wide_of_wide)(long long);

int main(void) {
    // one parameter too many: it is dropped
    volatile two extra = (two)one;
    extra((void*)41, (void*)99);
    printf("extra %ld\n", (long)seen);

    // parameters not passed: they are zero
    volatile int_of_int fewer = (int_of_int)three;
    printf("fewer %d\n", fewer(5));

    // a result of a function that has none: zero
    volatile int_of_int absent = (int_of_int)nothing;
    int result = absent(12);
    printf("absent %d %ld\n", result, (long)seen);

    // a result nobody takes
    volatile of_nothing ignored = (of_nothing)seven;
    seen = 0;
    ignored();
    printf("ignored %ld\n", (long)seen);

    // an int where a long long is taken: its bits, no sign; and back, cut
    volatile wide_of_int widened = (wide_of_int)wide;
    printf("widened %lld\n", widened(-2));
    volatile int_of_wide narrowed = (int_of_wide)wide;
    printf("narrowed %d\n", narrowed(0x100000003LL));

    // a float's bits in an integer's place, and the reverse
    volatile wide_of_wide bits = (wide_of_wide)twice;
    double d = 1.5;
    long long in, out;
    memcpy(&in, &d, sizeof in);
    out = bits(in);
    memcpy(&d, &out, sizeof d);
    printf("double %g\n", d);
    volatile int_of_int single = (int_of_int)next;
    float f = 2.5f;
    int fin, fout;
    memcpy(&fin, &f, sizeof fin);
    fout = single(fin);
    memcpy(&f, &fout, sizeof f);
    printf("float %g\n", f);

    // a call of the function's own type, and the library's of a callback
    volatile int_of_wide own = (int_of_wide)wide;
    volatile wide_of_wide same = wide;
    printf("own %lld %d\n", same(-5), (void*)own == (void*)same);
    int numbers[] = {3, 9, 1, 7};
    qsort(numbers, 4, sizeof numbers[0], descending);
    printf("qsort %d %d %d %d\n", numbers[0], numbers[1], numbers[2], numbers[3]);

    return 0;
}
