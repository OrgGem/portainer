package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"strings"
	"sync"
)

func main() {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", "/var/run/docker.sock")
	}}
	proxy := &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) { r.Out.URL.Scheme = "http"; r.Out.URL.Host = "docker" }, Transport: transport}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/review/status":
			select {
			case <-entered:
				fmt.Fprint(w, "blocked")
			default:
				fmt.Fprint(w, "waiting")
			}
			return
		case "/review/release":
			once.Do(func() { close(release) })
			fmt.Fprint(w, "ok")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/containers/create") && strings.Contains(r.URL.Query().Get("name"), os.Getenv("BLOCK_NAME")) {
			select {
			case <-entered:
			default:
				close(entered)
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		proxy.ServeHTTP(w, r)
	})
	if err := http.ListenAndServe(":8080", nil); err != nil {
		panic(err)
	}
}
