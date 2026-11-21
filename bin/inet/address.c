/* The card's address as its lease has it, of FreeBSD's kernel, as
 * ff_veth.c sets the first: the address the interface had goes, and its
 * default route; the one it has now comes, with its mask and broadcast,
 * and the default route through its router. An address 0 is none, and so
 * is a router 0. */
#include <sys/param.h>
#include <sys/systm.h>
#include <sys/proc.h>
#include <sys/socket.h>
#include <sys/socketvar.h>
#include <sys/sockio.h>
#include <net/if.h>
#include <net/if_var.h>
#include <net/route.h>
#include <net/route/route_ctl.h>
#include <netinet/in.h>
#include <netinet/in_var.h>

int inet_address(const char *ifname, uint32_t old, uint32_t oldRouter, uint32_t address, uint32_t mask, uint32_t router);

static void
inet_sin(struct sockaddr_in *sin, uint32_t address)
{
	bzero(sin, sizeof(*sin));
	sin->sin_len = sizeof(*sin);
	sin->sin_family = AF_INET;
	sin->sin_addr.s_addr = address;
}

static int
inet_default(int command, uint32_t router)
{
	struct rt_addrinfo info;
	struct rib_cmd_info rci;
	struct sockaddr_in gw, dst, nm;

	bzero(&info, sizeof(info));
	info.rti_flags = RTF_GATEWAY;
	inet_sin(&gw, router);
	inet_sin(&dst, 0);
	inet_sin(&nm, 0);
	info.rti_info[RTAX_GATEWAY] = (struct sockaddr *)&gw;
	info.rti_info[RTAX_DST] = (struct sockaddr *)&dst;
	info.rti_info[RTAX_NETMASK] = (struct sockaddr *)&nm;
	return (rib_action(0, command, &info, &rci));
}

int
inet_address(const char *ifname, uint32_t old, uint32_t oldRouter, uint32_t address, uint32_t mask, uint32_t router)
{
	struct socket *so = NULL;
	int error;

	error = socreate(AF_INET, &so, SOCK_DGRAM, 0, curthread->td_ucred, curthread);
	if (error != 0)
		return (error);
	if (oldRouter != 0)
		inet_default(RTM_DELETE, oldRouter);
	if (old != 0) {
		struct ifreq ifr;

		bzero(&ifr, sizeof(ifr));
		strlcpy(ifr.ifr_name, ifname, sizeof(ifr.ifr_name));
		inet_sin((struct sockaddr_in *)&ifr.ifr_addr, old);
		ifioctl(so, SIOCDIFADDR, (caddr_t)&ifr, curthread);
	}
	if (address != 0) {
		struct in_aliasreq req;

		bzero(&req, sizeof(req));
		strlcpy(req.ifra_name, ifname, sizeof(req.ifra_name));
		inet_sin(&req.ifra_addr, address);
		inet_sin(&req.ifra_mask, mask);
		inet_sin(&req.ifra_broadaddr, address | ~mask);
		error = ifioctl(so, SIOCAIFADDR, (caddr_t)&req, curthread);
		if (error == 0 && router != 0)
			error = inet_default(RTM_ADD, router);
	}
	soclose(so);
	return (error);
}
