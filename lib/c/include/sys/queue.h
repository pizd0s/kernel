#pragma once

// BSD's lists of the elements' own links, as macros: singly linked lists
// (SLIST), singly linked tail queues (STAILQ), doubly linked lists (LIST)
// and doubly linked tail queues (TAILQ), as BSD's and glibc's
// <sys/queue.h> have them.

#define SLIST_HEAD(name, type) \
    struct name {              \
        struct type* slh_first; \
    }
#define SLIST_HEAD_INITIALIZER(head) {0}
#define SLIST_ENTRY(type)      \
    struct {                   \
        struct type* sle_next; \
    }
#define SLIST_FIRST(head) ((head)->slh_first)
#define SLIST_EMPTY(head) ((head)->slh_first == 0)
#define SLIST_NEXT(elm, field) ((elm)->field.sle_next)
#define SLIST_INIT(head) ((head)->slh_first = 0)
#define SLIST_FOREACH(var, head, field) for ((var) = SLIST_FIRST(head); (var); (var) = SLIST_NEXT(var, field))
#define SLIST_FOREACH_SAFE(var, head, field, tvar) \
    for ((var) = SLIST_FIRST(head); (var) && ((tvar) = SLIST_NEXT(var, field), 1); (var) = (tvar))
#define SLIST_INSERT_HEAD(head, elm, field)              \
    do {                                                 \
        (elm)->field.sle_next = (head)->slh_first;       \
        (head)->slh_first = (elm);                       \
    } while (0)
#define SLIST_INSERT_AFTER(slistelm, elm, field)               \
    do {                                                       \
        (elm)->field.sle_next = (slistelm)->field.sle_next;    \
        (slistelm)->field.sle_next = (elm);                    \
    } while (0)
#define SLIST_REMOVE_HEAD(head, field)                                \
    do {                                                              \
        (head)->slh_first = (head)->slh_first->field.sle_next;        \
    } while (0)
#define SLIST_REMOVE_AFTER(elm, field)                                        \
    do {                                                                      \
        (elm)->field.sle_next = (elm)->field.sle_next->field.sle_next;        \
    } while (0)
#define SLIST_REMOVE(head, elm, type, field)                   \
    do {                                                       \
        if ((head)->slh_first == (elm)) {                      \
            SLIST_REMOVE_HEAD(head, field);                    \
        } else {                                               \
            struct type* _cur = (head)->slh_first;             \
            while (_cur->field.sle_next != (elm))              \
                _cur = _cur->field.sle_next;                   \
            SLIST_REMOVE_AFTER(_cur, field);                   \
        }                                                      \
    } while (0)

#define STAILQ_HEAD(name, type)  \
    struct name {                \
        struct type* stqh_first; \
        struct type** stqh_last; \
    }
#define STAILQ_HEAD_INITIALIZER(head) {0, &(head).stqh_first}
#define STAILQ_ENTRY(type)      \
    struct {                    \
        struct type* stqe_next; \
    }
#define STAILQ_FIRST(head) ((head)->stqh_first)
#define STAILQ_EMPTY(head) ((head)->stqh_first == 0)
#define STAILQ_NEXT(elm, field) ((elm)->field.stqe_next)
#define STAILQ_INIT(head)                              \
    do {                                               \
        (head)->stqh_first = 0;                        \
        (head)->stqh_last = &(head)->stqh_first;       \
    } while (0)
#define STAILQ_FOREACH(var, head, field) for ((var) = STAILQ_FIRST(head); (var); (var) = STAILQ_NEXT(var, field))
#define STAILQ_FOREACH_SAFE(var, head, field, tvar) \
    for ((var) = STAILQ_FIRST(head); (var) && ((tvar) = STAILQ_NEXT(var, field), 1); (var) = (tvar))
#define STAILQ_INSERT_HEAD(head, elm, field)                                \
    do {                                                                    \
        if (((elm)->field.stqe_next = (head)->stqh_first) == 0)             \
            (head)->stqh_last = &(elm)->field.stqe_next;                    \
        (head)->stqh_first = (elm);                                         \
    } while (0)
#define STAILQ_INSERT_TAIL(head, elm, field)                   \
    do {                                                       \
        (elm)->field.stqe_next = 0;                            \
        *(head)->stqh_last = (elm);                            \
        (head)->stqh_last = &(elm)->field.stqe_next;           \
    } while (0)
#define STAILQ_REMOVE_HEAD(head, field)                                                   \
    do {                                                                                  \
        if (((head)->stqh_first = (head)->stqh_first->field.stqe_next) == 0)              \
            (head)->stqh_last = &(head)->stqh_first;                                      \
    } while (0)

#define LIST_HEAD(name, type)   \
    struct name {               \
        struct type* lh_first;  \
    }
#define LIST_HEAD_INITIALIZER(head) {0}
#define LIST_ENTRY(type)         \
    struct {                     \
        struct type* le_next;    \
        struct type** le_prev;   \
    }
#define LIST_FIRST(head) ((head)->lh_first)
#define LIST_EMPTY(head) ((head)->lh_first == 0)
#define LIST_NEXT(elm, field) ((elm)->field.le_next)
#define LIST_INIT(head) ((head)->lh_first = 0)
#define LIST_FOREACH(var, head, field) for ((var) = LIST_FIRST(head); (var); (var) = LIST_NEXT(var, field))
#define LIST_FOREACH_SAFE(var, head, field, tvar) \
    for ((var) = LIST_FIRST(head); (var) && ((tvar) = LIST_NEXT(var, field), 1); (var) = (tvar))
#define LIST_INSERT_HEAD(head, elm, field)                                    \
    do {                                                                      \
        if (((elm)->field.le_next = (head)->lh_first) != 0)                   \
            (head)->lh_first->field.le_prev = &(elm)->field.le_next;          \
        (head)->lh_first = (elm);                                             \
        (elm)->field.le_prev = &(head)->lh_first;                             \
    } while (0)
#define LIST_INSERT_AFTER(listelm, elm, field)                                          \
    do {                                                                                \
        if (((elm)->field.le_next = (listelm)->field.le_next) != 0)                     \
            (listelm)->field.le_next->field.le_prev = &(elm)->field.le_next;            \
        (listelm)->field.le_next = (elm);                                               \
        (elm)->field.le_prev = &(listelm)->field.le_next;                               \
    } while (0)
#define LIST_REMOVE(elm, field)                                                \
    do {                                                                       \
        if ((elm)->field.le_next != 0)                                         \
            (elm)->field.le_next->field.le_prev = (elm)->field.le_prev;        \
        *(elm)->field.le_prev = (elm)->field.le_next;                          \
    } while (0)

#define TAILQ_HEAD(name, type)   \
    struct name {                \
        struct type* tqh_first;  \
        struct type** tqh_last;  \
    }
#define TAILQ_HEAD_INITIALIZER(head) {0, &(head).tqh_first}
#define TAILQ_ENTRY(type)         \
    struct {                      \
        struct type* tqe_next;    \
        struct type** tqe_prev;   \
    }
#define TAILQ_FIRST(head) ((head)->tqh_first)
#define TAILQ_EMPTY(head) ((head)->tqh_first == 0)
#define TAILQ_NEXT(elm, field) ((elm)->field.tqe_next)
#define TAILQ_INIT(head)                             \
    do {                                             \
        (head)->tqh_first = 0;                       \
        (head)->tqh_last = &(head)->tqh_first;       \
    } while (0)
#define TAILQ_FOREACH(var, head, field) for ((var) = TAILQ_FIRST(head); (var); (var) = TAILQ_NEXT(var, field))
#define TAILQ_FOREACH_SAFE(var, head, field, tvar) \
    for ((var) = TAILQ_FIRST(head); (var) && ((tvar) = TAILQ_NEXT(var, field), 1); (var) = (tvar))
#define TAILQ_INSERT_HEAD(head, elm, field)                                    \
    do {                                                                       \
        if (((elm)->field.tqe_next = (head)->tqh_first) != 0)                  \
            (head)->tqh_first->field.tqe_prev = &(elm)->field.tqe_next;        \
        else                                                                   \
            (head)->tqh_last = &(elm)->field.tqe_next;                         \
        (head)->tqh_first = (elm);                                             \
        (elm)->field.tqe_prev = &(head)->tqh_first;                            \
    } while (0)
#define TAILQ_INSERT_TAIL(head, elm, field)                  \
    do {                                                     \
        (elm)->field.tqe_next = 0;                           \
        (elm)->field.tqe_prev = (head)->tqh_last;            \
        *(head)->tqh_last = (elm);                           \
        (head)->tqh_last = &(elm)->field.tqe_next;           \
    } while (0)
#define TAILQ_REMOVE(head, elm, field)                                         \
    do {                                                                       \
        if (((elm)->field.tqe_next) != 0)                                      \
            (elm)->field.tqe_next->field.tqe_prev = (elm)->field.tqe_prev;     \
        else                                                                   \
            (head)->tqh_last = (elm)->field.tqe_prev;                          \
        *(elm)->field.tqe_prev = (elm)->field.tqe_next;                        \
    } while (0)
