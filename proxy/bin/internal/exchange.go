package internal

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"socks.it/proxy"
	"socks.it/utils/errs"
	"time"
)

type SocketIO struct {
	io.Reader
	io.Writer
	ReadBufferSize int
}

func Exchange(tunnel *Tunnel, socket *SocketIO, logger *slog.Logger) error {
	logger.Debug("tunnel opened")

	ctx, cancelPull := context.WithCancel(context.Background())
	keepAlive := time.NewTimer(proxy.TunnelIdleTimeout)

	pushErrChan := make(chan error)
	defer close(pushErrChan)
	go func() {
		// 通知pull退出
		defer func() {
			cancelPull()
		}()

		pushErrChan <- push(tunnel, socket.Reader, socket.ReadBufferSize, keepAlive)
	}()

	pullErrChan := make(chan error)
	defer close(pullErrChan)
	go func() {
		defer func() {
			_ = socket.Writer.(io.Closer).Close()
		}()

		pullErrChan <- pull(ctx, tunnel, socket.Writer, keepAlive)
	}()

	// Ideally, if a network disconnection is detected, notify the other end to close the connection promptly.
	// The current implementation assumes that the push routine's Read call should detect the network event first,
	// and if the push routine fails, it is likely due to a notification received from the other end.
	// fixme 2024.10.23: The Exchange function returning causes the tunnel to be closed.
	// This does not currently affect Disconnect event handling, but the logic appears unusual.
	var err error
	select {
	case err = <-pushErrChan:
		notice := &Disconnect{Error: err}
		data, err := notice.Encode()
		if err == nil {
			logger.Error("encode disconnect", "error", err, "disconnect", notice)
			break
		}

		// notify Close to the other end.
		tunnel.Pusher() <- &Bundle{
			Tunnel:  tunnel,
			Command: Close,
			Data:    []byte(data),
		}
	case err = <-pullErrChan:
		// received Close event from the other end.
	}
	logger.Debug("tunnel closed", "error", err)

	// waits to join the second routine.
	select {
	case <-pushErrChan:
	case <-pullErrChan:
	}

	return err
}

func push(tunnel *Tunnel, r io.Reader, readBufferSize int, keepAlive *time.Timer) error {
	// quit on closing r.(net.conn)
	for {
		buf := make([]byte, readBufferSize)

		// Of course Read can block.
		n, err := r.Read(buf)
		if err != nil {
			return errs.WithStack(err)
		}

		keepAlive.Reset(proxy.TunnelIdleTimeout)

		// Buffered delegate will block when buffer is full.
		tunnel.Pusher() <- &Bundle{Tunnel: tunnel, Command: Forward, Data: buf[:n]}
	}
}

func pull(ctx context.Context, tunnel *Tunnel, w io.Writer, keepAlive *time.Timer) error {
	for {
		select {
		case data, ok := <-tunnel.Puller():
			if !ok {
				return io.EOF
			}

			_, err := io.Copy(w, bytes.NewReader(data))
			if err != nil {
				return errs.WithStack(err)
			}

			keepAlive.Reset(proxy.TunnelIdleTimeout)

		case <-ctx.Done():
			return errs.WithStack(ctx.Err())

		case <-keepAlive.C:
			// Don't hang too long.
			return errs.WithStack(net.ErrClosed)
		}
	}
}
