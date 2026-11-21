/* What the stack's objects of FreeBSD's kernel name and nothing of it
 * has here: no kernel map of a machine's, whose end subr_param asks; no
 * AIO, whose queue a socket's job never reaches; the dynamic section of
 * the kernel's ELF file, which link_elf reads, empty; and strdup(9) and
 * asprintf(9), of malloc(9), which subr_prf and kern_malloc have, not
 * compiled here. */
#include <sys/param.h>
#include <sys/systm.h>
#include <sys/aio.h>
#include <sys/malloc.h>
#include <sys/libkern.h>
#include <machine/stdarg.h>
#include <vm/vm.h>

int vsnprintf(char *, size_t, const char *, va_list);

vm_offset_t vm_max_kernel_address = ~(vm_offset_t)0;

struct _dynamic {
	long d_tag;
	long d_val;
} _DYNAMIC;

void
aio_cancel(struct kaiocb *job)
{
	(void)job;
}

char *
strdup(const char *string, struct malloc_type *type)
{
	size_t length = strlen(string) + 1;
	char *copy = malloc(length, type, M_WAITOK);

	memcpy(copy, string, length);
	return (copy);
}

int
vasprintf(char **ret, struct malloc_type *type, const char *format, va_list ap)
{
	va_list again;
	int length;

	va_copy(again, ap);
	length = vsnprintf(NULL, 0, format, again);
	va_end(again);
	*ret = malloc(length + 1, type, M_NOWAIT);
	if (*ret == NULL)
		return (-1);
	vsnprintf(*ret, length + 1, format, ap);
	return (length);
}

int
asprintf(char **ret, struct malloc_type *type, const char *format, ...)
{
	va_list ap;
	int length;

	va_start(ap, format);
	length = vasprintf(ret, type, format, ap);
	va_end(ap);
	return (length);
}
