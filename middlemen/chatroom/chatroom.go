package chatroom

import (
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"socks.it/proxy"
	"socks.it/proxy/decorators"
	"socks.it/utils/errs"
	"time"
)

var wsURL = flag.String("wsURL", "ws://localhost:8088/ws", "chatroom Service URL")

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	readBufferSize  = 4096 * 2
	writeBufferSize = 4096 * 2
)

type chatroom struct {
	name string
	peer string

	//optional
	rawProxyURL string
	logger      *slog.Logger
}

type Option func(*chatroom)

func WithProxy(rawURL string) Option {
	return func(m *chatroom) {
		m.rawProxyURL = rawURL
	}
}

func New(name, peer string, logger *slog.Logger, options ...Option) proxy.Middleman {
	chat := &chatroom{
		name:   name,
		peer:   peer,
		logger: logger,
	}

	for _, option := range options {
		option(chat)
	}

	if chat.logger == nil {
		chat.logger = slog.Default()
	}
	chat.logger = chat.logger.With("role", chat.name)

	return chat
}

func (receiver *chatroom) Setup() error {
	return nil
}

func (receiver *chatroom) Teardown() error {
	return nil
}

func (receiver *chatroom) WriteSpace() int {
	// 内容会被base64编码
	return writeBufferSize * 3 / 4
}

func (receiver *chatroom) Ordered() bool {
	return false
}

func (receiver *chatroom) NewTransport() (proxy.Transporter, error) {
	var (
		conn *websocket.Conn
		err  error
	)

	dialer := &websocket.Dialer{
		Proxy: func(request *http.Request) (*url.URL, error) {
			if len(receiver.rawProxyURL) == 0 {
				return nil, nil
			}
			return url.Parse(receiver.rawProxyURL)
		},
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
		HandshakeTimeout: 45 * time.Second,
		ReadBufferSize:   readBufferSize,
		WriteBufferSize:  writeBufferSize,
		//EnableCompression: true,
	}

	header := http.Header{}
	header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/127.0.6533.100 Safari/537.36")
	header.Add("Origin", *wsURL)
	header.Add("Accept-Language", "zh-CN")

	conn, resp, err := dialer.Dial(*wsURL, header)
	if err != nil {
		receiver.logger.Error("dial chatroom failed", "error", err)
		return nil, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	defer func() {
		if conn != nil && err != nil {
			_ = conn.Close()
		}
	}()

	if resp.StatusCode != http.StatusSwitchingProtocols {
		receiver.logger.Error("upgrade connection failed", "error", resp.Status)
		body, _ := io.ReadAll(resp.Body)
		receiver.logger.Error("upgrade connection failed", "body", body)
		return nil, fmt.Errorf("connection not upgraded")
	}

	conn.SetPongHandler(func(string) error { _ = conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	t := &wsTransport{conn: conn, logger: receiver.logger}
	t.keepalive()

	return decorators.NewBase64Transport(t, receiver.logger), nil
}

type wsTransport struct {
	proxy.EventTrigger
	logger    *slog.Logger
	conn      *websocket.Conn
	pingTimer *time.Timer
}

func (t *wsTransport) NextReader() (io.Reader, error) {
	_, r, err := t.conn.NextReader()
	return r, errs.WithStack(err)
}

func (t *wsTransport) NextWriter() (io.WriteCloser, error) {
	_ = t.conn.SetWriteDeadline(time.Now().Add(writeWait))
	w, err := t.conn.NextWriter(websocket.TextMessage)
	return w, errs.WithStack(err)
}

type pingEvent struct{}

func (t *wsTransport) keepalive() {
	t.pingTimer = time.AfterFunc(pingPeriod, func() {
		t.Submit(pingEvent{})
		t.pingTimer.Reset(pingPeriod)
	})
}

func (t *wsTransport) Handle(event any) error {
	switch event.(type) {
	case pingEvent:
		//t.logger.Debug("handle ping")
		return t.conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(pongWait))
	default:
		return errs.WithStack(fmt.Errorf("reach last event handler: %v", event))
	}
}

func (t *wsTransport) Close() error {
	t.pingTimer.Stop()
	return t.conn.Close()
}
