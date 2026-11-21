/* wasm32: one CPU, no segment registers; F-Stack's machine/pcpu.h
 * (lib/include) defines the accessors over its pcpup. */
#ifndef _MACHINE_PCPU_H_
#define _MACHINE_PCPU_H_

#define PCPU_MD_FIELDS     \
    struct pcpu* pc_prvspace; \
    char __pc_md_pad[60]

#endif
