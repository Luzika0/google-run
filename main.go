
package main

import (
	"errors"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

func handleClient(client net.Conn, targets []string) {
	defer client.Close()

	var upstream net.Conn
	var target string

	// Try each upstream until a connection succeeds.
	for _, addr := range targets {
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			log.Printf("Upstream connection failed (%s): %v", addr, err)
			continue
		}

		upstream = conn
		target = addr
		break
	}

	if upstream == nil {
		log.Printf("All upstream connections failed")
		return
	}
	defer upstream.Close()

	log.Printf("Connected to upstream %s", target)

	var wg sync.WaitGroup
	wg.Add(2)

	copyStream := func(dst, src net.Conn, direction string) {
		defer wg.Done()

		_, err := io.Copy(dst, src)
		if err != nil &&
			!errors.Is(err, net.ErrClosed) &&
			!errors.Is(err, os.ErrDeadlineExceeded) {
			log.Printf("%s ended: %v", direction, err)
		}

		// Send EOF to the other side without immediately
		// closing the entire TCP connection.
		if tcp, ok := dst.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}

	go copyStream(upstream, client, "client-to-upstream")
	go copyStream(client, upstream, "upstream-to-client")

	wg.Wait()
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Comma-separated IP addresses:
	// V2RAY_SERVER_IPS=47.237.149.117,203.0.113.10
	rawIPs := os.Getenv("V2RAY_SERVER_IPS")
	if rawIPs == "" {
		rawIPs = os.Getenv("V2RAY_SERVER_IP")
	}
	if rawIPs == "" {
		log.Fatal("V2RAY_SERVER_IP or V2RAY_SERVER_IPS is required")
	}

	upstreamPort := os.Getenv("V2RAY_SERVER_PORT")
	if upstreamPort == "" {
		upstreamPort = "80"
	}

	var targets []string
	for _, ip := range strings.Split(rawIPs, ",") {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		targets = append(targets, net.JoinHostPort(ip, upstreamPort))
	}

	if len(targets) == 0 {
		log.Fatal("No valid upstream addresses configured")
	}

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatal(err)
	}
	defer listener.Close()

	log.Printf("Listening on :%s; upstreams: %s",
		port, strings.Join(targets, ", "))

	for {
		client, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		go handleClient(client, targets)
	}
}
