package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Service start time for uptime calculation
var startTime time.Time

// Configuration from environment variables
type Config struct {
	Port       string
	ConfigFile string
}

// APIResponse represents the response from the remote API
type APIResponse struct {
	Success   bool     `json:"success"`
	Message   string   `json:"message"`
	TimeTaken int      `json:"timeTaken"`
	Data      []Record `json:"data"`
}

// Record represents a short link mapping
type Record struct {
	ID             int     `json:"id"`
	ShortURL       string  `json:"shortUrl"`
	LongURL        string  `json:"longUrl"`
	Protocol       string  `json:"protocol"`
	TicketID       string  `json:"ticketId"`
	UserID         *string `json:"userId"`          // Can be string like "S72000" or null
	Department     *string `json:"department"`      // Can be string or null
	Username       *string `json:"username"`        // Can be string or null
	AllowURIConcat bool    `json:"allowUriConcat"`
}

// ProxyServer holds the state of the proxy service
type ProxyServer struct {
	config       Config
	shortLinkMap map[string]string
	mapMutex     sync.RWMutex
	lastSyncTime time.Time
	requestCount uint64
	countMutex   sync.RWMutex
}

func loadConfig() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}

	configFile := os.Getenv("CONFIG_FILE")
	if configFile == "" {
		configFile = "config.json"
	}

	return Config{
		Port:       port,
		ConfigFile: configFile,
	}
}

func NewProxyServer(config Config) *ProxyServer {
	return &ProxyServer{
		config:       config,
		shortLinkMap: make(map[string]string),
	}
}

// loadMappingsFromFile loads mappings from local config file
func (ps *ProxyServer) loadMappingsFromFile() error {
	log.Printf("[%s] Loading mappings from %s...", time.Now().Format(time.RFC3339), ps.config.ConfigFile)

	body, err := os.ReadFile(ps.config.ConfigFile)
	if err != nil {
		log.Printf("[%s] Failed to read config file: %v", time.Now().Format(time.RFC3339), err)
		return err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		log.Printf("[%s] Failed to parse JSON: %v", time.Now().Format(time.RFC3339), err)
		return err
	}

	if !apiResp.Success {
		log.Printf("[%s] Config file indicates error: %s", time.Now().Format(time.RFC3339), apiResp.Message)
		return fmt.Errorf("config error: %s", apiResp.Message)
	}

	newMap := make(map[string]string)
	for _, record := range apiResp.Data {
		if record.ShortURL != "" && record.LongURL != "" {
			// Construct full URL with protocol
			fullURL := record.Protocol + "://" + record.LongURL
			newMap[strings.ToLower(record.ShortURL)] = fullURL
		} else {
			log.Printf("[%s] Invalid record received: %+v", time.Now().Format(time.RFC3339), record)
		}
	}

	if len(newMap) == 0 {
		return fmt.Errorf("no valid records found in config file")
	}

	ps.mapMutex.Lock()
	ps.shortLinkMap = newMap
	ps.lastSyncTime = time.Now()
	ps.mapMutex.Unlock()
	
	log.Printf("[%s] Mappings loaded successfully. Total %d records.", time.Now().Format(time.RFC3339), len(newMap))
	return nil
}

// reloadMappings reloads mappings from config file (triggered by signal)
func (ps *ProxyServer) reloadMappings() {
	log.Printf("[%s] Reloading mappings from config file...", time.Now().Format(time.RFC3339))
	if err := ps.loadMappingsFromFile(); err != nil {
		log.Printf("[%s] Failed to reload mappings: %v", time.Now().Format(time.RFC3339), err)
	} else {
		log.Printf("[%s] Mappings reloaded successfully", time.Now().Format(time.RFC3339))
	}
}

// handleCheck provides health check and status information
func (ps *ProxyServer) handleCheck(w http.ResponseWriter, r *http.Request) {
	ps.mapMutex.RLock()
	mapSize := len(ps.shortLinkMap)
	lastSync := ps.lastSyncTime
	ps.mapMutex.RUnlock()

	// Get request count
	ps.countMutex.RLock()
	requestCount := ps.requestCount
	ps.countMutex.RUnlock()

	// Calculate uptime
	uptime := time.Since(startTime)
	
	// Build response
	response := map[string]interface{}{
		"status": "running",
		"service": "Trip Shorts Proxy",
		"version": "2.3.0",
		"request_count": requestCount,
		"uptime": uptime.String(),
		"mappings": map[string]interface{}{
			"total": mapSize,
			"last_load": lastSync.Format(time.RFC3339),
			"last_load_ago": time.Since(lastSync).String(),
		},
		"config": map[string]interface{}{
			"port": ps.config.Port,
			"config_file": ps.config.ConfigFile,
		},
		"timestamp": time.Now().Format(time.RFC3339),
		"note": "Send SIGUSR1 to reload config: kill -USR1 <pid>",
	}

	// Add sample mappings (first 5)
	ps.mapMutex.RLock()
	samples := make([]map[string]string, 0, 5)
	count := 0
	for key, value := range ps.shortLinkMap {
		if count >= 5 {
			break
		}
		samples = append(samples, map[string]string{
			"short": key,
			"target": value,
		})
		count++
	}
	ps.mapMutex.RUnlock()
	response["sample_mappings"] = samples

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handlePac serves the PAC file dynamically
func (ps *ProxyServer) handlePac(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	
	// Determine the port currently running on
	port := ps.config.Port
	
	// Simple PAC that directs non-dot hosts to us, others DIRECT
	pacTemplate := `function FindProxyForURL(url, host) {
    var safeHost = host.toLowerCase();
    // If it's a plain hostname (no dots), use our proxy
    if (safeHost.indexOf('.') === -1) {
        return "SOCKS5 127.0.0.1:%s; SOCKS 127.0.0.1:%s; DIRECT";
    }
    return "DIRECT";
}`
	fmt.Fprintf(w, pacTemplate, port, port)
}

// handleRequest processes HTTP proxy requests
func (ps *ProxyServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Health check endpoint
	if r.URL.Path == "/check" || r.URL.Path == "/health" {
		ps.handleCheck(w, r)
		return
	}

	// PAC file endpoint
	if r.URL.Path == "/proxy.pac" {
		ps.handlePac(w, r)
		return
	}

	// Increment request counter (excluding health checks and PAC)
	ps.countMutex.Lock()
	ps.requestCount++
	ps.countMutex.Unlock()

	// Extract hostname from Host header
	host := r.Host
	if host == "" {
		http.Error(w, "Host header is required", http.StatusBadRequest)
		return
	}

	// Remove port if present
	hostParts := strings.Split(host, ":")
	requestedHost := strings.ToLower(hostParts[0])

	// Look up target URL
	ps.mapMutex.RLock()
	targetURL, found := ps.shortLinkMap[requestedHost]
	ps.mapMutex.RUnlock()

	if !found {
		log.Printf("[%s] No mapping found for host: %s. Returning 404.", time.Now().Format(time.RFC3339), requestedHost)
		http.Error(w, fmt.Sprintf("No short link mapping found for \"%s\"", requestedHost), http.StatusNotFound)
		return
	}

	// Parse target URL
	parsedTarget, err := url.Parse(targetURL)
	if err != nil {
		log.Printf("[%s] Invalid target URL for host %s: %v", time.Now().Format(time.RFC3339), requestedHost, err)
		http.Error(w, "Internal configuration error", http.StatusInternalServerError)
		return
	}

	// Preserve path, query, and fragment
	parsedTarget.Path = strings.TrimRight(parsedTarget.Path, "/") + "/" + strings.TrimLeft(r.URL.Path, "/")
	parsedTarget.RawQuery = r.URL.RawQuery
	parsedTarget.Fragment = r.URL.Fragment

	// Clean up path (remove duplicate slashes)
	parsedTarget.Path = strings.ReplaceAll(parsedTarget.Path, "//", "/")

	finalURL := parsedTarget.String()
	log.Printf("[%s] Redirecting %s%s -> %s", time.Now().Format(time.RFC3339), requestedHost, r.URL.String(), finalURL)

	// Send 302 redirect
	http.Redirect(w, r, finalURL, http.StatusFound)
}

// --- Mixed Protocol (HTTP + SOCKS5) Support ---

// ChannelListener implements net.Listener but accepts connections from a channel
type ChannelListener struct {
	addr    net.Addr
	conns   chan net.Conn
	closed  chan struct{}
	closeMu sync.Mutex
}

func NewChannelListener(addr net.Addr) *ChannelListener {
	return &ChannelListener{
		addr:   addr,
		conns:  make(chan net.Conn),
		closed: make(chan struct{}),
	}
}

func (l *ChannelListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *ChannelListener) Close() error {
	l.closeMu.Lock()
	defer l.closeMu.Unlock()
	select {
	case <-l.closed:
		return nil
	default:
		close(l.closed)
		return nil
	}
}

func (l *ChannelListener) Addr() net.Addr {
	return l.addr
}

// PeekConn wraps a net.Conn to support peeking and unreading bytes
type PeekConn struct {
	net.Conn
	r *bufio.Reader
}

func NewPeekConn(c net.Conn) *PeekConn {
	return &PeekConn{
		Conn: c,
		r:    bufio.NewReader(c),
	}
}

func (c *PeekConn) Read(p []byte) (int, error) {
	return c.r.Read(p)
}

func (c *PeekConn) Peek(n int) ([]byte, error) {
	return c.r.Peek(n)
}

func (ps *ProxyServer) Start() error {
	// Load initial mappings from config file
	if err := ps.loadMappingsFromFile(); err != nil {
		log.Fatalf("Failed to load initial config: %v", err)
	}

	// Create the actual TCP listener
	listener, err := net.Listen("tcp", ":"+ps.config.Port)
	if err != nil {
		return err
	}
	
	// Create our virtual listener for the HTTP server
	httpListener := NewChannelListener(listener.Addr())

	// Create HTTP server
	server := &http.Server{
		Handler: http.HandlerFunc(ps.handleRequest),
	}

	// Setup signal handlers
	shutdownChan := make(chan os.Signal, 1)
	reloadChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	signal.Notify(reloadChan, syscall.SIGUSR1)

	// Handle reload signal
	go func() {
		for range reloadChan {
			ps.reloadMappings()
		}
	}()

	// Start connection dispatcher
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				select {
				case <-httpListener.closed:
					return // Listener closed
				default:
					log.Printf("Accept error: %v", err)
					continue
				}
			}
			go ps.handleConnection(conn, httpListener)
		}
	}()

	// Start HTTP server using our virtual listener
	go func() {
		log.Printf("[%s] Trip Short Link Proxy (SOCKS5+HTTP) listening on port %s", time.Now().Format(time.RFC3339), ps.config.Port)
		log.Printf("[%s] Config file: %s", time.Now().Format(time.RFC3339), ps.config.ConfigFile)
		
		if err := server.Serve(httpListener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for shutdown
	sig := <-shutdownChan
	log.Printf("[%s] %s signal received: shutting down gracefully...", time.Now().Format(time.RFC3339), sig)

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Close the main listener first to stop accepting new raw connections
	listener.Close()
	
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error during server shutdown: %v", err)
		return err
	}

	log.Printf("[%s] HTTP server closed successfully", time.Now().Format(time.RFC3339))
	return nil
}

// handleConnection identifies protocol and performs SOCKS handshake if needed
func (ps *ProxyServer) handleConnection(rawConn net.Conn, httpListener *ChannelListener) {
	// Wrap connection to allow peeking
	conn := NewPeekConn(rawConn)
	
	// Peek first byte
	head, err := conn.Peek(1)
	if err != nil {
		if err != io.EOF {
			log.Printf("Peek error: %v", err)
		}
		conn.Close()
		return
	}

	// Check for SOCKS5 (0x05)
	if head[0] == 0x05 {
		if err := handleSocks5Handshake(conn); err != nil {
			log.Printf("SOCKS5 handshake failed: %v", err)
			conn.Close()
			return
		}
		// After successful handshake, the client will send the actual HTTP request.
		// We pass the connection (which is now positioned at the start of HTTP request) to the HTTP server.
	} 
	
	// Pass to HTTP server (either it was HTTP all along, or we unwrapped SOCKS5)
	// We need to be careful not to block if the server is shutting down
	select {
	case httpListener.conns <- conn:
	case <-httpListener.closed:
		conn.Close()
	}
}

// handleSocks5Handshake performs a minimal SOCKS5 server handshake
func handleSocks5Handshake(conn io.ReadWriter) error {
	// 1. Client Greeting
	// Version (1) + NMethods (1) + Methods (N)
	buf := make([]byte, 258)
	// Read Version and NMethods
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return fmt.Errorf("read greeting header: %w", err)
	}
	if buf[0] != 5 {
		return fmt.Errorf("unsupported version: %d", buf[0])
	}
	nMethods := int(buf[1])
	// Read Methods
	if _, err := io.ReadFull(conn, buf[:nMethods]); err != nil {
		return fmt.Errorf("read methods: %w", err)
	}

	// 2. Server Choice
	// Version (1) + Method (1)
	// We choose No Authentication (0x00)
	if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
		return fmt.Errorf("write choice: %w", err)
	}

	// 3. Client Request
	// Ver(1) + Cmd(1) + Rsv(1) + Atyp(1) + DstAddr(?) + DstPort(2)
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return fmt.Errorf("read request header: %w", err)
	}
	
	cmd := buf[1]
	if cmd != 1 { // CONNECT
		return fmt.Errorf("unsupported command: %d", cmd)
	}
	
atyp := buf[3]
	switch atyp {
	case 1: // IPv4
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return err
		}
	case 3: // Domain
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return err
		}
		addrLen := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:addrLen]); err != nil {
			return err
		}
	case 4: // IPv6
		if _, err := io.ReadFull(conn, buf[:16]); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported address type: %d", atyp)
	}
	
	// Read Port
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return err
	}

	// 4. Server Reply
	// Ver(1) + Rep(1) + Rsv(1) + Atyp(1) + BndAddr(?) + BndPort(2)
	// Rep: 0x00 (Succeeded)
	// We just return 0.0.0.0:0 as bound address
	response := []byte{
		0x05, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x00,
		0x00, 0x00,
	}
	if _, err := conn.Write(response); err != nil {
		return fmt.Errorf("write reply: %w", err)
	}

	return nil
}

func main() {
	startTime = time.Now()
	config := loadConfig()
	
	proxy := NewProxyServer(config)
	
	if err := proxy.Start(); err != nil {
		log.Fatalf("Failed to run proxy server: %v", err)
	}
}