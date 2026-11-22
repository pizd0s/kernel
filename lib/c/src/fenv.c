#include <fenv.h>

/*
 * The floating-point environment of wasm: no exception flag, which
 * FE_ALL_EXCEPT 0 says, so none is ever raised, cleared or saved, and one
 * rounding mode, to nearest, which only fesetround of another fails at.
 * Exceptions never trap: the non-stop mode feholdexcept installs is the
 * only one.
 */
int feclearexcept(int excepts) {
    (void)excepts;

    return 0;
}

int fegetexceptflag(fexcept_t* flagp, int excepts) {
    (void)excepts;
    *flagp = 0;

    return 0;
}

int feraiseexcept(int excepts) {
    (void)excepts;

    return 0;
}

int fesetexceptflag(const fexcept_t* flagp, int excepts) {
    (void)flagp;
    (void)excepts;

    return 0;
}

int fetestexcept(int excepts) {
    (void)excepts;

    return 0;
}

int fegetround(void) {
    return FE_TONEAREST;
}

int fesetround(int round) {
    return round == FE_TONEAREST ? 0 : -1;
}

int fegetenv(fenv_t* envp) {
    envp->__cw = 0;

    return 0;
}

int feholdexcept(fenv_t* envp) {
    return fegetenv(envp);
}

int fesetenv(const fenv_t* envp) {
    (void)envp;

    return 0;
}

int feupdateenv(const fenv_t* envp) {
    return fesetenv(envp);
}
