package tcp

import (
	"net"
	"net/netip"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/lowmem"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/redir"
)

type Handler interface {
	M.TCPConnectionHandler
	E.Handler
}

type Listener struct {
	bind    netip.AddrPort
	handler Handler
	trans   redir.TransproxyMode
	lAddr   *net.TCPAddr
	*net.TCPListener
}

type Error struct {
	Conn  net.Conn
	Cause error
}

func (e *Error) Error() string {
	return e.Cause.Error()
}

func (e *Error) Unwrap() error {
	return e.Cause
}

func (e *Error) Close() error {
	return common.Close(e.Conn)
}

func NewTCPListener(listen netip.AddrPort, handler Handler, options ...Option) *Listener {
	listener := &Listener{
		bind:    listen,
		handler: handler,
	}
	for _, option := range options {
		option(listener)
	}
	return listener
}

func (l *Listener) Start() error {
	tcpListener, err := net.ListenTCP(M.NetworkFromNetAddr("tcp", l.bind.Addr()), net.TCPAddrFromAddrPort(l.bind))
	if err != nil {
		return err
	}
	if l.trans == redir.ModeTProxy {
		l.lAddr = tcpListener.Addr().(*net.TCPAddr)
		fd, err := common.GetFileDescriptor(tcpListener)
		if err != nil {
			return err
		}
		err = redir.TProxy(fd, l.bind.Addr().Is6())
		if err != nil {
			return E.Cause(err, "configure tproxy")
		}
	}
	l.TCPListener = tcpListener
	go l.loop()
	return nil
}

func (l *Listener) Close() error {
	if l == nil || l.TCPListener == nil {
		return nil
	}
	return l.TCPListener.Close()
}

func (l *Listener) loop() {
	for {
		tcpConn, err := l.Accept()
		if err != nil {
			l.Close()
			return
		}
		metadata := M.Metadata{
			Source: M.AddrPortFromNetAddr(tcpConn.RemoteAddr()),
		}
		switch l.trans {
		case redir.ModeRedirect:
			metadata.Destination, _ = redir.GetOriginalDestination(tcpConn)
		case redir.ModeTProxy:
			lAddr := tcpConn.LocalAddr().(*net.TCPAddr)
			rAddr := tcpConn.RemoteAddr().(*net.TCPAddr)

			if lAddr.Port != l.lAddr.Port || !lAddr.IP.Equal(rAddr.IP) && !lAddr.IP.IsLoopback() && !lAddr.IP.IsPrivate() {
				metadata.Destination = M.AddrPortFromNetAddr(lAddr)
			}
		}
		go func() {
			hErr := l.handler.NewConnection(tcpConn, metadata)
			if hErr != nil {
				l.handler.HandleError(&Error{Conn: tcpConn, Cause: hErr})
			}
			lowmem.Free()
		}()
	}
}
