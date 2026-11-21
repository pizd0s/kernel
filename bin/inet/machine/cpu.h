/* wasm32: no cycle counter but the clock, no traps of a user mode. */
#ifndef MACHINE_CPU_H
#define MACHINE_CPU_H

#include <machine/frame.h>

void cpu_halt(void);
void swi_vm(void*);

#ifdef _KERNEL
static __inline uint64_t get_cyclecount(void) {
    struct bintime bt;
    binuptime(&bt);
    return ((uint64_t)bt.sec << 56 | bt.frac >> 8);
}
#endif

#define TRAPF_USERMODE(frame) 0
#define TRAPF_PC(tfp) ((tfp)->tf_pc)
#define cpu_getstack(td) ((td)->td_frame->tf_sp)
#define cpu_setstack(td, sp) ((td)->td_frame->tf_sp = (sp))
#define cpu_spinwait()
#define cpu_lock_delay() DELAY(1)

#endif
