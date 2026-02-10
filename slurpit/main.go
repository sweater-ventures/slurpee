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
	"sync/atomic"
	"time"

	"github.com/alexflint/go-arg"
)

type SendCmd struct {
	URL      string `arg:"--url,required" help:"Slurpee base URL"`
	SecretID string `arg:"--secret-id,required" help:"API secret UUID"`
	Secret   string `arg:"--secret,required" help:"API secret value"`
	Subject  string `arg:"--subject" default:"loadtest.event" help:"Event subject"`
	Rate     int    `arg:"--rate" default:"10" help:"Events per second"`
	Count    int    `arg:"--count" default:"100" help:"Total events to send"`
	Workers  int    `arg:"--workers" default:"1" help:"Number of concurrent sender goroutines"`
}

type ReceiveCmd struct {
	URL         string        `arg:"--url,required" help:"Slurpee base URL"`
	AdminSecret string        `arg:"--admin-secret,required" help:"Admin secret for subscriber registration"`
	Listen      string        `arg:"--listen" default:":9090" help:"Local listen address"`
	EndpointURL string        `arg:"--endpoint-url,required" help:"Publicly reachable URL for this receiver"`
	Subject     string        `arg:"--subject" default:"loadtest.*" help:"Subject pattern to subscribe to"`
	Duration    time.Duration `arg:"--duration" default:"30s" help:"How long to listen"`
}

type BenchCmd struct {
	URL              string        `arg:"--url,required" help:"Slurpee base URL"`
	SecretID         string        `arg:"--secret-id,required" help:"API secret UUID"`
	Secret           string        `arg:"--secret,required" help:"API secret value"`
	AdminSecret      string        `arg:"--admin-secret,required" help:"Admin secret for subscriber registration"`
	Listen           string        `arg:"--listen" default:":9090" help:"Local listen address for webhook receiver"`
	EndpointURL      string        `arg:"--endpoint-url,required" help:"Publicly reachable URL for the receiver"`
	Subject          string        `arg:"--subject" default:"loadtest.event" help:"Event subject to send"`
	SubscribePattern string        `arg:"--subscribe-pattern" default:"loadtest.*" help:"Subject pattern for subscriber"`
	Rate             int           `arg:"--rate" default:"10" help:"Events per second"`
	Count            int           `arg:"--count" default:"100" help:"Total events to send"`
	Workers          int           `arg:"--workers" default:"1" help:"Number of concurrent sender goroutines"`
	Drain            time.Duration `arg:"--drain" default:"5s" help:"Time to wait after sending for remaining events"`
}

type args struct {
	Send    *SendCmd    `arg:"subcommand:send" help:"Send events to Slurpee"`
	Receive *ReceiveCmd `arg:"subcommand:receive" help:"Receive events from Slurpee and measure latency"`
	Bench   *BenchCmd   `arg:"subcommand:bench" help:"Run sender and receiver together as a full benchmark"`
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
	case a.Bench != nil:
		runBench(a.Bench)
	default:
		p.WriteUsage(os.Stdout)
		fmt.Println()
		p.WriteHelp(os.Stdout)
		os.Exit(1)
	}
}

func runSend(cmd *SendCmd) {
	if cmd.Workers < 1 {
		cmd.Workers = 1
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: cmd.Workers,
			MaxConnsPerHost:     cmd.Workers,
		},
	}
	interval := time.Second / time.Duration(cmd.Rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var sent, errors int64
	start := time.Now()

	// Work channel — the main goroutine rate-limits by sending tokens
	work := make(chan int, cmd.Workers)

	var wg sync.WaitGroup
	for w := 0; w < cmd.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				body, _ := json.Marshal(map[string]any{
					"subject": cmd.Subject,
					"data": map[string]any{
						"sent_at": time.Now().UTC().Format(time.RFC3339Nano),
					},
				})

				req, err := http.NewRequest(http.MethodPost, cmd.URL+"/api/events", bytes.NewReader(body))
				if err != nil {
					fmt.Fprintf(os.Stderr, "\nerror creating request: %v\n", err)
					atomic.AddInt64(&errors, 1)
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Slurpee-Secret-ID", cmd.SecretID)
				req.Header.Set("X-Slurpee-Secret", cmd.Secret)

				resp, err := client.Do(req)
				if err != nil {
					fmt.Fprintf(os.Stderr, "\nerror sending event: %v\n", err)
					atomic.AddInt64(&errors, 1)
					continue
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusCreated {
					fmt.Fprintf(os.Stderr, "\nunexpected status %d for event %d\n", resp.StatusCode, i+1)
					atomic.AddInt64(&errors, 1)
					continue
				}

				s := atomic.AddInt64(&sent, 1)
				e := atomic.LoadInt64(&errors)
				fmt.Fprintf(os.Stderr, "\rSent: %d/%d  Errors: %d  Workers: %d", s, cmd.Count, e, cmd.Workers)
			}
		}()
	}

	// Rate-limited dispatch
	for i := 0; i < cmd.Count; i++ {
		<-ticker.C
		work <- i
	}
	close(work)
	wg.Wait()

	elapsed := time.Since(start)
	actualRate := float64(sent) / elapsed.Seconds()
	fmt.Fprintf(os.Stderr, "\r%s\r", "                                                  ")
	fmt.Fprintf(os.Stderr, "Send complete: %d/%d sent, %d errors, %.1fs elapsed, %.1f events/sec\n",
		sent, cmd.Count, errors, elapsed.Seconds(), actualRate)
}

func runBench(cmd *BenchCmd) {
	if cmd.Workers < 1 {
		cmd.Workers = 1
	}

	// --- 1. Start webhook receiver ---
	var mu sync.Mutex
	var received int
	var latencies []time.Duration

	mux := http.NewServeMux()
	mux.HandleFunc("POST /", func(w http.ResponseWriter, r *http.Request) {
		receivedAt := time.Now()

		eventID := r.Header.Get("X-Event-ID")
		eventSubject := r.Header.Get("X-Event-Subject")
		if eventID == "" || eventSubject == "" {
			http.Error(w, "missing required headers", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			mu.Lock()
			received++
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		var payload struct {
			SentAt string `json:"sent_at"`
		}

		mu.Lock()
		received++
		if err := json.Unmarshal(body, &payload); err != nil || payload.SentAt == "" {
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		sentAt, err := time.Parse(time.RFC3339Nano, payload.SentAt)
		if err != nil {
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		latencies = append(latencies, receivedAt.Sub(sentAt))
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{Addr: cmd.Listen, Handler: mux}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "webhook server error: %v\n", err)
		}
	}()

	fmt.Fprintf(os.Stderr, "Receiver listening on %s\n", cmd.Listen)

	// --- 2. Register subscriber ---
	apiClient := &http.Client{Timeout: 10 * time.Second}

	regBody, _ := json.Marshal(map[string]any{
		"name":         "slurpit-bench-" + randomSuffix(6),
		"endpoint_url": cmd.EndpointURL,
		"auth_secret":  "slurpit-webhook-secret",
		"subscriptions": []map[string]any{
			{"subject_pattern": cmd.SubscribePattern},
		},
	})

	regReq, err := http.NewRequest(http.MethodPost, cmd.URL+"/api/subscribers", bytes.NewReader(regBody))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating registration request: %v\n", err)
		os.Exit(1)
	}
	regReq.Header.Set("Content-Type", "application/json")
	regReq.Header.Set("X-Slurpee-Admin-Secret", cmd.AdminSecret)

	regResp, err := apiClient.Do(regReq)
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

	fmt.Fprintf(os.Stderr, "Registered subscriber (ID: %s)\n", subResp.ID)

	// Small delay to let the subscription propagate
	time.Sleep(500 * time.Millisecond)

	// --- 3. Run sender ---
	sendClient := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConnsPerHost: cmd.Workers,
			MaxConnsPerHost:     cmd.Workers,
		},
	}

	interval := time.Second / time.Duration(cmd.Rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var sent, sendErrors int64
	sendStart := time.Now()

	work := make(chan int, cmd.Workers)
	var wg sync.WaitGroup
	for w := 0; w < cmd.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				body, _ := json.Marshal(map[string]any{
					"subject": cmd.Subject,
					"data": map[string]any{
						"sent_at": time.Now().UTC().Format(time.RFC3339Nano),
					},
				})

				req, err := http.NewRequest(http.MethodPost, cmd.URL+"/api/events", bytes.NewReader(body))
				if err != nil {
					atomic.AddInt64(&sendErrors, 1)
					continue
				}
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Slurpee-Secret-ID", cmd.SecretID)
				req.Header.Set("X-Slurpee-Secret", cmd.Secret)

				resp, err := sendClient.Do(req)
				if err != nil {
					atomic.AddInt64(&sendErrors, 1)
					continue
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusCreated {
					fmt.Fprintf(os.Stderr, "\nunexpected status %d for event %d\n", resp.StatusCode, i+1)
					atomic.AddInt64(&sendErrors, 1)
					continue
				}

				s := atomic.AddInt64(&sent, 1)
				e := atomic.LoadInt64(&sendErrors)
				mu.Lock()
				r := received
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "\rSent: %d/%d  Errors: %d  Received: %d  Workers: %d",
					s, cmd.Count, e, r, cmd.Workers)
			}
		}()
	}

	for i := 0; i < cmd.Count; i++ {
		<-ticker.C
		work <- i
	}
	close(work)
	wg.Wait()

	sendElapsed := time.Since(sendStart)
	fmt.Fprintf(os.Stderr, "\r%s\r", "                                                                        ")
	fmt.Fprintf(os.Stderr, "Send complete: %d/%d sent, %d errors, %.1fs, %.1f events/sec\n",
		sent, cmd.Count, sendErrors, sendElapsed.Seconds(), float64(sent)/sendElapsed.Seconds())

	// --- 4. Drain: wait for remaining events ---
	fmt.Fprintf(os.Stderr, "Draining for %s...\n", cmd.Drain)
	drainDeadline := time.After(cmd.Drain)
	drainTicker := time.NewTicker(500 * time.Millisecond)
	defer drainTicker.Stop()

drainLoop:
	for {
		select {
		case <-drainDeadline:
			break drainLoop
		case <-drainTicker.C:
			mu.Lock()
			r := received
			mu.Unlock()
			fmt.Fprintf(os.Stderr, "\rDraining... Received: %d/%d", r, sent)
			if int64(r) >= sent {
				break drainLoop
			}
		}
	}

	// --- 5. Shutdown ---
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(shutdownCtx)

	// Deregister subscriber
	delReq, err := http.NewRequest(http.MethodDelete, cmd.URL+"/api/subscribers/"+subResp.ID, nil)
	if err == nil {
		delReq.Header.Set("X-Slurpee-Admin-Secret", cmd.AdminSecret)
		if delResp, err := apiClient.Do(delReq); err == nil {
			delResp.Body.Close()
		}
	}

	// --- 6. Combined summary ---
	totalElapsed := time.Since(sendStart)
	fmt.Fprintf(os.Stderr, "\r%s\r", "                                                                        ")
	fmt.Fprintf(os.Stderr, "\n=== Bench Summary ===\n")
	fmt.Fprintf(os.Stderr, "  Sent           : %d/%d events (%d errors)\n", sent, cmd.Count, sendErrors)
	fmt.Fprintf(os.Stderr, "  Send rate      : %.1f events/sec\n", float64(sent)/sendElapsed.Seconds())
	fmt.Fprintf(os.Stderr, "  Received       : %d events\n", received)
	if sent > 0 {
		fmt.Fprintf(os.Stderr, "  Delivery       : %.1f%%\n", float64(received)/float64(sent)*100)
	}
	fmt.Fprintf(os.Stderr, "  Total duration : %.1fs\n", totalElapsed.Seconds())

	if len(latencies) > 0 {
		sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
		var total time.Duration
		for _, l := range latencies {
			total += l
		}
		mean := total / time.Duration(len(latencies))
		p50 := latencies[len(latencies)*50/100]
		p95 := latencies[len(latencies)*95/100]
		p99 := latencies[len(latencies)*99/100]

		fmt.Fprintf(os.Stderr, "  Latency min    : %.1f ms\n", float64(latencies[0].Microseconds())/1000.0)
		fmt.Fprintf(os.Stderr, "  Latency max    : %.1f ms\n", float64(latencies[len(latencies)-1].Microseconds())/1000.0)
		fmt.Fprintf(os.Stderr, "  Latency mean   : %.1f ms\n", float64(mean.Microseconds())/1000.0)
		fmt.Fprintf(os.Stderr, "  Latency p50    : %.1f ms\n", float64(p50.Microseconds())/1000.0)
		fmt.Fprintf(os.Stderr, "  Latency p95    : %.1f ms\n", float64(p95.Microseconds())/1000.0)
		fmt.Fprintf(os.Stderr, "  Latency p99    : %.1f ms\n", float64(p99.Microseconds())/1000.0)
	} else {
		fmt.Fprintf(os.Stderr, "  Latency        : no data\n")
	}
	fmt.Fprintf(os.Stderr, "=====================\n")
}

func randomSuffix(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
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
			SentAt string `json:"sent_at"`
		}

		mu.Lock()
		received++
		if err := json.Unmarshal(body, &payload); err != nil || payload.SentAt == "" {
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nwarning: failed to parse event data: %v\n", err)
			} else {
				fmt.Fprintf(os.Stderr, "\nwarning: event missing sent_at field\n")
			}
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}

		sentAt, err := time.Parse(time.RFC3339Nano, payload.SentAt)
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
