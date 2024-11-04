package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/tebeka/atexit"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"socks.it/utils/logs"
	"strings"
	"time"
)

const (
	HttpServeAddr = "localhost:28080"
)

var logger *slog.Logger

func main() {
	flag.Parse()

	defer atexit.Exit(0)
	logger = logs.GetLogger("run/test.log", "Debug")
	logger.Error("Starting test")
	content := strings.Repeat("0123456789ABCDEF", 10)
	serveHTTP(content)
	time.Sleep(1 * time.Second)

	request(fmt.Sprintf("http://%s/hi", HttpServeAddr), content)
	//request("https://www.baidu.com")
}

func request(reqUrl, expect string) {
	p, err := url.Parse("socks5://localhost:9015")
	if err != nil {
		logger.Error("failed to parse url", "error", err)
		return
	}

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(p),
		},
		Timeout: 20 * time.Second,
	}

	resp, err := client.Get(reqUrl)
	if err != nil {
		logger.Error("failed to do request", "error", err)
		return
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error("failed to read body", "error", err)
		return
	}
	if expect != string(body) {
		logger.Error("unexpected response")
	}
	logger.Info("received response")
	fmt.Println(hex.Dump(body))
}

func serveHTTP(content string) {
	go func() {
		http.HandleFunc("/hi", func(w http.ResponseWriter, r *http.Request) {
			logger.Info("receive request", "address", r.RemoteAddr)

			_, err := io.WriteString(w, content)
			if err != nil {
				logger.Error("write response", "error", err)
			}
		})

		logger.Info("Starting service", "address", HttpServeAddr)
		logger.Error("stop serving", "error",
			http.ListenAndServe(HttpServeAddr, nil))
	}()
}
