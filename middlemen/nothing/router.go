// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package nothing

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"socks.it/nothing/internal"
	"socks.it/utils/errs"
	"time"
)

// Client is a middleman between the websocket connection and the router.
type Client struct {
	local  string
	remote string
	router *Router

	conn *websocket.Conn
	send chan []byte
}

func (c *Client) readPump() error {
	defer func() {
		c.router.unregister <- c
		_ = c.conn.Close()
	}()

	//c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { _ = c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			//if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			//	log.Printf("error: %v", err)
			//}
			return errs.WithStack(err)
		}

		c.router.incoming <- Message{client: c, data: message}
	}
}

func (c *Client) writePump() error {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The router closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return errors.New("application stopped")
			}

			w, err := c.conn.NextWriter(websocket.BinaryMessage)
			if err != nil {
				return errs.WithStack(err)
			}
			_, err = w.Write(message)
			if err != nil {
				return errs.WithStack(err)
			}

			if err := w.Close(); err != nil {
				return errs.WithStack(err)
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return errs.WithStack(err)
			}
		}
	}
}

func (h *Router) serveWs(w http.ResponseWriter, r *http.Request) {
	var upgrade = websocket.Upgrader{
		ReadBufferSize:    readBufferSize,
		WriteBufferSize:   writeBufferSize,
		EnableCompression: true,
	}

	conn, err := upgrade.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("failed client upgrade", "error", err)
		http.Error(w, fmt.Sprintf("failed client upgrade client websocket: %v", err), http.StatusInternalServerError)
		return
	}
	client := &Client{
		local:  r.URL.Query().Get("local"),
		remote: r.URL.Query().Get("remote"),
		router: h,
		conn:   conn,
		send:   make(chan []byte, 128),
	}
	client.router.register <- client

	go func() {
		if err = client.writePump(); err != nil {
			h.logger.Error("writePump failed", "error", err)
		}
	}()

	go func() {
		if err = client.readPump(); err != nil {
			h.logger.Error("readPump failed", "error", err)
		}
	}()
}

type Message struct {
	client *Client
	data   []byte
}

// Router maintains the set of active clients and routes messages client the target client.
type Router struct {
	caCertPool *x509.CertPool
	logger     *slog.Logger

	server     *http.Server
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	incoming   chan Message

	runContext context.Context
	runCancel  context.CancelFunc
}

func newRouter(caCertPool *x509.CertPool, logger *slog.Logger) *Router {
	r := &Router{
		caCertPool: caCertPool,
		logger:     logger,
		incoming:   make(chan Message, 512),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		clients:    make(map[*Client]bool),
	}
	r.runContext, r.runCancel = context.WithCancel(context.Background())

	return r
}

func (h *Router) run(ctx context.Context) {
	for {
		select {
		case client := <-h.register:
			h.logger.Info("client register", "local", client.local, "remote", client.remote)
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				h.logger.Info("client unregister", "local", client.local, "remote", client.remote)
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.incoming:
			h.dispatch(message)
		case <-ctx.Done():
			return
		}
	}
}

func (h *Router) dispatch(message Message) {
	for client := range h.clients {
		if client.local == message.client.remote {
			select {
			case client.send <- message.data:
			default:
				h.logger.Error("send message channel is full", "client", client.local, "client", client.remote)
				close(client.send)
				delete(h.clients, client)
			}

			return
		}
	}

	h.logger.Warn("client not register", "client", message.client.remote, "to", message.client.local)
}

func (h *Router) listenAndServe() error {
	wsURL, err := url.Parse(*baseURL)
	if err != nil {
		return errs.WithStack(err)
	}

	go h.run(h.runContext)

	tlsConfig := &tls.Config{
		ClientCAs:        h.caCertPool,
		ClientAuth:       tls.RequireAndVerifyClientCert,
		MinVersion:       tls.VersionTLS13,
		CurvePreferences: []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		},
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			h.logger.Info("connect attempt", "client", hello.Conn.RemoteAddr())
			return nil, nil
		},
		VerifyPeerCertificate: func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			subjects := make([]pkix.Name, 0, len(verifiedChains))
			for _, chain := range verifiedChains {
				for _, cert := range chain {
					subjects = append(subjects, cert.Subject)
					if cert.Subject.CommonName == internal.ClientSubjectName {
						return nil
					}
				}
			}

			for _, subject := range subjects {
				h.logger.Warn("unknown client", "subject", subject)
			}

			return errors.New("reject unknown client")
		},
	}

	handler := http.NewServeMux()
	handler.HandleFunc(wsURL.Path, func(w http.ResponseWriter, r *http.Request) {
		h.serveWs(w, r)
	})
	h.logger.Info("serving websocket", "address", *baseURL)

	h.server = &http.Server{
		Addr:      wsURL.Host,
		Handler:   handler,
		TLSConfig: tlsConfig,
		ErrorLog:  log.New(io.Discard, "", 0),
	}

	return h.server.ListenAndServeTLS(*serverCertPath, *serverKeyPath)
}

func (h *Router) shutdown() error {
	h.runCancel()
	return h.server.Shutdown(context.Background())
}
