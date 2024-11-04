package nothing

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"socks.it/proxy"
	"socks.it/utils/errs"
	"time"
)

const (
	dialTimeout = 30 * time.Second
	// Time allowed client write a message client the peer.
	writeWait = 10 * time.Second

	// Time allowed client read the next pong message client the peer.
	pongWait = 60 * time.Second

	// Send pings client peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed client peer.
	readBufferSize  = 4096 * 4
	writeBufferSize = 4096 * 4
)

var baseURL = flag.String("wsURL", "wss://127.0.0.1:10443/ws", "websocket Service URL")

var caCertPath = flag.String("caCertPath", "certs/ca.crt", "CA certificate file path")
var clientCertPath = flag.String("clientCertPath", "certs/client.crt", "client application certificate file path")
var clientKeyPath = flag.String("clientKeyPath", "certs/client.key", "client application key file path")
var serverCertPath = flag.String("serverCertPath", "certs/server.crt", "server application certificate file path")
var serverKeyPath = flag.String("serverKeyPath", "certs/server.key", "server application key file path")

type noMiddleman struct {
	local       string
	remote      string
	asServer    bool
	rawWsURL    string
	rawProxyURL string
	logger      *slog.Logger

	httpServer *http.Server
	caCertPool *x509.CertPool
	router     *Router
}

type Option func(*noMiddleman)

func WithProxy(rawProxyURL string) Option {
	return func(m *noMiddleman) {
		m.rawProxyURL = rawProxyURL
	}
}

func EnableServer() Option {
	return func(m *noMiddleman) {
		m.asServer = true
	}
}

func New(local, remote string, logger *slog.Logger, options ...Option) proxy.Middleman {
	m := &noMiddleman{
		local:    local,
		remote:   remote,
		rawWsURL: *baseURL,
		logger:   logger,
	}

	for _, option := range options {
		option(m)
	}

	return m
}

func (m *noMiddleman) Setup() error {
	caCertFile, err := os.ReadFile(*caCertPath)
	if err != nil {
		return errs.WithStack(err)
	}
	m.caCertPool = x509.NewCertPool()
	m.caCertPool.AppendCertsFromPEM(caCertFile)

	if m.asServer {
		m.router = newRouter(m.caCertPool, m.logger)
		go func() {
			if err := m.router.listenAndServe(); err != nil {
				m.logger.Error("service stopped", "error", err)
			}
		}()
	}

	return nil
}

func (m *noMiddleman) Teardown() error {
	if m.asServer {
		return m.router.shutdown()
	}

	return nil
}

func (m *noMiddleman) WriteSpace() int {
	return writeBufferSize
}

func (m *noMiddleman) Ordered() bool {
	return true
}

func (m *noMiddleman) NewTransport() (proxy.Transporter, error) {
	conn, err := m.dial()

	if err != nil {
		return nil, err
	}

	t := newWsTransport(conn)
	t.keepalive()

	return t, nil
}

func (m *noMiddleman) dial() (conn *websocket.Conn, err error) {
	certificate, err := tls.LoadX509KeyPair(*clientCertPath, *clientKeyPath)
	if err != nil {
		return nil, errs.WithStack(err)
	}

	dialer := &websocket.Dialer{
		Proxy: func(request *http.Request) (*url.URL, error) {
			if m.rawProxyURL == "" {
				return nil, nil
			}
			return url.Parse(m.rawProxyURL)
		},
		TLSClientConfig: &tls.Config{
			RootCAs:      m.caCertPool,
			Certificates: []tls.Certificate{certificate},
		},
		HandshakeTimeout:  dialTimeout,
		ReadBufferSize:    readBufferSize,
		WriteBufferSize:   writeBufferSize,
		EnableCompression: true,
	}

	var resp *http.Response
	conn, resp, err = dialer.Dial(fmt.Sprintf("%s?local=%s&remote=%s", m.rawWsURL, m.local, m.remote), nil)
	if err != nil {
		return nil, errs.WithStack(err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	defer func() {
		if conn != nil && err != nil {
			_ = conn.Close()
		}
	}()

	if err = conn.SetReadDeadline(time.Time{}); err != nil {
		return nil, errs.WithStack(err)
	}

	m.logger.Info("connection created", "url", m.rawWsURL)
	return conn, nil
}

type wsTransport struct {
	proxy.EventTrigger
	*websocket.Conn
	pingTimer *time.Timer
}

func newWsTransport(conn *websocket.Conn) *wsTransport {
	return &wsTransport{Conn: conn}
}

func (t *wsTransport) NextWriter() (io.WriteCloser, error) {
	_ = t.Conn.SetWriteDeadline(time.Now().Add(writeWait))
	return t.Conn.NextWriter(websocket.BinaryMessage)
}

func (t *wsTransport) NextReader() (io.Reader, error) {
	_, r, err := t.Conn.NextReader()
	return r, errs.WithStack(err)
}

func (t *wsTransport) Handle(event any) error {
	switch event.(type) {
	case pingEvent:
		return t.Conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(pongWait))
	default:
		return errs.WithStack(fmt.Errorf("reach last event handler: %v", event))
	}
}

func (t *wsTransport) Close() error {
	if t.pingTimer != nil {
		t.pingTimer.Stop()
	}
	return t.Conn.Close()
}

type pingEvent struct{}

func (t *wsTransport) keepalive() {
	t.pingTimer = time.AfterFunc(pingPeriod, func() {
		t.Submit(pingEvent{})
		t.pingTimer.Reset(pingPeriod)
	})
}
