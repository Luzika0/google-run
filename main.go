
package main

import (
	"errors"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPort        = "8080"
	defaultUpstreamPort = "80"
	defaultMaxConnections = 50
	dialTimeout        = 5 * time.Second
)

func handleClient(client net.Conn, targets []string) {
	defer client.Close()

	var upstream net.Conn
	var selectedTarget string

	// Try upstreams in order and stop at the first successful connection.
	for _, target := range targets {
		conn, err := net.DialTimeout("tcp", target, dialTimeout)
		if err != nil {
			log.Printf("Upstream connection failed (%s): %v", target, err)
			continue
		}

		upstream = conn
		selectedTarget = target
		break
	}

	if upstream == nil {
		log.Print("All upstream connections failed")
		return
	}
	defer upstream.Close()

	log.Printf("Connected to upstream %s", selectedTarget)

	// Each direction runs one copy operation.
	// If either direction ends, close both connections to unblock the other.
	errs := make(chan error, 2)

	copyStream := func(dst, src net.Conn) {
		_, err := io.Copy(dst, src)
		errs <- err
	}

	go copyStream(upstream, client)
	go copyStream(client, upstream)

	err := <-errs
	if err != nil &&
		!errors.Is(err, net.ErrClosed) &&
		!errors.Is(err, os.ErrDeadlineExceeded) {
		log.Printf("TCP forwarding ended: %v", err)
	}

	// Closing both sockets prevents the other copy from lingering.
	_ = client.Close()
	_ = upstream.Close()

	// Wait until the other copy goroutine has exited.
	<-errs
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	rawIPs := os.Getenv("V2RAY_SERVER_IPS")
	if rawIPs == "" {
		rawIPs = os.Getenv("V2RAY_SERVER_IP")
	}
	if strings.TrimSpace(rawIPs) == "" {
		log.Fatal("V2RAY_SERVER_IP or V2RAY_SERVER_IPS is required")
	}

	upstreamPort := os.Getenv("V2RAY_SERVER_PORT")
	if upstreamPort == "" {
		upstreamPort = defaultUpstreamPort
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

	maxConnections := defaultMaxConnections
	if value := os.Getenv("MAX_CONNECTIONS"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 {
			log.Fatal("MAX_CONNECTIONS must be a positive integer")
		}
		maxConnections = n
	}

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Cannot listen on port %s: %v", port, err)
	}
	defer listener.Close()

	// This semaphore bounds the number of active proxy sessions.
	slots := make(chan struct{}, maxConnections)

	log.Printf(
		"Listening on :%s; upstreams: %s; max connections: %d",
		port, strings.Join(targets, ", "), maxConnections,
	)

	for {
		client, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		select {
		case slots <- struct{}{}:
			go func(conn net.Conn) {
				defer func() { <-slots }()
				handleClient(conn, targets)
			}(client)

		default:
			// Capacity is full. Reject this socket rather than allowing
			// unlimited goroutines and connections to consume memory.
			_ = client.Close()
			log.Printf("Connection limit reached (%d)", maxConnections)
		}
	}
}
