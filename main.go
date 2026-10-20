
package main

import (
	"io"
	"log"
	"net"
	"os"
	"time"
)

func handleClient(client net.Conn, target string) {
	defer client.Close()

	upstream, err := net.DialTimeout(
		"tcp",
		target,
		10*time.Second,
	)
	if err != nil {
		log.Printf("Cannot connect to upstream %s: %v", target, err)
		return
	}
	defer upstream.Close()

	done := make(chan struct{})

	go func() {
		_, err := io.Copy(upstream, client)
		if err != nil {
			log.Printf("Client copy error: %v", err)
		}
		close(done)
	}()

	_, err = io.Copy(client, upstream)
	if err != nil {
		log.Printf("Upstream copy error: %v", err)
	}

	<-done
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ip := os.Getenv("V2RAY_SERVER_IP")
	if ip == "" {
		log.Fatal("V2RAY_SERVER_IP is required")
	}

	upstreamPort := os.Getenv("V2RAY_SERVER_PORT")
	if upstreamPort == "" {
		upstreamPort = "80"
	}

	target := net.JoinHostPort(ip, upstreamPort)

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	log.Printf("Listening on :%s; upstream: %s", port, target)

	for {
		client, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleClient(client, target)
	}
}
