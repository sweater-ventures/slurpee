package main

import (
	"fmt"
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
	fmt.Println("send mode not yet implemented")
}

func runReceive(cmd *ReceiveCmd) {
	fmt.Println("receive mode not yet implemented")
}
