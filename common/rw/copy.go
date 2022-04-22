package rw

import (
	"context"
	"io"
	"net"
	"os"

	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	"github.com/sagernet/sing/common/task"
)

func ReadFromVar(writerVar *io.Writer, reader io.Reader) (int64, error) {
	writer := *writerVar
	writerBack := writer
	for {
		if w, ok := writer.(io.ReaderFrom); ok {
			return w.ReadFrom(reader)
		}
		if f, ok := writer.(common.Flusher); ok {
			err := f.Flush()
			if err != nil {
				return 0, err
			}
		}
		if u, ok := writer.(common.WriterWithUpstream); ok {
			if u.Replaceable() && writerBack == writer {
				writer = u.Upstream()
				writerBack = writer
				writerVar = &writer
				continue
			}
			writer = u.Upstream()
			writerBack = writer
		} else {
			break
		}
	}
	return 0, os.ErrInvalid
}

func CopyConn(ctx context.Context, conn net.Conn, dest net.Conn) error {
	err := task.Run(ctx, func() error {
		defer CloseRead(conn)
		defer CloseWrite(dest)
		return common.Error(io.Copy(dest, conn))
	}, func() error {
		defer CloseRead(dest)
		defer CloseWrite(conn)
		return common.Error(io.Copy(conn, dest))
	})
	conn.Close()
	dest.Close()
	return err
}

func CopyPacketConn(ctx context.Context, conn net.PacketConn, outPacketConn net.PacketConn) error {
	return task.Run(ctx, func() error {
		_buffer := buf.With(make([]byte, buf.UDPBufferSize))
		buffer := common.Dup(_buffer)
		for {
			n, addr, err := conn.ReadFrom(buffer.FreeBytes())
			if err != nil {
				return err
			}
			buffer.Truncate(n)
			_, err = outPacketConn.WriteTo(buffer.Bytes(), addr)
			if err != nil {
				return err
			}
			buffer.FullReset()
		}
	}, func() error {
		_buffer := buf.With(make([]byte, buf.UDPBufferSize))
		buffer := common.Dup(_buffer)
		for {
			n, addr, err := outPacketConn.ReadFrom(buffer.FreeBytes())
			if err != nil {
				return err
			}
			buffer.Truncate(n)
			_, err = conn.WriteTo(buffer.Bytes(), addr)
			if err != nil {
				return err
			}
			buffer.FullReset()
		}
	})
}
