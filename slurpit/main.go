package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/alexflint/go-arg"
)

type SendCmd struct {
	URL       string `arg:"--url,required" help:"Slurpee base URL"`
	SecretID  string `arg:"--secret-id,required" help:"API secret UUID"`
	Secret    string `arg:"--secret,required" help:"API secret value"`
	Subject   string `arg:"--subject" default:"loadtest.event" help:"Event subject"`
	Rate      int    `arg:"--rate" default:"10" help:"Events per second"`
	Count     int    `arg:"--count" default:"100" help:"Total events to send"`
}

type ReceiveCmd struct {
	URL         string        `arg:"--url,required" help:"Slurpee base URL"`
	AdminSecret string        `arg:"--admin-secret,required" help:"Admin secret for subscriber registration"`
	Listen      string        `arg:"--listen" default:":9090" help:"Local listen address"`
	EndpointURL string        `arg:"--endpoint-url,required" help:"Publicly reachable URL for this receiver"`
	Subject     string        `arg:"--subject" default:"loadtest.*" help:"Subject pattern to subscribe to"`
	Duration    time.Duration `arg:"--duration" default:"30s" help:"How long to listen"`
}

type args struct {
	Send    *SendCmd    `arg:"subcommand:send" help:"Send events to Slurpee"`
	Receive *ReceiveCmd `arg:"subcommand:receive" help:"Receive events from Slurpee and measure latency"`
}

func (args) Description() string {
	return "slurpit — load testing tool for the Slurpee event broker"
}

func main() {
	var a args
	p := arg.MustParse(&a)

	switch {
	case a.Send != nil:
		runSend(a.Send)
	case a.Receive != nil:
		runReceive(a.Receive)
	default:
		p.WriteUsage(os.Stdout)
		fmt.Println()
		p.WriteHelp(os.Stdout)
		os.Exit(1)
	}
}

func runSend(cmd *SendCmd) {
	client := &http.Client{Timeout: 10 * time.Second}
	interval := time.Second / time.Duration(cmd.Rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var sent, errors int
	start := time.Now()

	for i := 0; i < cmd.Count; i++ {
		<-ticker.C

		body, _ := json.Marshal(map[string]any{
			"subject": cmd.Subject,
			"data": map[string]any{
				"sent_at": time.Now().UTC().Format(time.RFC3339Nano),
			},
		})

		req, err := http.NewRequest(http.MethodPost, cmd.URL+"/api/events", bytes.NewReader(body))
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nerror creating request: %v\n", err)
			errors++
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Slurpee-Secret-ID", cmd.SecretID)
		req.Header.Set("X-Slurpee-Secret", cmd.Secret)

		resp, err := client.Do(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nerror sending event: %v\n", err)
			errors++
			continue
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			fmt.Fprintf(os.Stderr, "\nunexpected status %d for event %d\n", resp.StatusCode, i+1)
			errors++
			continue
		}

		sent++
		fmt.Fprintf(os.Stderr, "\rSent: %d/%d  Errors: %d", sent, cmd.Count, errors)
	}

	elapsed := time.Since(start)
	actualRate := float64(sent) / elapsed.Seconds()
	fmt.Fprintf(os.Stderr, "\r%s\r", "                                                  ")
	fmt.Fprintf(os.Stderr, "Send complete: %d/%d sent, %d errors, %.1fs elapsed, %.1f events/sec\n",
		sent, cmd.Count, errors, elapsed.Seconds(), actualRate)
}

func runReceive(cmd *ReceiveCmd) {
	// Generate random suffix for subscriber name
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	suffix := make([]byte, 6)
	for i := range suffix {
		suffix[i] = letters[rand.Intn(len(letters))]
	}
	subscriberName := "slurpit-receiver-" + string(suffix)

	client := &http.Client{Timeout: 10 * time.Second}

	// Register subscriber
	regBody, _ := json.Marshal(map[string]any{
		"name":         subscriberName,
		"endpoint_url": cmd.EndpointURL,
		"auth_secret":  "slurpit-webhook-secret",
		"subscriptions": []map[string]any{
			{"subject_pattern": cmd.Subject},
		},
	})

	regReq, err := http.NewRequest(http.MethodPost, cmd.URL+"/api/subscribers", bytes.NewReader(regBody))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating registration request: %v\n", err)
		os.Exit(1)
	}
	regReq.Header.Set("Content-Type", "application/json")
	regReq.Header.Set("X-Slurpee-Admin-Secret", cmd.AdminSecret)

	regResp, err := client.Do(regReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error registering subscriber: %v\n", err)
		os.Exit(1)
	}
	defer regResp.Body.Close()

	if regResp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "subscriber registration failed with status %d\n", regResp.StatusCode)
		os.Exit(1)
	}

	var subResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(regResp.Body).Decode(&subResp); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding subscriber response: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Registered subscriber %s (ID: %s)\n", subscriberName, subResp.ID)

	// Track received events and latencies (thread-safe)
	var mu sync.Mutex
	var received int
	var latencies []time.Duration

	// Start webhook HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		receivedAt := time.Now()

		eventID := r.Header.Get("X-Event-ID")
		eventSubject := r.Header.Get("X-Event-Subject")

		if eventID == "" || eventSubject == "" {
			http.Error(w, "missing required headers", http.StatusBadRequest)
			return
		}

		// Parse body to extract sent_at for latency calculation
		body, err := io.ReadAll(r.Body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nwarning: failed to read event body: %v\n", err)
			mu.Lock()
			received++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		var payload struct {
			Data struct {
				SentAt string `json:"sent_at"`
			} `json:"data"`
		}

		mu.Lock()
		received++
		if err := json.Unmarshal(body, &payload); err != nil || payload.Data.SentAt == "" {
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nwarning: failed to parse event data: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "\nwarning: event missing sent_at field\n")
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		sentAt, err := time.Parse(time.RFC3339Nano, payload.Data.SentAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nwarning: failed to parse sent_at timestamp: %v\n", err)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		latency := receivedAt.Sub(sentAt)
		latencies = append(latencies, latency)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Addr:    cmd.Listen,
		Handler: mux,
	}

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "webhook server error: %v\n", err)
		}
	}()

	fmt.Fprintf(os.Stderr, "Listening on %s for %s...\n", cmd.Listen, cmd.Duration)

	// Live stats ticker
	start := time.Now()
	statsTicker := time.NewTicker(1 * time.Second)
	defer statsTicker.Stop()

	prevReceived := 0
	done := time.After(cmd.Duration)

statsLoop:
	for {
		select {
		case <-done:
			break statsLoop
		case <-statsTicker.C:
			mu.Lock()
			cur := received
			var minL, maxL, meanL time.Duration
			if len(latencies) > 0 {
				minL = latencies[0]
				maxL = latencies[0]
				var total time.Duration
				for _, l := range latencies {
					if l < minL {
						minL = l
					}
					if l > maxL {
						maxL = l
					}
					total += l
				}
				meanL = total / time.Duration(len(latencies))
			}
			mu.Unlock()

			rate := cur - prevReceived
			prevReceived = cur

			if len(latencies) > 0 {
				fmt.Fprintf(os.Stderr, "\rReceived: %d  Rate: %d/s  Latency min/max/mean: %.1f/%.1f/%.1f ms",
					cur, rate,
					float64(minL.Microseconds())/1000.0,
					float64(maxL.Microseconds())/1000.0,
					float64(meanL.Microseconds())/1000.0)
			} else {
				fmt.Fprintf(os.Stderr, "\rReceived: %d  Rate: %d/s  Latency: n/a", cur, rate)
			}
		}
	}

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(shutdownCtx)

	// Deregister subscriber
	delReq, err := http.NewRequest(http.MethodDelete, cmd.URL+"/api/subscribers/"+subResp.ID, nil)
	if err == nil {
		delReq.Header.Set("X-Slurpee-Admin-Secret", cmd.AdminSecret)
		delResp, err := client.Do(delReq)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to deregister subscriber: %v\n", err)
		} else {
			delResp.Body.Close()
			if delResp.StatusCode == http.StatusNoContent {
				fmt.Fprintf(os.Stderr, "Deregistered subscriber %s\n", subResp.ID)
			} else {
				fmt.Fprintf(os.Stderr, "warning: deregistration returned status %d\n", delResp.StatusCode)
			}
		}
	}

	// Final summary
	elapsed := time.Since(start)
	fmt.Fprintf(os.Stderr, "\r%s\r", "                                                                                    ")
	fmt.Fprintf(os.Stderr, "\n=== Receive Summary ===\n")
	fmt.Fprintf(os.Stderr, "  Total received : %d events\n", received)
	fmt.Fprintf(os.Stderr, "  Duration       : %.1fs\n", elapsed.Seconds())
	if elapsed.Seconds() > 0 {
		fmt.Fprintf(os.Stderr, "  Throughput     : %.1f events/sec\n", float64(received)/elapsed.Seconds())
	}

	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		var total time.Duration
		for _, l := range latencies {
			total += l
		}
		mean := total / time.Duration(len(latencies))
		minL := latencies[0]
		maxL := latencies[len(latencies)-1]
		p50 := latencies[len(latencies)*50/100]
		p95 := latencies[len(latencies)*95/100]
		p99 := latencies[len(latencies)*99/100]

		fmt.Fprintf(os.Stderr, "  Latency min    : %.1f ms\n", float64(minL.Microseconds())/1000.0)
		fmt.Fprintf(os.Stderr, "  Latency max    : %.1f ms\n", float64(maxL.Microseconds())/1000.0)
		fmt.Fprintf(os.Stderr, "  Latency mean   : %.1f ms\n", float64(mean.Microseconds())/1000.0)
		fmt.Fprintf(os.Stderr, "  Latency p50    : %.1f ms\n", float64(p50.Microseconds())/1000.0)
		fmt.Fprintf(os.Stderr, "  Latency p95    : %.1f ms\n", float64(p95.Microseconds())/1000.0)
		fmt.Fprintf(os.Stderr, "  Latency p99    : %.1f ms\n", float64(p99.Microseconds())/1000.0)
	} else {
		fmt.Fprintf(os.Stderr, "  Latency        : no data (no events with valid sent_at)\n")
	}
	fmt.Fprintf(os.Stderr, "=======================\n")
}
