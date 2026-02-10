package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	fmt.Println("receive mode not yet implemented")
}
