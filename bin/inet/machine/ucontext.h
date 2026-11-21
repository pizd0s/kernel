#ifndef _MACHINE_UCONTEXT_H_
#define _MACHINE_UCONTEXT_H_

typedef struct __mcontext {
    int mc_onstack;
    int mc_len;
    int mc_flags;
    int mc_spare[16];
} mcontext_t;

#endif
