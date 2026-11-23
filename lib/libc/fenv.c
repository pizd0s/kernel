#include <fenv.h>

/*
 * The floating-point environment of wasm: no exception flag, which
 * FE_ALL_EXCEPT 0 says, so none is ever raised, cleared or saved, and one
 * rounding mode, to nearest, which only fesetround of another fails at.
 * Exceptions never trap: the non-stop mode feholdexcept installs is the
 * only one. An environment and a flag come with their sizes, as the
 * kernel's structures do; each has had one layout.
 */
int __kernel_feclearexcept(int excepts) {
    (void)excepts;

    return 0;
}

int __kernel_fegetexceptflag(fexcept_t* flagp, size_t size, int excepts) {
    (void)excepts;
    *flagp = 0;

    return 0;
}

int __kernel_feraiseexcept(int excepts) {
    (void)excepts;

    return 0;
}

int __kernel_fesetexceptflag(const fexcept_t* flagp, size_t size, int excepts) {
    (void)flagp;
    (void)excepts;

    return 0;
}

int __kernel_fetestexcept(int excepts) {
    (void)excepts;

    return 0;
}

int __kernel_fegetround(void) {
    return FE_TONEAREST;
}

int __kernel_fesetround(int round) {
    return round == FE_TONEAREST ? 0 : -1;
}

int __kernel_fegetenv(fenv_t* envp, size_t size) {
    envp->__cw = 0;

    return 0;
}

int __kernel_feholdexcept(fenv_t* envp, size_t size) {
    return __kernel_fegetenv(envp, size);
}

int __kernel_fesetenv(const fenv_t* envp, size_t size) {
    (void)envp;

    return 0;
}

int __kernel_feupdateenv(const fenv_t* envp, size_t size) {
    return __kernel_fesetenv(envp, size);
}
