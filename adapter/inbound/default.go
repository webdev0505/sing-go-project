package inbound

import (
	"context"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/database64128/tfo-go"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/config"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

var _ adapter.Inbound = (*myInboundAdapter)(nil)

type myInboundAdapter struct {
	protocol       string
	network        []string
	ctx            context.Context
	router         adapter.Router
	logger         log.Logger
	tag            string
	listenOptions  config.ListenOptions
	connHandler    adapter.ConnectionHandler
	packetHandler  adapter.PacketHandler
	packetUpstream any

	// internal

	tcpListener          *net.TCPListener
	udpConn              *net.UDPConn
	packetForce6         bool
	packetAccess         sync.RWMutex
	packetOutboundClosed chan struct{}
	packetOutbound       chan *myInboundPacket
}

func (a *myInboundAdapter) Type() string {
	return a.protocol
}

func (a *myInboundAdapter) Tag() string {
	return a.tag
}

func (a *myInboundAdapter) Start() error {
	bindAddr := M.SocksaddrFromAddrPort(netip.Addr(a.listenOptions.Listen), a.listenOptions.Port)
	var listenAddr net.Addr
	if common.Contains(a.network, C.NetworkTCP) {
		var tcpListener *net.TCPListener
		var err error
		if !a.listenOptions.TCPFastOpen {
			tcpListener, err = net.ListenTCP(M.NetworkFromNetAddr("tcp", bindAddr.Addr), bindAddr.TCPAddr())
		} else {
			tcpListener, err = tfo.ListenTCP(M.NetworkFromNetAddr("tcp", bindAddr.Addr), bindAddr.TCPAddr())
		}
		if err != nil {
			return err
		}
		a.tcpListener = tcpListener
		go a.loopTCPIn()
		listenAddr = tcpListener.Addr()
	}
	if common.Contains(a.network, C.NetworkUDP) {
		udpConn, err := net.ListenUDP(M.NetworkFromNetAddr("udp", bindAddr.Addr), bindAddr.UDPAddr())
		if err != nil {
			return err
		}
		a.udpConn = udpConn
		a.packetForce6 = M.SocksaddrFromNet(udpConn.LocalAddr()).Addr.Is6()
		a.packetOutboundClosed = make(chan struct{})
		a.packetOutbound = make(chan *myInboundPacket)
		if _, threadUnsafeHandler := common.Cast[N.ThreadUnsafeWriter](a.packetUpstream); !threadUnsafeHandler {
			go a.loopUDPIn()
		} else {
			go a.loopUDPInThreadSafe()
		}
		go a.loopUDPOut()
		if listenAddr == nil {
			listenAddr = udpConn.LocalAddr()
		}
	}
	a.logger.Info("server started at ", listenAddr)
	return nil
}

func (a *myInboundAdapter) Close() error {
	return common.Close(
		common.PtrOrNil(a.tcpListener),
		common.PtrOrNil(a.udpConn),
	)
}

func (a *myInboundAdapter) upstreamHandler(metadata adapter.InboundContext) adapter.UpstreamHandlerAdapter {
	return adapter.NewUpstreamHandler(metadata, a.newConnection, a.newPacketConnection, a)
}

func (a *myInboundAdapter) upstreamContextHandler() adapter.UpstreamHandlerAdapter {
	return adapter.NewUpstreamContextHandler(a.newConnection, a.newPacketConnection, a)
}

func (a *myInboundAdapter) newConnection(ctx context.Context, conn net.Conn, metadata adapter.InboundContext) error {
	a.logger.WithContext(ctx).Info("inbound connection to ", metadata.Destination)
	return a.router.RouteConnection(ctx, conn, metadata)
}

func (a *myInboundAdapter) newPacketConnection(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext) error {
	a.logger.WithContext(ctx).Info("inbound packet connection to ", metadata.Destination)
	return a.router.RoutePacketConnection(ctx, conn, metadata)
}

func (a *myInboundAdapter) loopTCPIn() {
	tcpListener := a.tcpListener
	for {
		conn, err := tcpListener.Accept()
		if err != nil {
			return
		}
		var metadata adapter.InboundContext
		metadata.Inbound = a.tag
		metadata.Source = M.AddrPortFromNet(conn.RemoteAddr())
		go func() {
			metadata.Network = "tcp"
			ctx := log.ContextWithID(a.ctx)
			a.logger.WithContext(ctx).Info("inbound connection from ", conn.RemoteAddr())
			hErr := a.connHandler.NewConnection(ctx, conn, metadata)
			if hErr != nil {
				a.NewError(ctx, E.Cause(hErr, "process connection from ", conn.RemoteAddr()))
			}
		}()
	}
}

func (a *myInboundAdapter) loopUDPIn() {
	defer close(a.packetOutboundClosed)
	_buffer := buf.StackNewPacket()
	defer common.KeepAlive(_buffer)
	buffer := common.Dup(_buffer)
	defer buffer.Release()
	buffer.IncRef()
	defer buffer.DecRef()
	packetService := (*myInboundPacketAdapter)(a)
	var metadata adapter.InboundContext
	metadata.Inbound = a.tag
	metadata.Network = "udp"
	for {
		buffer.Reset()
		n, addr, err := a.udpConn.ReadFromUDPAddrPort(buffer.FreeBytes())
		if err != nil {
			return
		}
		buffer.Truncate(n)
		metadata.Source = addr
		err = a.packetHandler.NewPacket(a.ctx, packetService, buffer, metadata)
		if err != nil {
			a.newError(E.Cause(err, "process packet from ", addr))
		}
	}
}

func (a *myInboundAdapter) loopUDPInThreadSafe() {
	defer close(a.packetOutboundClosed)
	packetService := (*myInboundPacketAdapter)(a)
	var metadata adapter.InboundContext
	metadata.Inbound = a.tag
	metadata.Network = "udp"
	for {
		buffer := buf.NewPacket()
		n, addr, err := a.udpConn.ReadFromUDPAddrPort(buffer.FreeBytes())
		if err != nil {
			return
		}
		buffer.Truncate(n)
		metadata.Source = addr
		err = a.packetHandler.NewPacket(a.ctx, packetService, buffer, metadata)
		if err != nil {
			buffer.Release()
			a.newError(E.Cause(err, "process packet from ", addr))
		}
	}
}

func (a *myInboundAdapter) loopUDPOut() {
	for {
		select {
		case packet := <-a.packetOutbound:
			err := a.writePacket(packet.buffer, packet.destination)
			if err != nil && !E.IsClosed(err) {
				a.newError(E.New("write back udp: ", err))
			}
			continue
		case <-a.packetOutboundClosed:
		}
		for {
			select {
			case packet := <-a.packetOutbound:
				packet.buffer.Release()
			default:
				return
			}
		}
	}
}

func (a *myInboundAdapter) newError(err error) {
	a.logger.Warn(err)
}

func (a *myInboundAdapter) NewError(ctx context.Context, err error) {
	common.Close(err)
	if E.IsClosed(err) {
		a.logger.WithContext(ctx).Debug("connection closed")
		return
	}
	a.logger.Error(err)
}

func (a *myInboundAdapter) writePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	defer buffer.Release()
	if destination.Family().IsFqdn() {
		udpAddr, err := net.ResolveUDPAddr("udp", destination.String())
		if err != nil {
			return err
		}
		return common.Error(a.udpConn.WriteTo(buffer.Bytes(), udpAddr))
	}
	if a.packetForce6 && destination.Addr.Is4() {
		destination.Addr = netip.AddrFrom16(destination.Addr.As16())
	}
	return common.Error(a.udpConn.WriteToUDPAddrPort(buffer.Bytes(), destination.AddrPort()))
}

type myInboundPacketAdapter myInboundAdapter

func (s *myInboundPacketAdapter) ReadPacket(buffer *buf.Buffer) (M.Socksaddr, error) {
	n, addr, err := s.udpConn.ReadFromUDPAddrPort(buffer.FreeBytes())
	if err != nil {
		return M.Socksaddr{}, err
	}
	buffer.Truncate(n)
	return M.SocksaddrFromNetIP(addr), nil
}

func (s *myInboundPacketAdapter) WriteIsThreadUnsafe() {
}

type myInboundPacket struct {
	buffer      *buf.Buffer
	destination M.Socksaddr
}

func (s *myInboundPacketAdapter) Upstream() any {
	return s.udpConn
}

func (s *myInboundPacketAdapter) WritePacket(buffer *buf.Buffer, destination M.Socksaddr) error {
	s.packetAccess.RLock()
	defer s.packetAccess.RUnlock()

	select {
	case <-s.packetOutboundClosed:
		return os.ErrClosed
	default:
	}

	s.packetOutbound <- &myInboundPacket{buffer, destination}
	return nil
}

func (s *myInboundPacketAdapter) Close() error {
	return s.udpConn.Close()
}

func (s *myInboundPacketAdapter) LocalAddr() net.Addr {
	return s.udpConn.LocalAddr()
}

func (s *myInboundPacketAdapter) SetDeadline(t time.Time) error {
	return s.udpConn.SetDeadline(t)
}

func (s *myInboundPacketAdapter) SetReadDeadline(t time.Time) error {
	return s.udpConn.SetReadDeadline(t)
}

func (s *myInboundPacketAdapter) SetWriteDeadline(t time.Time) error {
	return s.udpConn.SetWriteDeadline(t)
}
