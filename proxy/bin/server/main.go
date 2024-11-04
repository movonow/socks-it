package main

import (
	"flag"
	"github.com/tebeka/atexit"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"socks.it/proxy/bin/internal"
	"socks.it/ssrf"
	"socks.it/utils/logs"
	"syscall"
	//_ "net/http/pprof" // debug
)

var logLevel = flag.String("logLevel", "Info", "Set log level: [Debug,Info,Warn,Error]")

var proxyURL = flag.String("proxyURL", "", "HTTP Proxy URL")

func main() {
	flag.Parse()

	logger := logs.GetLogger("run/server.log", *logLevel)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		atexit.Exit(0)
	}()

	//go func() {
	//	const debugURL = "localhost:38082"
	//	logger.Info("start debug service", "url", fmt.Sprintf("http://%s/debug/pprof", debugURL))
	//	err := http.ListenAndServe(debugURL, nil)
	//	if err != nil {
	//		slog.Error("failed to start pprof", "error", err)
	//	}
	//}()

	//middleman := chatroom.New("server", "client", logger, chatroom.WithProxy(*proxyURL))
	//middleman := comment.New(logger, comment.WithProxy(*proxyURL))
	//middleman := nothing.New("server", "client", logger, nothing.WithProxy(*proxyURL), nothing.EnableServer())
	middleman := ssrf.New("server", "client", logger, ssrf.WithProxy(*proxyURL))

	manager := internal.New("server", "client", logger)
	if err := manager.Setup(middleman); err != nil {
		logger.Error("failed to setup manager", "error", err)
		return
	}
	defer func() {
		_ = manager.Teardown()
	}()

	// Only use public API of proxy.TunnelID
	listener, err := manager.NewListener()
	if err != nil {
		logger.Error("failed to NewListener", "error", err)
		return
	}
	defer func() {
		_ = listener.Close()
	}()

	if err = listener.ListenAndServe(
		func(name string) *internal.Tunnel {
			return manager.Create(name)
		},
		func(tunnel *internal.Tunnel, rw io.ReadWriter, logger *slog.Logger) error {
			socket := &internal.SocketIO{Reader: rw, Writer: rw, ReadBufferSize: manager.WriteSpace()}
			return internal.Exchange(tunnel, socket, logger)
		},
		func(tunnel *internal.Tunnel) {
			manager.Remove(tunnel)
			_ = tunnel.Close()
		}); err != nil {
		logger.Error("failed to serve", "error", err)
	}
}
