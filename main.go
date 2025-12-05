package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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

	// Calculate uptime
	uptime := time.Since(startTime)
	
	// Build response
	response := map[string]interface{}{
		"status": "running",
		"service": "Trip Short Link Proxy",
		"version": "2.1.0-golang-file",
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

// handleRequest processes HTTP proxy requests
func (ps *ProxyServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Health check endpoint
	if r.URL.Path == "/check" || r.URL.Path == "/health" {
		ps.handleCheck(w, r)
		return
	}

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

func (ps *ProxyServer) Start() error {
	// Load initial mappings from config file
	if err := ps.loadMappingsFromFile(); err != nil {
		log.Fatalf("Failed to load initial config: %v", err)
	}

	// Create HTTP server
	server := &http.Server{
		Addr:    ":" + ps.config.Port,
		Handler: http.HandlerFunc(ps.handleRequest),
	}

	// Setup signal handlers
	shutdownChan := make(chan os.Signal, 1)
	reloadChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	signal.Notify(reloadChan, syscall.SIGUSR1) // SIGUSR1 for reload

	// Handle reload signal in background
	go func() {
		for range reloadChan {
			ps.reloadMappings()
		}
	}()

	// Start server in goroutine
	go func() {
		log.Printf("[%s] Trip Short Link Proxy service listening on port %s", time.Now().Format(time.RFC3339), ps.config.Port)
		log.Printf("[%s] Config file: %s", time.Now().Format(time.RFC3339), ps.config.ConfigFile)
		log.Printf("[%s] Send SIGUSR1 to reload config: kill -USR1 %d", time.Now().Format(time.RFC3339), os.Getpid())
		
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for shutdown signal
	sig := <-shutdownChan
	log.Printf("[%s] %s signal received: shutting down gracefully...", time.Now().Format(time.RFC3339), sig)

	// Shutdown server with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Error during server shutdown: %v", err)
		return err
	}

	log.Printf("[%s] HTTP server closed successfully", time.Now().Format(time.RFC3339))
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

