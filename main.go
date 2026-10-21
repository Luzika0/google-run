package main

import (
	"errors"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultPort           = "8080"
	defaultUpstreamPort   = "443" // Changed to 443 as V2Ray proxies rarely use port 80
	defaultMaxConnections = 500   // Increased baseline default
	dialTimeout           = 5 * time.Second
)

func handleClient(client net.Conn, targets []string) {
	// Let defer handle all connection closures safely exactly once
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

	log.Printf("Proxying session: %s <-> %s", client.RemoteAddr(), selectedTarget)

	errs := make(chan error, 2)

	// Bidirectional data pipe copy
	go func() {
		_, err := io.Copy(upstream, client)
		errs <- err
	}()
	go func() {
		_, err := io.Copy(client, upstream)
		errs <- err
	}()

	// Wait for the first stream copy error/completion to finish
	err := <-errs
	if err != nil &&
		!errors.Is(err, net.ErrClosed) &&
		!errors.Is(err, os.ErrDeadlineExceeded) {
		log.Printf("TCP forwarding session closed with network status: %v", err)
	}

	// Keep execution alive until the second goroutine exits safely to prevent leaks
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
		log.Fatal("V2RAY_SERVER_IP or V2RAY_SERVER_IPS environment variable is required")
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

	// Rate limiting channel tokens
	slots := make(chan struct{}, maxConnections)

	log.Printf(
		"Listening on :%s; Upstreams: %s; Max allocated slots: %d",
		port, strings.Join(targets, ", "), maxConnections,
	)

	for {
		client, err := listener.Accept()
		if err != nil {
			log.Printf("Accept critical system error: %v", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// REMOVED select-default drop logic. 
		// If capacity is full, this blocks cleanly, queuing up traffic inside the TCP stack 
		// instead of generating hard socket drops that break Cloud Run's availability metrics.
		slots <- struct{}{}
		go func(conn net.Conn) {
			handleClient(conn, targets)
			<-slots // Release the slot back to the queue when finished
		}(client)
	}
}
