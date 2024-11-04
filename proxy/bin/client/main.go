package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/tebeka/atexit"
	"github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"socks.it/proxy/bin/internal"
	"socks.it/ssrf"
	"socks.it/utils/logs"
	"strings"
	"syscall"
	//_ "net/http/pprof" // debug
)

var logLevel = flag.String("logLevel", "Info", "Set log level: [Debug,Info,Warn,Error]")
var socksAddr = flag.String("socksAddr", "127.0.0.1:9015", "Socks5 proxy serve address")
var localResolve = flag.Bool("localResolve", false, "Resolve domain name in local")

// var proxyURL = flag.String("proxyURL", "http://localhost:8080", "HTTP Proxy URL")
var proxyURL = flag.String("proxyURL", "", "HTTP Proxy URL, debug facility")

func main() {
	flag.Parse()

	logger := logs.GetLogger("run/client.log", *logLevel)

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		atexit.Exit(0)
	}()

	//go func() {
	//	const debugURL = "localhost:38080"
	//	logger.Info("start debug service", "url", fmt.Sprintf("http://%s/debug/pprof", debugURL))
	//	err := http.ListenAndServe(debugURL, nil)
	//	if err != nil {
	//		slog.Error("failed to start pprof", "error", err)
	//	}
	//}()

	//middleman := chatroom.New("client", "server", logger, chatroom.WithProxy(*proxyURL))
	//middleman := comment.New(logger, comment.WithProxy(*proxyURL))
	//middleman := nothing.New("client", "server", logger, nothing.WithProxy(*proxyURL))
	middleman := ssrf.New("client", "server", logger, ssrf.WithProxy(*proxyURL))

	manager := internal.New("client", "server", logger)
	if err := manager.Setup(middleman); err != nil {
		logger.Error("failed to setup manager", "error", err)
		return
	}
	defer func() {
		_ = manager.Teardown()
	}()

	logger.Info("Starting sock5 proxy", "address", "socks5://"+*socksAddr)
	server := socks5.NewServer(
		//socks5.WithLogger(socks5.NewLogger(slog.NewLogLogger(logger.Handler(), slog.LevelDebug))),
		socks5.WithLogger(socks5.NewLogger(log.New(io.Discard, "", 0))),
		socks5.WithResolver(resolver()),
		socks5.WithConnectHandle(func(ctx context.Context, writer io.Writer, request *socks5.Request) error {
			if dead(request.DstAddr.String()) {
				//if dead(request.DstAddr.String()) || noisy(request.DstAddr.String()) {
				return fmt.Errorf("discarded by proxy: %s", request.DstAddr.String())
			}

			initiator, err := manager.NewInitiator()
			if err != nil {
				logger.Error("new initiator", "error", err)
				return err
			}
			defer func() {
				manager.Remove(initiator)
				_ = initiator.Close()
			}()

			openRequest := internal.OpenRequest{ClientAddr: request.RemoteAddr, ServerAddr: request.DstAddr}

			return initiator.OpenAndServe(ctx, &openRequest,
				func(addr net.Addr, err error) error {
					return reply(writer, addr, err)
				},
				func(tunnel *internal.Tunnel, logger *slog.Logger) error {
					socket := internal.SocketIO{Reader: request.Reader, Writer: writer, ReadBufferSize: manager.WriteSpace()}
					return internal.Exchange(initiator, &socket, logger)
				})
		}),
	)

	if err := server.ListenAndServe("tcp", *socksAddr); err != nil {
		logger.Error("server stopped", "error", err)
	}
}

func reply(socksWriter io.Writer, bindAddr net.Addr, err error) error {
	if err != nil {
		msg := err.Error()
		resp := statute.RepHostUnreachable
		if strings.Contains(msg, "refused") {
			resp = statute.RepConnectionRefused
		} else if strings.Contains(msg, "network is unreachable") {
			resp = statute.RepNetworkUnreachable
		}

		if replyErr := socks5.SendReply(socksWriter, resp, nil); replyErr != nil {
			return err
		}

		return err
	}

	return socks5.SendReply(socksWriter, statute.RepSuccess, bindAddr)
}

type nopResolver struct{}

// Resolve implement interface NameResolver
func (d nopResolver) Resolve(ctx context.Context, _ string) (context.Context, net.IP, error) {
	return ctx, net.IP{}, nil
}

func resolver() socks5.NameResolver {
	if *localResolve {
		return socks5.DNSResolver{}
	}
	return nopResolver{}
}

func noisy(url string) bool {
	return strings.Contains(url, "services.mozilla.com") ||
		strings.Contains(url, "addons.mozilla.org") ||
		strings.Contains(url, "cdn.mozilla.net") ||
		strings.Contains(url, "telemetry.mozilla.org") ||
		strings.Contains(url, "detectportal.firefox.com") ||
		strings.Contains(url, "aus5.mozilla.org") ||
		strings.Contains(url, "spocs.getpocket.com") ||
		strings.Contains(url, "ocsp.digicert.com") ||
		strings.Contains(url, "safebrowsing.googleapis.com") ||
		strings.Contains(url, "www.google.com")
}

func dead(url string) bool {
	return strings.Contains(url, "ocsp.crlocsp.cn") // spammed with the connect error.
}
