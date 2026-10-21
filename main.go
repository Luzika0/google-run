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
	defaultUpstreamPort   = "443" // Standard secure port for V2Ray/Xray TLS
	defaultMaxConnections = 500   // Protects Cloud Run from running out of memory
	dialTimeout           = 10 * time.Second
)

func handleClient(clientConn net.Conn, targetAddr string) {
	// Defer handles connection closures safely exactly once when the function exits
	defer clientConn.Close()

	remoteConn, err := net.DialTimeout("tcp", targetAddr, dialTimeout)
	if err != nil {
		log.Printf("Upstream connection to %s failed: %v", targetAddr, err)
		return
	}
	defer remoteConn.Close()

	errs := make(chan error, 2)

	// Bidirectional data pipe copy
	go func() {
		_, err := io.Copy(remoteConn, clientConn)
		errs <- err
	}()
	go func() {
		_, err := io.Copy(clientConn, remoteConn)
		errs <- err
	}()

	// Wait for the first stream copy error/completion to finish
	err = <-errs
	if err != nil && !errors.Is(err, net.ErrClosed) {
		log.Printf("TCP session closing: %v", err)
	}

	// Wait for the second goroutine to exit to prevent data race leaks
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
		log.Fatalf("Cannot listen on %s: %v", listenAddr, err)
	}
	defer listener.Close()

	// Rate limiting channel tokens (The critical safety mechanism)
	slots := make(chan struct{}, maxConnections)

	log.Printf("Proxy listening on %s, forwarding to %s (Max Slots: %d)", listenAddr, targetAddr, maxConnections)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept failed: %v", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// Anti-burst pacing delay: Spreads out fast connection setups 
		// by 2 milliseconds to stop "rate exceeded" errors on upstream firewalls.
		time.Sleep(2 * time.Millisecond)

		// Blocks safely if capacity is reached. Traffic queues up in the 
		// OS network stack instead of causing heavy container crashes.
		slots <- struct{}{}
		go func(conn net.Conn) {
			handleClient(conn, targetAddr)
			<-slots // Release the slot back when finished
		}(clientConn)
	}
}
