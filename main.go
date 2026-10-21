package main

import (
	"errors"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"time"
)

const (
	defaultPort           = "8080"
	defaultUpstreamPort   = "80"   // Matches your active upstream configuration
	defaultMaxConnections = 500  
	dialTimeout           = 5 * time.Second // Tightened to drop dead connections faster
)

func handleClient(clientConn net.Conn, targetAddr string) {
	defer clientConn.Close()

	remoteConn, err := net.DialTimeout("tcp", targetAddr, dialTimeout)
	if err != nil {
		log.Printf("Upstream firewall rejection or failure to %s: %v", targetAddr, err)
		return
	}
	defer remoteConn.Close()

	errs := make(chan error, 2)

	// Bidirectional stream pipes
	go func() {
		_, err := io.Copy(remoteConn, clientConn)
		errs <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, remoteConn)
		errs <- err
	}()

	err = <-errs
	if err != nil && !errors.Is(err, net.ErrClosed) {
		// Log structural context without spamming stack traces
		log.Printf("Stream terminated: %v", err)
	}

	<-errs
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

	upstreamPort := os.Getenv("V2RAY_SERVER_PORT")
	if upstreamPort == "" {
		upstreamPort = defaultUpstreamPort
	}

	maxConnections := defaultMaxConnections
	if value := os.Getenv("MAX_CONNECTIONS"); value != "" {
		if n, err := strconv.Atoi(value); err == nil && n > 0 {
			maxConnections = n
		}
	}

	targetAddr := net.JoinHostPort(serverIP, upstreamPort)
	listenAddr := ":" + port

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Network interface binding failed on %s: %v", listenAddr, err)
	}
	defer listener.Close()

	slots := make(chan struct{}, maxConnections)

	log.Printf("Proxy online. Forwarding to %s | Max concurrent connections: %d", targetAddr, maxConnections)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Socket accept error: %v", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// ELEVATED PACING DELAY: Spreads out aggressive browser connection requests
		// to stop upstream firewalls from identifying the proxy traffic as a DDoS burst.
		time.Sleep(10 * time.Millisecond)

		slots <- struct{}{}
		go func(conn net.Conn) {
			handleClient(conn, targetAddr)
			<-slots
		}(clientConn)
	}
}
