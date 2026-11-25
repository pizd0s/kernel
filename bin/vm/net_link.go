package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync"

	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/containers/gvisor-tap-vsock/pkg/virtualnetwork"
	"github.com/sirupsen/logrus"
)

const (
	netName      = "0"
	netMTU       = 1500
	netBuffer    = 1 << 17
	netSubnet    = "192.168.127.0/24"
	netGateway   = "192.168.127.1"
	netGatewayHw = "5a:94:ef:e4:0c:dd"
	netHost      = "192.168.127.254"
	netZone      = "internal."
)

type NetLink struct {
	end    *PairEnd
	conn   net.Conn
	closed sync.Once
}

func startNet(end *PairEnd) {
	logrus.SetOutput(io.Discard)

	vn := throw2(virtualnetwork.New(&types.Configuration{
		MTU:               netMTU,
		Subnet:            netSubnet,
		GatewayIP:         netGateway,
		GatewayMacAddress: netGatewayHw,
		NAT:               map[string]string{netHost: "127.0.0.1"},
		GatewayVirtualIPs: []string{netHost},
		DNS: []types.Zone{{Name: netZone, Records: []types.Record{
			{Name: "host", IP: net.ParseIP(netHost)},
			{Name: "gateway", IP: net.ParseIP(netGateway)},
		}}},
		Protocol: types.BessProtocol,
	}))

	ours, theirs := net.Pipe()
	l := &NetLink{end: end, conn: ours}

	end.consume(l.sent)

	go l.receive()
	go vn.AcceptBess(context.Background(), theirs)
}

func (l *NetLink) sent(pk *Packet) {
	dropFiles(pk.files)

	if pk.kind == packetEnd {
		l.close()

		return
	}

	if _, err := l.conn.Write(pk.data); err != nil {
		l.close()
	}
}

func (l *NetLink) receive() {
	b := make([]byte, netBuffer)

	for {
		n, err := l.conn.Read(b)

		if err != nil {
			l.close()

			return
		}

		if err := l.end.deliver(nil, &Packet{data: bytes.Clone(b[:n])}, IoMode{nonblock: true}); err != nil && err != EAGAIN {
			l.close()

			return
		}
	}
}

func (l *NetLink) close() {
	l.closed.Do(func() {
		l.conn.Close()
		l.end.closed()
	})
}
