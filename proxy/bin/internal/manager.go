package internal

import (
	"context"
	"errors"
	"github.com/tebeka/atexit"
	"io"
	"log/slog"
	"math"
	"socks.it/proxy"
	"socks.it/proxy/decorators"
	"sync"
	"sync/atomic"
	"time"
)

var (
	errIgnoreMessage = errors.New("ignore message")
	errEventNotified = errors.New("none original error")
	errUserCancelled = errors.New("user cancelled service")
)

// TunnelID obtains the virtual channel used for sending and receiving data. Purpose of this design:
// The SOCKS5 connection logic on the client side and the connection and data transmission logic on the server side are
// relatively stable, so they can be handled in proxy/bin/[client, server].
// Push/Pull targets the Middleman service:
// The channel for the Pusher is used to push data to the Transport,
// and the channel for the Puller is used to pull data from the Transport.

// TunnelInitiator initiates a TCP connection, which is also a Tunnel
type TunnelInitiator interface {
	Open(context.Context, *OpenRequest) (*OpenResponse, *slog.Logger, error)

	Pusher() chan<- *Bundle
	Puller() <-chan []byte

	io.Closer
}

type TunnelListener interface {
	ListenAndServe(func(string) Tunnel, func(Tunnel)) error

	Pusher() chan<- *Bundle
	Puller() <-chan []byte

	io.Closer
}

type Manager struct {
	name   string
	peer   string
	logger *slog.Logger

	// implementation details
	writeSpace int
	eventChan  chan any
	pushChan   chan *Bundle

	tunnelTable map[string]*Tunnel
	tunnelLock  sync.Mutex //fixme: 2024/10/19 必要性
}

func New(name, peer string, logger *slog.Logger) *Manager {
	m := &Manager{
		name:        name,
		peer:        peer,
		logger:      logger,
		eventChan:   make(chan any, 128),
		tunnelTable: make(map[string]*Tunnel),
		pushChan:    make(chan *Bundle, proxy.PushChanSize),
	}

	if m.logger == nil {
		m.logger = slog.Default()
	}
	m.logger = m.logger.With("role", m.name)
	return m
}

func (m *Manager) Setup(middleman proxy.Middleman) error {
	transportClosed := atomic.Bool{}
	exitNotify := make(chan struct{})
	exitDone := make(chan struct{})
	var transport *routeDecorator

	atexit.Register(func() {
		close(exitNotify)
		if transportClosed.CompareAndSwap(false, true) {
			// NewTransport can fail.
			if transport != nil {
				_ = transport.Close()
			}
		}
		<-exitDone
	})

	serve := func() error {
		netTransport, err := middleman.NewTransport()
		if err != nil {
			return err
		}

		m.logger.Info("transport is working")

		gather := decorators.NewGather(netTransport, 50*time.Millisecond, middleman.WriteSpace(), m.logger)
		transport = newRouteDecorator(gather, m.logger)
		defer func() {
			if transportClosed.CompareAndSwap(false, true) {
				_ = transport.Close()
			}
		}()

		transport.Attach(m.eventChan)
		m.writeSpace = middleman.WriteSpace() - transport.MetaLength()

		pullErrChan := make(chan error)
		defer close(pullErrChan)
		go func() {
			m.pullPump(transport, pullErrChan)
			m.eventChan <- exitEvent{}
		}()

		pushErrChan := make(chan error)
		defer close(pushErrChan)
		go func() {
			m.pushPump(transport, pushErrChan)
		}()

		var firstErr error
		select {
		case firstErr = <-pullErrChan:
		case firstErr = <-pushErrChan:
		case <-exitNotify:
			firstErr = errUserCancelled
		}

		select {
		case <-pullErrChan:
		case <-pushErrChan:
		}
		return firstErr
	}

	go func() {
		if err := middleman.Setup(); err != nil {
			close(exitDone)
			m.logger.Error("setup middleman", "error", err)
			return
		}
		defer func() {
			if err := middleman.Teardown(); err != nil {
				m.logger.Warn("teardown middleman", "error", err)
			}
		}()

		wait := 10
		for {
			m.logger.Info("create transport")
			err := serve()
			m.logger.Warn("transport stopped", "error", err)

			wait = wait * 2
			wait = int(math.Min(float64(wait), 300))
			timer := time.NewTimer(time.Duration(wait) * time.Second)
			select {
			case <-exitNotify:
				close(exitDone)
				return
			case <-timer.C:
			}
		}
	}()

	return nil
}

func (m *Manager) Teardown() error {
	m.tunnelLock.Lock()
	defer m.tunnelLock.Unlock()
	for _, tunnel := range m.tunnelTable {
		close(tunnel.pullChan)
		_ = tunnel.wait()
	}
	clear(m.tunnelTable)
	close(m.eventChan)

	return nil
}

func (m *Manager) WriteSpace() int {
	return m.writeSpace
}

func (m *Manager) NewInitiator() (*Tunnel, error) {
	l := m.newTunnel(nextTunnelID())
	// fixme：this is subtle.
	l.nextPullID = 1
	return l, nil
}

func (m *Manager) NewListener() (*Tunnel, error) {
	return m.newTunnel(listenerName), nil
}

func (m *Manager) Create(name string) *Tunnel {
	l := m.newTunnel(name)
	// fixme：this is subtle, Connect message consumed an ID
	l.nextPullID = 2
	return l
}

func (m *Manager) Remove(t *Tunnel) {
	m.tunnelLock.Lock()
	defer m.tunnelLock.Unlock()
	m.remove(t)
}

func (m *Manager) remove(t *Tunnel) {
	if _, ok := m.tunnelTable[t.id]; ok {
		delete(m.tunnelTable, t.id)
		close(t.pullChan)
	}
}

func (m *Manager) newTunnel(name string) *Tunnel {
	t := &Tunnel{
		id:       name,
		pushChan: m.pushChan,
		pullChan: make(chan []byte, proxy.PullChanSize),
		logger:   m.logger.With("tid", name),
	}

	m.tunnelLock.Lock()
	defer m.tunnelLock.Unlock()
	m.tunnelTable[t.id] = t

	return t
}

type exitEvent struct{}

func (m *Manager) pushPump(transport *routeDecorator, errChan chan<- error) {
	pollFunc := func() error {
		select {
		case bundle, ok := <-m.pushChan:
			if !ok {
				return errors.New("write push chan closed")
			}

			transport.setWriteHead(bundle.Tunnel.newHead(m.name, m.peer, bundle.Command))

			m.logger.Debug("push packet", "head", &transport.writeHead, "size", len(bundle.Data))
			//m.logger.Debug("push packet", "head", &transport.writeHead, "data", string(bundle.Data))

			w, err := transport.NextWriter()
			if err != nil {
				return err
			}
			defer func() {
				if err = w.Close(); err != nil {
					m.logger.Warn("flush packet", "error", err)
				}
			}()

			if _, err := w.Write(bundle.Data); err != nil {
				return err
			}

			return nil

		case event := <-m.eventChan:
			switch event.(type) {
			case exitEvent:
				return errEventNotified
			default:
				return transport.Handle(event)
			}
		}
	}

	for {
		if err := pollFunc(); err != nil {
			errChan <- err
			break
		}
	}
}

func (m *Manager) pullPump(transport *routeDecorator, errChan chan<- error) {
	pollFunc := func() error {
		r, err := transport.NextReader()
		if err != nil {
			return err
		}

		head := transport.ReadHead()

		// Packets read by Transport are distributed to different tunnel forwarding routines,
		// so this copy operation eliminates the need to synchronize the Reader.
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}

		if head.To != m.name {
			return errIgnoreMessage
		}

		m.logger.Debug("pull packet", "head", &head, "size", len(data))
		//m.logger.Debug("pull packet", "head", &head, "data", data)

		// The server only has a communication-ready Tunnel after the Connect process is complete.
		if head.Command == Command(Connect).String() {
			head.TunnelID = listenerName
		}

		func() {
			m.tunnelLock.Lock()
			defer m.tunnelLock.Unlock()

			tunnel, ok := m.tunnelTable[head.TunnelID]
			if !ok {
				//m.logger.Warn("tunnel not found", "head", &head)
				return
			}

			m.dispatch(tunnel, &head, data)
		}()

		return nil
	}

	for {
		if err := pollFunc(); err != nil {
			if !errors.Is(err, errIgnoreMessage) {
				errChan <- err
				break
			}
		}
	}
}

// Don't call blocking method, still holding m.tunnelLock
func (m *Manager) dispatch(t *Tunnel, head *tunnelHead, data []byte) {
	handlers := []func(*tunnelHead, []byte){
		Connect: func(head *tunnelHead, data []byte) {
			select {
			case t.pullChan <- data:
			default:
				t.logger.Warn("pullChan is full")
			}
		},
		ConnectAck: func(head *tunnelHead, data []byte) {
			t.pull(head, data)
		},
		Execute: func(head *tunnelHead, data []byte) {
		},
		ExecuteAck: func(head *tunnelHead, data []byte) {
		},
		Forward: func(head *tunnelHead, data []byte) {
			t.pull(head, data)
		},
		Close: func(_ *tunnelHead, _ []byte) {
			m.remove(t)
		},
	}

	for index, handler := range handlers {
		if Command(index).String() == head.Command {
			handler(head, data)
			break
		}
	}
}
