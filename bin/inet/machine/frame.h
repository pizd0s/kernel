/* wasm32: the stack takes no traps; a frame only names a pc and a sp. */
#ifndef _MACHINE_FRAME_H_
#define _MACHINE_FRAME_H_

struct trapframe {
    register_t tf_pc;
    register_t tf_sp;
};

#endif
