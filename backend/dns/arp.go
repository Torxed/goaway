package arp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type vendorResponse struct {
	Success bool   `json:"success"`
	Found   bool   `json:"found"`
	Company string `json:"company"`
}

type ARPCache struct {
	mu    sync.RWMutex
	table map[string]string
}

var (
	cache      = &ARPCache{table: make(map[string]string)}
	httpClient = &http.Client{Timeout: 5 * time.Second}
)

func ProcessARPTable() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Update on first startup
	updateARPTable()

	for range ticker.C {
		updateARPTable()
	}
}

func updateARPTable() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "arp", "-a")
	out, err := cmd.Output()
	if err != nil {
		fmt.Println("Error running ARP command:", err)
		return
	}

	newTable := make(map[string]string)
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.Trim(line, " \t\r")
		if line == "" {
			continue
		}

		line = strings.ReplaceAll(line, "(", "")
		line = strings.ReplaceAll(line, ")", "")

		fields := strings.Fields(line)
		if len(fields) >= 3 {
			ip := fields[1]
			mac := strings.ToLower(fields[3])
			if isValidMAC(mac) {
				newTable[ip] = mac
			}
		}
	}

	cache.mu.Lock()
	cache.table = newTable
	cache.mu.Unlock()
}

func GetMacAddress(ip string) string {
	cache.mu.RLock()
	mac, exists := cache.table[ip]
	cache.mu.RUnlock()

	if exists {
		return mac
	}
	return "unknown"
}

func GetMacVendor(mac string) (string, error) {
	if mac == "" || mac == "unknown" {
		return "", fmt.Errorf("invalid MAC address")
	}

	mac = strings.ReplaceAll(mac, ":", "")
	mac = strings.ReplaceAll(mac, "-", "")
	mac = strings.ToLower(mac)

	url := fmt.Sprintf("https://api.maclookup.app/v2/macs/%s", mac)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch MAC vendor: %w", err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var result vendorResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if result.Found {
		return result.Company, nil
	}

	return "", fmt.Errorf("vendor not found")
}

func isValidMAC(mac string) bool {
	cleanMAC := strings.ReplaceAll(mac, ":", "")
	cleanMAC = strings.ReplaceAll(cleanMAC, "-", "")

	return len(cleanMAC) == 12 && cleanMAC != "000000000000"
}
