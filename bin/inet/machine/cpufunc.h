/* wasm32: no privileged instructions, no interrupts to mask. */
#ifndef _MACHINE_CPUFUNC_H_
#define _MACHINE_CPUFUNC_H_

static __inline void breakpoint(void) {
    __builtin_trap();
}

static __inline register_t intr_disable(void) {
    return 0;
}

static __inline void intr_restore(register_t s) {
    (void)s;
}

#endif
