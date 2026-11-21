#ifndef _MACHINE_PCB_H_
#define _MACHINE_PCB_H_

struct pcb {
    int pcb_flags;
    void* pcb_onfault;
};

#endif
