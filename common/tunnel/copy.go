package tunnel

import (
	"context"
	"net"
	"runtime"
	"time"

	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/bufio"
	E "github.com/sagernet/sing/common/exceptions"
)

func CopyEarlyConn(ctx context.Context, conn net.Conn, serverConn net.Conn) error {
	_payload := buf.StackNew()
	payload := common.Dup(_payload)
	err := conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if err != nil {
		return err
	}
	_, err = payload.ReadFrom(conn)
	if err != nil && !E.IsTimeout(err) {
		return E.Cause(err, "read payload")
	}
	err = conn.SetReadDeadline(time.Time{})
	if err != nil {
		payload.Release()
		return err
	}
	_, err = serverConn.Write(payload.Bytes())
	if err != nil {
		return E.Cause(err, "client handshake")
	}
	runtime.KeepAlive(_payload)
	return bufio.CopyConn(ctx, conn, serverConn)
}
