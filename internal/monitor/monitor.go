package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/thoaid/ctlstream/internal/hub"
	"github.com/thoaid/ctlstream/internal/parser"
)

const (
	LogListURL   = "https://www.gstatic.com/ct/log_list/v3/all_logs_list.json"
	UserAgent    = "ctlstream"
	batchSize    = 256
	pollInterval = 5 * time.Second
	maxBackoff   = time.Minute
)

type CTLog struct {
	Description string `json:"description"`
	URL         string `json:"url"`
	State       struct {
		Usable json.RawMessage `json:"usable"`
	} `json:"state"`
}

type CertMessage struct {
	CertPEM   string `json:"cert_pem,omitempty"`
	Subject   string `json:"subject"`
	Issuer    string `json:"issuer"`
	NotBefore string `json:"not_before"`
	NotAfter  string `json:"not_after"`
	Source    string `json:"source"`
	Timestamp int64  `json:"timestamp"`
}

type Entry struct {
	LeafInput string `json:"leaf_input"`
	ExtraData string `json:"extra_data"`
}

func FetchLogs(ctx context.Context) ([]CTLog, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, LogListURL, nil)
	req.Header.Set("User-Agent", UserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

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

func MonitorLog(ctx context.Context, h *hub.Hub, lg CTLog, noCert bool) {
	client := &http.Client{Timeout: 15 * time.Second}
	var lastIndex uint64
	backoff := time.Second
	initialized := false

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		treeSize, err := getTreeSize(ctx, client, lg.URL)
		if err != nil {
			time.Sleep(backoff)
			backoff = minDuration(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second

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
				break
			}

			for _, e := range entries {
				certs, _ := parser.ParseCertificates(e.LeafInput, e.ExtraData)
				for _, cert := range certs {
					msg := CertMessage{
						Subject:   cert.Subject.String(),
						Issuer:    cert.Issuer.String(),
						NotBefore: cert.NotBefore.Format(time.RFC3339),
						NotAfter:  cert.NotAfter.Format(time.RFC3339),
						Source:    lg.Description,
						Timestamp: time.Now().Unix(),
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
		return 0, fmt.Errorf("status %d", resp.StatusCode)
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

func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
