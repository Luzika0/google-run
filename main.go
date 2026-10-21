
package main

import (
	"io"
	"log"
	"net"
	"os"
	"time"
)

const (
	upstreamPort = "80"
	dialTimeout  = 10 * time.Second
)

func handleClient(clientConn net.Conn, targetAddr string) {
	defer clientConn.Close()

	// Connect to the upstream V2Ray server.
	remoteConn, err := net.DialTimeout("tcp", targetAddr, dialTimeout)
	if err != nil {
		log.Printf("Upstream connection to %s failed: %v", targetAddr, err)
		return
	}
	defer remoteConn.Close()

	log.Printf("Connected to upstream %s", targetAddr)

	// Copy data in both directions.
	done := make(chan struct{}, 2)

	copyData := func(dst, src net.Conn, direction string) {
		_, copyErr := io.Copy(dst, src)
		if copyErr != nil {
			log.Printf("%s ended: %v", direction, copyErr)
		}
		done <- struct{}{}
	}

	go copyData(remoteConn, clientConn, "client-to-upstream")
	go copyData(clientConn, remoteConn, "upstream-to-client")

	// When either direction ends, close both connections so
	// the other copy operation cannot remain blocked forever.
	<-done
	_ = clientConn.Close()
	_ = remoteConn.Close()
	<-done
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	serverIP := os.Getenv("V2RAY_SERVER_IP")
	if serverIP == "" {
		log.Fatal("Missing required environment variable V2RAY_SERVER_IP")
	}

	targetAddr := net.JoinHostPort(serverIP, upstreamPort)
	listenAddr := ":" + port

	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Cannot listen on %s: %v", listenAddr, err)
	}
	defer listener.Close()

	log.Printf("Proxy listening on %s", listenAddr)
	log.Printf("Forwarding connections to %s", targetAddr)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept failed: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		go handleClient(clientConn, targetAddr)
	}
}
