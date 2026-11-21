#ifndef _WASM_MACHINE_VMPARAM_H_
#define _WASM_MACHINE_VMPARAM_H_

#include_next <machine/vmparam.h>

struct kva_layout_s {
    vm_offset_t km_low;
    vm_offset_t km_high;
};

#endif
