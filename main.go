
package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func main() {
	port := env("PORT", "8080")
	wsPath := env("WS_PATH", "/TG-@Not_Ragnar")
	vlessUUID := os.Getenv("VLESS_UUID")

	if vlessUUID == "" {
		log.Fatal("VLESS_UUID is required")
	}

	upstreamURL := os.Getenv("UPSTREAM_URL")
	if upstreamURL == "" {
		ip := os.Getenv("V2RAY_SERVER_IP")
		if ip == "" {
			log.Fatal("V2RAY_SERVER_IP or UPSTREAM_URL is required")
		}
		upstreamPort := env("V2RAY_SERVER_PORT", "80")
		upstreamURL = "http://" + ip + ":" + upstreamPort
	}

	target, err := url.Parse(upstreamURL)
	if err != nil || target.Host == "" ||
		(target.Scheme != "http" && target.Scheme != "https") {
		log.Fatal("Invalid UPSTREAM_URL")
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(
		w http.ResponseWriter,
		r *http.Request,
		err error,
	) {
		log.Printf("Upstream error: %v", err)
		http.Error(w, "Upstream unavailable",
			http.StatusBadGateway)
	}

	http.HandleFunc("/healthz", func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	http.HandleFunc("/", func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		if r.URL.Path != wsPath {
			http.NotFound(w, r)
			return
		}

		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "WebSocket upgrade required",
				http.StatusUpgradeRequired)
			return
		}

		// UUID authentication is performed by Xray upstream.
		proxy.ServeHTTP(w, r)
	})

	log.Printf("Listening on :%s", port)
	log.Printf("WebSocket path configured")
	log.Printf("Upstream: %s", target.Host)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
