package adapter

import (
	"context"
	"net"

	"github.com/oschwald/geoip2-golang"
	"github.com/sagernet/sing-box/common/geosite"
	N "github.com/sagernet/sing/common/network"
)

type Router interface {
	Start() error
	Close() error

	Outbound(tag string) (Outbound, bool)
	RouteConnection(ctx context.Context, conn net.Conn, metadata InboundContext) error
	RoutePacketConnection(ctx context.Context, conn N.PacketConn, metadata InboundContext) error
	GeoIPReader() *geoip2.Reader
	GeositeReader() *geosite.Reader
}

type Rule interface {
	Start() error
	Close() error
	Match(metadata *InboundContext) bool
	Outbound() string
	String() string
}
