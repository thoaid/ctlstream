package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/thoaid/ctlstream/internal/hub"
	"github.com/thoaid/ctlstream/internal/parser"
)

const (
	LogListURL     = "https://www.gstatic.com/ct/log_list/v3/all_logs_list.json"
	UserAgent      = "ctlstream"
	batchSize      = 512
	pollInterval   = 10 * time.Second
	maxBackoff     = time.Minute * 2
	logListRefresh = 15 * time.Minute
)

type CTLog struct {
	Description string `json:"description"`
	URL         string `json:"url"`
	State       struct {
		Usable json.RawMessage `json:"usable"`
	} `json:"state"`
}

type CertMessage struct {
	CertPEM   string         `json:"cert_pem"`
	Subject   parser.DNInfo  `json:"subject"`
	SANs      parser.SANInfo `json:"sans"`
	Issuer    parser.DNInfo  `json:"issuer"`
	NotBefore string         `json:"not_before"`
	NotAfter  string         `json:"not_after"`
	Source    string         `json:"source"`
	Timestamp int64          `json:"timestamp"`
}

type Entry struct {
	LeafInput string `json:"leaf_input"`
	ExtraData string `json:"extra_data"`
}

type LogMonitor struct {
	ctx        context.Context
	hub        *hub.Hub
	noCert     bool
	mu         sync.RWMutex
	activeLogs map[string]context.CancelFunc
}

func NewLogMonitor(ctx context.Context, h *hub.Hub, noCert bool) *LogMonitor {
	return &LogMonitor{
		ctx:        ctx,
		hub:        h,
		noCert:     noCert,
		activeLogs: make(map[string]context.CancelFunc),
	}
}

func (lm *LogMonitor) Start() error {
	if err := lm.refreshLogs(); err != nil {
		return fmt.Errorf("initial log fetch: %w", err)
	}

	go lm.periodicRefresh()
	return nil
}

func (lm *LogMonitor) periodicRefresh() {
	ticker := time.NewTicker(logListRefresh)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := lm.refreshLogs(); err != nil {
				log.Printf("failed to refresh log list: %v", err)
			}
		case <-lm.ctx.Done():
			return
		}
	}
}

func (lm *LogMonitor) refreshLogs() error {
	logs, err := fetchLogs(lm.ctx)
	if err != nil {
		return err
	}

	lm.mu.Lock()
	defer lm.mu.Unlock()

	currentLogs := make(map[string]bool)
	for _, lg := range logs {
		currentLogs[lg.URL] = true
	}

	for url, cancel := range lm.activeLogs {
		if !currentLogs[url] {
			log.Printf("removing CT log: %s", url)
			cancel()
			delete(lm.activeLogs, url)
		}
	}

	for _, lg := range logs {
		if _, exists := lm.activeLogs[lg.URL]; !exists {
			log.Printf("adding CT log: %s (%s)", lg.Description, lg.URL)
			ctx, cancel := context.WithCancel(lm.ctx)
			lm.activeLogs[lg.URL] = cancel
			go monitorLog(ctx, lm.hub, lg, lm.noCert)
		}
	}

	log.Printf("monitoring %d CT logs", len(lm.activeLogs))
	return nil
}

func fetchLogs(ctx context.Context) ([]CTLog, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, LogListURL, nil)
	req.Header.Set("User-Agent", UserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("log list fetch failed: status %d", resp.StatusCode)
	}

	var list struct {
		Operators []struct {
			Logs []CTLog `json:"logs"`
		} `json:"operators"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return nil, err
	}

	var logs []CTLog
	for _, op := range list.Operators {
		for _, lg := range op.Logs {
			if lg.State.Usable != nil {
				logs = append(logs, lg)
			}
		}
	}

	return logs, nil
}

func monitorLog(ctx context.Context, h *hub.Hub, lg CTLog, noCert bool) {
	log.Printf("starting monitor for %s (%s)", lg.Description, lg.URL)
	defer log.Printf("stopped monitoring %s", lg.Description)

	client := &http.Client{Timeout: 15 * time.Second}
	var lastIndex uint64
	backoff := pollInterval
	initialized := false

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		treeSize, err := getTreeSize(ctx, client, lg.URL)
		if err != nil {
			log.Printf("failed to get tree size for %s: %v", lg.Description, err)
			time.Sleep(backoff)
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = pollInterval

		if !initialized {
			lastIndex = treeSize
			initialized = true
			time.Sleep(pollInterval)
			continue
		}

		if lastIndex >= treeSize {
			time.Sleep(pollInterval)
			continue
		}

		for start := lastIndex; start < treeSize; start += batchSize {
			end := min(start+batchSize-1, treeSize-1)

			entries, err := getEntries(ctx, client, lg.URL, start, end)
			if err != nil {
				log.Printf("failed to get entries for %s (range %d-%d): %v", lg.Description, start, end, err)
				break
			}

			for _, e := range entries {
				certs, _ := parser.ParseCertificates(e.LeafInput, e.ExtraData)
				for _, cert := range certs {
					msg := CertMessage{
						Subject:   parser.ParseDN(cert.Subject),
						Issuer:    parser.ParseDN(cert.Issuer),
						NotBefore: cert.NotBefore.Format(time.RFC3339),
						NotAfter:  cert.NotAfter.Format(time.RFC3339),
						Source:    lg.Description,
						Timestamp: time.Now().Unix(),
						SANs:      parser.ParseSANs(cert),
					}

					if !noCert {
						msg.CertPEM = parser.CertToPEM(cert)
					}

					if data, err := json.Marshal(msg); err == nil {
						h.Msgs <- append(data, '\n')
					}
				}
			}
			lastIndex = end + 1
		}
		time.Sleep(pollInterval)
	}
}

func getTreeSize(ctx context.Context, client *http.Client, logURL string) (uint64, error) {
	url := fmt.Sprintf("%s/ct/v1/get-sth", strings.TrimSuffix(logURL, "/"))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return 0, fmt.Errorf("status %d, ", resp.StatusCode)
	}

	var sth struct {
		TreeSize uint64 `json:"tree_size"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sth); err != nil {
		return 0, err
	}

	return sth.TreeSize, nil
}

func getEntries(ctx context.Context, client *http.Client, logURL string, start, end uint64) ([]Entry, error) {
	url := fmt.Sprintf("%s/ct/v1/get-entries?start=%d&end=%d",
		strings.TrimSuffix(logURL, "/"), start, end)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var result struct {
		Entries []Entry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Entries, nil
}
