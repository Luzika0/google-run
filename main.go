
package main

import (
	"io"
	"log"
	"net"
	"os"
	"time"
)

const (
	defaultPort        = "8080"
	upstreamPort       = "80"
	defaultDialTimeout = 10 * time.Second
)

func handleClient(clientConn net.Conn, targetAddr string, timeout time.Duration) {
	defer clientConn.Close()

	remoteConn, err := net.DialTimeout("tcp", targetAddr, timeout)
	if err != nil {
		log.Printf("Upstream connection to %s failed: %v", targetAddr, err)
		return
	}
	defer remoteConn.Close()

	done := make(chan struct{}, 2)

	copyData := func(dst, src net.Conn, direction string) {
		_, err := io.Copy(dst, src)
		if err != nil {
			log.Printf("%s ended: %v", direction, err)
		}
		done <- struct{}{}
	}

	go copyData(remoteConn, clientConn, "client-to-upstream")
	go copyData(clientConn, remoteConn, "upstream-to-client")

	<-done
	_ = clientConn.Close()
	_ = remoteConn.Close()
	<-done
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	serverIP := os.Getenv("V2RAY_SERVER_IP")
	if serverIP == "" {
		log.Fatal("Missing required environment variable V2RAY_SERVER_IP")
	}

	timeout := defaultDialTimeout
	if value := os.Getenv("DIAL_TIMEOUT"); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			log.Fatal("DIAL_TIMEOUT must be a positive duration, e.g. 5s or 10s")
		}
		timeout = parsed
	}

	targetAddr := net.JoinHostPort(serverIP, upstreamPort)
	listenAddr := ":" + port

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Cannot listen on %s: %v", listenAddr, err)
	}
	defer listener.Close()

	log.Printf("Proxy listening on %s", listenAddr)
	log.Printf("Forwarding to %s | Dial timeout: %s", targetAddr, timeout)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept failed: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		go handleClient(clientConn, targetAddr, timeout)
	}
}
