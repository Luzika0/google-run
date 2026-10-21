
package main

import (
	"container/list"
	"errors"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	defaultPort           = "8080"
	defaultUpstreamPort   = "80"
	defaultMaxConnections = 80
	dialTimeout           = 5 * time.Second
)

type connectionManager struct {
	mu     sync.Mutex
	max    int
	active map[net.Conn]*list.Element
	order  *list.List
}

func newConnectionManager(max int) *connectionManager {
	return &connectionManager{
		max:    max,
		active: make(map[net.Conn]*list.Element),
		order:  list.New(),
	}
}

// Add registers a new connection.
// If full, it removes and closes the oldest registered connection.
func (m *connectionManager) Add(conn net.Conn) {
	var oldest net.Conn

	m.mu.Lock()

	if m.order.Len() >= m.max {
		oldestElement := m.order.Front()
		if oldestElement != nil {
			oldest = oldestElement.Value.(net.Conn)
			delete(m.active, oldest)
			m.order.Remove(oldestElement)
		}
	}

	element := m.order.PushBack(conn)
	m.active[conn] = element

	m.mu.Unlock()

	if oldest != nil {
		log.Printf("Connection limit reached; closing oldest connection")
		_ = oldest.Close()
	}
}

func (m *connectionManager) Remove(conn net.Conn) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if element, ok := m.active[conn]; ok {
		delete(m.active, conn)
		m.order.Remove(element)
	}
}

func handleClient(
	clientConn net.Conn,
	targetAddr string,
	manager *connectionManager,
) {
	defer manager.Remove(clientConn)
	defer clientConn.Close()

	remoteConn, err := net.DialTimeout(
		"tcp",
		targetAddr,
		dialTimeout,
	)
	if err != nil {
		log.Printf("Upstream connection to %s failed: %v", targetAddr, err)
		return
	}
	defer remoteConn.Close()

	errs := make(chan error, 2)

	go func() {
		_, err := io.Copy(remoteConn, clientConn)
		errs <- err
	}()

	go func() {
		_, err := io.Copy(clientConn, remoteConn)
		errs <- err
	}()

	// When either direction ends, close both sockets so the
	// other copy operation cannot hold a connection slot forever.
	firstErr := <-errs

	_ = clientConn.Close()
	_ = remoteConn.Close()

	secondErr := <-errs

	for _, copyErr := range []error{firstErr, secondErr} {
		if copyErr != nil &&
			!errors.Is(copyErr, net.ErrClosed) &&
			!errors.Is(copyErr, io.EOF) {
			log.Printf("Proxy stream ended: %v", copyErr)
		}
	}
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
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			log.Fatalf("MAX_CONNECTIONS must be a positive integer: %q", value)
		}
		maxConnections = n
	}

	targetAddr := net.JoinHostPort(serverIP, upstreamPort)

	listener, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("Cannot listen on port %s: %v", port, err)
	}
	defer listener.Close()

	manager := newConnectionManager(maxConnections)

	log.Printf(
		"Proxy online. Upstream: %s | Max connections: %d | Policy: replace oldest",
		targetAddr,
		maxConnections,
	)

	for {
		clientConn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept failed: %v", err)
			time.Sleep(50 * time.Millisecond)
			continue
		}

		// Do not block the accept loop waiting for a free slot.
		manager.Add(clientConn)

		go handleClient(clientConn, targetAddr, manager)
	}
}
