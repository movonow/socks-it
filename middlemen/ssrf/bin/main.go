package main

import (
	"encoding/json"
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

var ssrfURL = flag.String("ssrfURL", "http://localhost:10081/ssrf", "SSRF Vulnerable endpoint")

func main() {
	flag.Parse()
	defer atexit.Exit(0)

	logger := logs.GetLogger("run/app.log", "Debug")

	serveURL, err := url.Parse(*ssrfURL)
	if err != nil {
		logger.Error("Failed to parse vulnerable URL", "error", err)
		return
	}

	http.HandleFunc(serveURL.Path,
		func(w http.ResponseWriter, request *http.Request) {
			proxyHandler(w, request, logger)
		})

	logger.Info("Starting web server", "address", serveURL.String())
	if err := http.ListenAndServe(serveURL.Host, nil); err != nil {
		logger.Warn("Stop to serve", "error", err)
	}
}

func proxyHandler(w http.ResponseWriter, r *http.Request, logger *slog.Logger) {
	// Only allow POST requests
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read the request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusBadRequest)
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	var proxyRequest = struct {
		Method  string      `json:"method"`
		URL     string      `json:"url"`
		Headers http.Header `json:"headers,omitempty"`
		Body    string      `json:"body"`
	}{}
	if err := json.Unmarshal(body, &proxyRequest); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	client := &http.Client{
		Timeout: time.Second * 10,
	}
	resp, err := client.Post(proxyRequest.URL, "text/plain", io.NopCloser(strings.NewReader(proxyRequest.Body)))
	if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching URL: %v", err), http.StatusInternalServerError)
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		logger.Warn("Failed to write response", "error", err)
	}
}
