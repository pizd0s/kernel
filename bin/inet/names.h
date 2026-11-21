/* The names of FreeBSD's kernel that lib/c has too, for its own: the
 * stack's are renamed as it is compiled, as F-Stack makes them local to
 * its library on a host, and the service links both into one module.
 * strdup, asprintf and vasprintf are malloc(9)'s, of a malloc type, and
 * glue.c's. */
#define asprintf bsd_asprintf
#define domains bsd_domains
#define dummy bsd_dummy
#define free bsd_free
#define in6addr_any bsd_in6addr_any
#define in6addr_loopback bsd_in6addr_loopback
#define log bsd_log
#define malloc bsd_malloc
#define poll bsd_poll
#define printf bsd_printf
#define realloc bsd_realloc
#define strlcpy bsd_strlcpy
#define strdup bsd_strdup
#define strnlen bsd_strnlen
#define vasprintf bsd_vasprintf
#define vprintf bsd_vprintf
