
package main

import (
	"io"
	"log"
	"net"
	"os"
	"time"
)

func handleClient(clientConn net.Conn, targetAddr string) {
	defer clientConn.Close()

	remoteConn, err := net.DialTimeout("tcp", targetAddr, 5*time.Second)
	if err != nil {
		log.Printf("Failed to connect to upstream %s: %v", targetAddr, err)
		return
	}
	defer remoteConn.Close()

	go func() {
		if _, err := io.Copy(remoteConn, clientConn); err != nil {
			log.Printf("Client-to-upstream copy error: %v", err)
		}
	}()

	if _, err := io.Copy(clientConn, remoteConn); err != nil {
		log.Printf("Upstream-to-client copy error: %v", err)
	}
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	serverIP := os.Getenv("V2RAY_SERVER_IP")
	if serverIP == "" {
		log.Fatal("V2RAY_SERVER_IP is not set")
	}

	targetAddr := net.JoinHostPort(serverIP, "80")

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Failed to listen on port %s: %v", port, err)
	}
	defer listener.Close()

	log.Printf("Listening on port %s; forwarding to %s", port, targetAddr)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		go handleClient(clientConn, targetAddr)
	}
}
