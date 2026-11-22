//@ host: false
//@ compile-flags: -g -D_GNU_SOURCE -fno-builtin
//@ stdout: "complex numbers: ok\n"

// complex.h's functions as C11's Annex G specifies them (G.6), in double
// and float, at each special value it lists and its reflections by the
// functions' symmetries; the infinity properties of products and
// quotients (G.5.1), lib/c's __muldc3 and __divdc3 and their float ones;
// accuracy where a plain formula loses it, clog near |z| = 1; and each
// function undone by its inverse. long double is double here, so its
// functions give double's results exactly. ctanh(+0 + i∞) and
// ctanh(+0 + iNaN), which C17 changed (DR 471), are left out. Raised
// exceptions are not checked: wasm has no floating-point flags. host:
// false: the host musl's inverse functions and clog miss Annex G
// (casin(0 + i0) is 0 - i0 there, catanh(+inf + i0) NaN - iπ/2), and its
// reference is linked without the compiler's runtime, which products and
// quotients call. In C++ complex.h is <complex>, so the case declares C's
// functions itself.

#include <assert.h>
#include <math.h>
#include <stdio.h>

extern "C" {
#define UNARY(name) \
    double _Complex name(double _Complex); \
    float _Complex name##f(float _Complex); \
    long double _Complex name##l(long double _Complex);

UNARY(cacos)
UNARY(casin)
UNARY(catan)
UNARY(ccos)
UNARY(csin)
UNARY(ctan)
UNARY(cacosh)
UNARY(casinh)
UNARY(catanh)
UNARY(ccosh)
UNARY(csinh)
UNARY(ctanh)
UNARY(cexp)
UNARY(clog)
UNARY(csqrt)
UNARY(conj)
UNARY(cproj)

double cabs(double _Complex);
float cabsf(float _Complex);
double carg(double _Complex);
float cargf(float _Complex);
double creal(double _Complex);
double cimag(double _Complex);
double _Complex cpow(double _Complex, double _Complex);
float _Complex cpowf(float _Complex, float _Complex);
long double _Complex cpowl(long double _Complex, long double _Complex);
}

namespace {
    const double inf = INFINITY;
    const double NaN = NAN;
    const double pi = M_PI;
    int failures;

    struct Function {
        const char* name;
        double _Complex (*d)(double _Complex);
        float _Complex (*f)(float _Complex);
        long double _Complex (*l)(long double _Complex);
        // f(conj z) = conj f(z); f(-z) = -f(z) (odd), f(z) (even) or neither.
        int parity;
    };

    enum { NEITHER, ODD, EVEN };

    // What a part should be: a NaN any NaN; a number with its sign, or with
    // either where Annex G leaves it unspecified; a number not 0 nor
    // infinite within 2 units in its last place.
    struct Part {
        double value;
        bool anySign;
    };

    Part v(double value) {
        return {value, false};
    }

    Part either(double value) {
        return {value, true};
    }

    double _Complex of(double x, double y) {
        double _Complex z;
        __real__ z = x;
        __imag__ z = y;
        return z;
    }

    float _Complex floats(double x, double y) {
        float _Complex z;
        __real__ z = (float)x;
        __imag__ z = (float)y;
        return z;
    }

    long double _Complex wide(double x, double y) {
        long double _Complex z;
        __real__ z = x;
        __imag__ z = y;
        return z;
    }

    template <class T>
    bool near(T got, T want, int units) {
        if (isnan(want)) {
            return isnan(got);
        }
        if (want == 0 || isinf(want)) {
            return got == want && signbit(got) == signbit(want);
        }
        T unit = nextafter(fabs(want), (T)INFINITY) - fabs(want);
        return signbit(got) == signbit(want) && fabs(got - want) <= units * unit;
    }

    template <class T>
    bool matches(T got, Part want) {
        T value = (T)want.value;
        if (want.anySign && !isnan(value)) {
            return near((T)fabs(got), (T)fabs(value), 2);
        }
        return near(got, value, 2);
    }

    void report(const char* name, char kind, double x, double y, double re, double im, Part a, Part b) {
        printf("%s%c(%g%+gi) = %.17g%+.17gi, Annex G: %s%g%s%gi\n", name, kind, x, y, re, im, a.anySign ? "±" : "",
               a.value, b.anySign ? " ± " : " + ", b.value);
        failures++;
    }

    bool same(long double got, double want) {
        return isnan(want) ? isnan(got) : got == want && signbit(got) == signbit(want);
    }

    void one(const Function& f, double x, double y, Part re, Part im) {
        double _Complex d = f.d(of(x, y));
        if (!matches(__real__ d, re) || !matches(__imag__ d, im)) {
            report(f.name, ' ', x, y, __real__ d, __imag__ d, re, im);
        }
        float _Complex g = f.f(floats(x, y));
        if (!matches(__real__ g, re) || !matches(__imag__ g, im)) {
            report(f.name, 'f', x, y, __real__ g, __imag__ g, re, im);
        }
        long double _Complex l = f.l(wide(x, y));
        if (!same(__real__ l, __real__ d) || !same(__imag__ l, __imag__ d)) {
            report(f.name, 'l', x, y, (double)__real__ l, (double)__imag__ l, re, im);
        }
    }

    Part negated(Part p) {
        return {-p.value, p.anySign};
    }

    // An entry of Annex G and what the function's symmetries make of it.
    void entry(const Function& f, double x, double y, Part re, Part im) {
        one(f, x, y, re, im);
        one(f, x, -y, re, negated(im));
        if (f.parity == ODD) {
            one(f, -x, -y, negated(re), negated(im));
            one(f, -x, y, negated(re), im);
        } else if (f.parity == EVEN) {
            one(f, -x, -y, re, im);
            one(f, -x, y, re, negated(im));
        }
    }

    bool infinite(double _Complex z) {
        return isinf(__real__ z) || isinf(__imag__ z);
    }

    bool infinitef(float _Complex z) {
        return isinf(__real__ z) || isinf(__imag__ z);
    }

    bool zero(double _Complex z) {
        return __real__ z == 0 && __imag__ z == 0;
    }

    bool zerof(float _Complex z) {
        return __real__ z == 0 && __imag__ z == 0;
    }

    // z undone by its inverse, within units of the larger part's precision.
    bool undone(double _Complex z, double _Complex back, double units) {
        double scale = fmax(fabs(__real__ z), fabs(__imag__ z));
        return fabs(__real__ back - __real__ z) <= units * scale * 0x1p-52 &&
               fabs(__imag__ back - __imag__ z) <= units * scale * 0x1p-52;
    }
}

int main() {
    const Function cacosF = {"cacos", cacos, cacosf, cacosl, NEITHER};
    const Function cacoshF = {"cacosh", cacosh, cacoshf, cacoshl, NEITHER};
    const Function casinhF = {"casinh", casinh, casinhf, casinhl, ODD};
    const Function catanhF = {"catanh", catanh, catanhf, catanhl, ODD};
    const Function ccoshF = {"ccosh", ccosh, ccoshf, ccoshl, EVEN};
    const Function csinhF = {"csinh", csinh, csinhf, csinhl, ODD};
    const Function ctanhF = {"ctanh", ctanh, ctanhf, ctanhl, ODD};
    const Function cexpF = {"cexp", cexp, cexpf, cexpl, NEITHER};
    const Function clogF = {"clog", clog, clogf, clogl, NEITHER};
    const Function csqrtF = {"csqrt", csqrt, csqrtf, csqrtl, NEITHER};
    const Function casinF = {"casin", casin, casinf, casinl, ODD};
    const Function catanF = {"catan", catan, catanf, catanl, ODD};
    const Function ccosF = {"ccos", ccos, ccosf, ccosl, EVEN};
    const Function csinF = {"csin", csin, csinf, csinl, ODD};
    const Function ctanF = {"ctan", ctan, ctanf, ctanl, ODD};
    const Function cprojF = {"cproj", cproj, cprojf, cprojl, NEITHER};

    // G.6.1.1
    entry(cacosF, 0, 0, v(pi / 2), v(-0.0));
    entry(cacosF, -0.0, 0, v(pi / 2), v(-0.0));
    entry(cacosF, 0, NaN, v(pi / 2), v(NaN));
    entry(cacosF, -0.0, NaN, v(pi / 2), v(NaN));
    entry(cacosF, 1.5, inf, v(pi / 2), v(-inf));
    entry(cacosF, NaN, inf, v(NaN), v(-inf));
    entry(cacosF, 1.5, NaN, v(NaN), v(NaN));
    entry(cacosF, -inf, 1.5, v(pi), v(-inf));
    entry(cacosF, -inf, 0, v(pi), v(-inf));
    entry(cacosF, inf, 1.5, v(0), v(-inf));
    entry(cacosF, inf, 0, v(0), v(-inf));
    entry(cacosF, -inf, inf, v(3 * pi / 4), v(-inf));
    entry(cacosF, inf, inf, v(pi / 4), v(-inf));
    entry(cacosF, inf, NaN, v(NaN), either(inf));
    entry(cacosF, -inf, NaN, v(NaN), either(inf));
    entry(cacosF, NaN, 1.5, v(NaN), v(NaN));
    entry(cacosF, NaN, NaN, v(NaN), v(NaN));
    // G.6.2.1
    entry(cacoshF, 0, 0, v(0), v(pi / 2));
    entry(cacoshF, -0.0, 0, v(0), v(pi / 2));
    entry(cacoshF, 1.5, inf, v(inf), v(pi / 2));
    entry(cacoshF, 1.5, NaN, v(NaN), v(NaN));
    entry(cacoshF, -inf, 1.5, v(inf), v(pi));
    entry(cacoshF, inf, 1.5, v(inf), v(0));
    entry(cacoshF, -inf, inf, v(inf), v(3 * pi / 4));
    entry(cacoshF, inf, inf, v(inf), v(pi / 4));
    entry(cacoshF, inf, NaN, v(inf), v(NaN));
    entry(cacoshF, -inf, NaN, v(inf), v(NaN));
    entry(cacoshF, NaN, 1.5, v(NaN), v(NaN));
    entry(cacoshF, NaN, inf, v(inf), v(NaN));
    entry(cacoshF, NaN, NaN, v(NaN), v(NaN));
    // G.6.2.2
    entry(casinhF, 0, 0, v(0), v(0));
    entry(casinhF, 1.5, inf, v(inf), v(pi / 2));
    entry(casinhF, 0, inf, v(inf), v(pi / 2));
    entry(casinhF, 1.5, NaN, v(NaN), v(NaN));
    entry(casinhF, inf, 1.5, v(inf), v(0));
    entry(casinhF, inf, inf, v(inf), v(pi / 4));
    entry(casinhF, inf, NaN, v(inf), v(NaN));
    entry(casinhF, NaN, 0, v(NaN), v(0));
    entry(casinhF, NaN, 1.5, v(NaN), v(NaN));
    entry(casinhF, NaN, inf, either(inf), v(NaN));
    entry(casinhF, NaN, NaN, v(NaN), v(NaN));
    // G.6.2.3
    entry(catanhF, 0, 0, v(0), v(0));
    entry(catanhF, 0, NaN, v(0), v(NaN));
    entry(catanhF, 1, 0, v(inf), v(0));
    entry(catanhF, 1.5, inf, v(0), v(pi / 2));
    entry(catanhF, 0, inf, v(0), v(pi / 2));
    entry(catanhF, 1.5, NaN, v(NaN), v(NaN));
    entry(catanhF, inf, 1.5, v(0), v(pi / 2));
    entry(catanhF, inf, 0, v(0), v(pi / 2));
    entry(catanhF, inf, inf, v(0), v(pi / 2));
    entry(catanhF, inf, NaN, v(0), v(NaN));
    entry(catanhF, NaN, 1.5, v(NaN), v(NaN));
    entry(catanhF, NaN, inf, either(0), v(pi / 2));
    entry(catanhF, NaN, NaN, v(NaN), v(NaN));
    // G.6.2.4
    entry(ccoshF, 0, 0, v(1), v(0));
    entry(ccoshF, 0, inf, v(NaN), either(0));
    entry(ccoshF, 0, NaN, v(NaN), either(0));
    entry(ccoshF, 1.5, inf, v(NaN), v(NaN));
    entry(ccoshF, 1.5, NaN, v(NaN), v(NaN));
    entry(ccoshF, inf, 0, v(inf), v(0));
    entry(ccoshF, inf, 2, v(inf * cos(2.0)), v(inf * sin(2.0)));
    entry(ccoshF, inf, 4, v(inf * cos(4.0)), v(inf * sin(4.0)));
    entry(ccoshF, inf, inf, either(inf), v(NaN));
    entry(ccoshF, inf, NaN, v(inf), v(NaN));
    entry(ccoshF, NaN, 0, v(NaN), either(0));
    entry(ccoshF, NaN, 1.5, v(NaN), v(NaN));
    entry(ccoshF, NaN, NaN, v(NaN), v(NaN));
    // G.6.2.5
    entry(csinhF, 0, 0, v(0), v(0));
    entry(csinhF, 0, inf, either(0), v(NaN));
    entry(csinhF, 0, NaN, either(0), v(NaN));
    entry(csinhF, 1.5, inf, v(NaN), v(NaN));
    entry(csinhF, 1.5, NaN, v(NaN), v(NaN));
    entry(csinhF, inf, 0, v(inf), v(0));
    entry(csinhF, inf, 2, v(inf * cos(2.0)), v(inf * sin(2.0)));
    entry(csinhF, inf, 4, v(inf * cos(4.0)), v(inf * sin(4.0)));
    entry(csinhF, inf, inf, either(inf), v(NaN));
    entry(csinhF, inf, NaN, either(inf), v(NaN));
    entry(csinhF, NaN, 0, v(NaN), v(0));
    entry(csinhF, NaN, 1.5, v(NaN), v(NaN));
    entry(csinhF, NaN, NaN, v(NaN), v(NaN));
    // G.6.2.6
    entry(ctanhF, 0, 0, v(0), v(0));
    entry(ctanhF, 1.5, inf, v(NaN), v(NaN));
    entry(ctanhF, 1.5, NaN, v(NaN), v(NaN));
    entry(ctanhF, inf, 1, v(1), v(0));
    entry(ctanhF, inf, 2, v(1), v(-0.0));
    entry(ctanhF, inf, 0, v(1), v(0));
    entry(ctanhF, inf, inf, v(1), either(0));
    entry(ctanhF, inf, NaN, v(1), either(0));
    entry(ctanhF, NaN, 0, v(NaN), v(0));
    entry(ctanhF, NaN, 1.5, v(NaN), v(NaN));
    entry(ctanhF, NaN, NaN, v(NaN), v(NaN));
    // G.6.3.1
    entry(cexpF, 0, 0, v(1), v(0));
    entry(cexpF, -0.0, 0, v(1), v(0));
    entry(cexpF, 1.5, inf, v(NaN), v(NaN));
    entry(cexpF, 1.5, NaN, v(NaN), v(NaN));
    entry(cexpF, inf, 0, v(inf), v(0));
    entry(cexpF, -inf, 2, v(0 * cos(2.0)), v(0 * sin(2.0)));
    entry(cexpF, -inf, 0, v(0), v(0));
    entry(cexpF, inf, 2, v(inf * cos(2.0)), v(inf * sin(2.0)));
    entry(cexpF, -inf, inf, either(0), either(0));
    entry(cexpF, inf, inf, either(inf), v(NaN));
    entry(cexpF, -inf, NaN, either(0), either(0));
    entry(cexpF, inf, NaN, either(inf), v(NaN));
    entry(cexpF, NaN, 0, v(NaN), v(0));
    entry(cexpF, NaN, 1.5, v(NaN), v(NaN));
    entry(cexpF, NaN, NaN, v(NaN), v(NaN));
    // G.6.3.2
    entry(clogF, -0.0, 0, v(-inf), v(pi));
    entry(clogF, 0, 0, v(-inf), v(0));
    entry(clogF, 1.5, inf, v(inf), v(pi / 2));
    entry(clogF, 1.5, NaN, v(NaN), v(NaN));
    entry(clogF, -inf, 1.5, v(inf), v(pi));
    entry(clogF, inf, 1.5, v(inf), v(0));
    entry(clogF, -inf, inf, v(inf), v(3 * pi / 4));
    entry(clogF, inf, inf, v(inf), v(pi / 4));
    entry(clogF, inf, NaN, v(inf), v(NaN));
    entry(clogF, -inf, NaN, v(inf), v(NaN));
    entry(clogF, NaN, 1.5, v(NaN), v(NaN));
    entry(clogF, NaN, inf, v(inf), v(NaN));
    entry(clogF, NaN, NaN, v(NaN), v(NaN));
    // G.6.4.2
    entry(csqrtF, 0, 0, v(0), v(0));
    entry(csqrtF, -0.0, 0, v(0), v(0));
    entry(csqrtF, 1.5, inf, v(inf), v(inf));
    entry(csqrtF, -inf, inf, v(inf), v(inf));
    entry(csqrtF, NaN, inf, v(inf), v(inf));
    entry(csqrtF, 1.5, NaN, v(NaN), v(NaN));
    entry(csqrtF, -inf, 1.5, v(0), v(inf));
    entry(csqrtF, inf, 1.5, v(inf), v(0));
    entry(csqrtF, -inf, NaN, v(NaN), either(inf));
    entry(csqrtF, inf, NaN, v(inf), v(NaN));
    entry(csqrtF, NaN, 1.5, v(NaN), v(NaN));
    entry(csqrtF, NaN, NaN, v(NaN), v(NaN));
    // G.6, 7: casin(z) = -i casinh(iz) and its kind, at a few values.
    entry(casinF, 0, 0, v(0), v(0));
    entry(casinF, inf, 1.5, v(pi / 2), v(inf));
    entry(casinF, 1.5, inf, v(0), v(inf));
    entry(catanF, 0, 0, v(0), v(0));
    entry(catanF, 0, 1, v(0), v(inf));
    entry(catanF, inf, 1.5, v(pi / 2), v(0));
    entry(ccosF, 0, 0, v(1), v(-0.0));
    entry(ccosF, 0, inf, v(inf), v(-0.0));
    entry(csinF, 0, 0, v(0), v(0));
    entry(csinF, 0, inf, v(0), v(inf));
    entry(ctanF, 0, 0, v(0), v(0));
    entry(ctanF, 1.5, inf, v(0), v(1));
    // 7.3.9.5: an infinity projects to +inf on the real axis.
    entry(cprojF, inf, NaN, v(inf), v(0));
    entry(cprojF, NaN, -inf, v(inf), v(-0.0));
    entry(cprojF, 3, -4, v(3), v(-4));
    // G.6, 6: cabs is hypot, carg atan2.
    assert(cabs(of(3, -4)) == 5 && cabs(of(NaN, -inf)) == inf && cabsf(floats(inf, NaN)) == INFINITY);
    assert(carg(of(-1, 0)) == pi && carg(of(-1, -0.0)) == -pi && carg(of(-0.0, -0.0)) == -pi);
    assert(cargf(floats(0, -0.0)) == 0 && signbit(cargf(floats(0, -0.0))));
    assert(creal(of(2, 3)) == 2 && cimag(of(2, 3)) == 3 && __imag__ conj(of(1, 0)) == 0 && signbit(__imag__ conj(of(1, 0))));

    // G.5.1, 4: an infinity times a nonzero number or an infinity is an
    // infinity; an infinity over a number is one, a number over an
    // infinity a zero, a nonzero number or an infinity over a zero an
    // infinity; the plain formula where it gives numbers.
    volatile double values[] = {inf, NaN, 2, 0, -inf, 1, 3, 4};
    double _Complex big = of(values[0], values[1]);
    double _Complex two = of(values[2], values[3]);
    double _Complex nanInf = of(values[1], values[4]);
    double _Complex one = of(values[5], values[5]);
    double _Complex zeroes = of(values[3], values[3]);
    assert(infinite(big * two) && infinite(two * big) && infinite(nanInf * one) && infinite(big * big));
    assert(infinite(big / one) && zero(one / big) && zero(one / nanInf) && infinite(one / zeroes) && infinite(big / zeroes));
    double _Complex product = of(values[5], values[2]) * of(values[6], values[7]);
    double _Complex quotient = of(values[6], values[7]) / of(values[5], -values[2]);
    assert(__real__ product == -5 && __imag__ product == 10 && __real__ quotient == -1 && __imag__ quotient == 2);
    float _Complex bigf = floats(values[0], values[1]);
    float _Complex onef = floats(values[5], values[5]);
    assert(infinitef(bigf * floats(values[2], values[3])) && infinitef(bigf / onef) && zerof(onef / bigf));
    assert(infinitef(onef / floats(values[3], values[3])));
    float _Complex productf = floats(values[5], values[2]) * floats(values[6], values[7]);
    float _Complex quotientf = floats(values[6], values[7]) / floats(values[5], -values[2]);
    assert(__real__ productf == -5 && __imag__ productf == 10 && __real__ quotientf == -1 && __imag__ quotientf == 2);

    // Where a plain formula loses all digits: clog's real part near
    // |z| = 1, 0.5 log1p(x² + y² - 1), and the inverses at tiny and huge
    // arguments.
    assert(near(__real__ clog(of(1, 1e-10)), 0x1.79ca10c924224p-68, 4) && __imag__ clog(of(1, 1e-10)) == 1e-10);
    assert(near(__real__ clog(of(0.6, 0.8)), 0x1.999999999999ap-56, 4));
    assert(near(__real__ casin(of(1e-200, 1e-200)), 1e-200, 2) && near(__imag__ casin(of(1e-200, 1e-200)), 1e-200, 2));
    assert(near(__imag__ cacos(of(1e300, 1e300)), -(log(1e300) + 1.5 * log(2.0)), 8));
    assert(near(__real__ catanh(of(1e-300, 0)), 1e-300, 2) && near(__imag__ catanh(of(1e300, 1)), pi / 2, 2));
    assert(near(__real__ casinh(of(1, 0)), asinh(1.0), 2) && near(__real__ cacos(of(0.5, 0)), acos(0.5), 2));

    // Each function undone by its inverse, and cpow as cexp(c clog(z)).
    const double plain[][2] = {{0.5, 0.25}, {-2, 3}, {3, -4}, {0.75, -0.1}, {-0.3, -0.9}};
    for (const auto& p : plain) {
        double _Complex z = of(p[0], p[1]);
        assert(undone(z, csin(casin(z)), 64) && undone(z, ccos(cacos(z)), 64) && undone(z, ctan(catan(z)), 64));
        assert(undone(z, csinh(casinh(z)), 64) && undone(z, ccosh(cacosh(z)), 64) && undone(z, ctanh(catanh(z)), 64));
        assert(undone(z, cexp(clog(z)), 64) && undone(z, csqrt(z) * csqrt(z), 16));
        assert(undone(z, cpow(z, of(1, 0)), 16) && undone(z * z, cpow(z, of(2, 0)), 64));
        long double _Complex w = cpowl(wide(p[0], p[1]), wide(2, 0));
        double _Complex d = cpow(z, of(2, 0));
        assert(__real__ w == __real__ d && __imag__ w == __imag__ d);
    }
    assert(near(__real__ cpowf(floats(2, 0), floats(0.5, 0)), (float)M_SQRT2, 2));

    if (failures) {
        printf("complex numbers: %d off Annex G\n", failures);
        return 1;
    }
    printf("complex numbers: ok\n");
}
