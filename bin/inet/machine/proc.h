#ifndef _MACHINE_PROC_H_
#define _MACHINE_PROC_H_

struct mdthread {
    int md_spinlock_count;
    register_t md_saved_flags;
};

struct mdproc {
    void* md_sigtramp;
};

#define KINFO_PROC_SIZE 816

#endif
