package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kardianos/service"

	"sentinel/agent/internal/buffer"
	"sentinel/agent/internal/collector"
	"sentinel/agent/internal/config"
	"sentinel/agent/internal/transport"
)

var svcConfig = &service.Config{
	Name:        "sentinel-agent",
	DisplayName: "Sentinel Monitoring Agent",
	Description: "Collects CPU, memory, disk, and GPU metrics and reports them to a Sentinel monitoring server.",
}

type program struct {
	quit chan struct{}
}

func (p *program) Start(s service.Service) error {
	p.quit = make(chan struct{})
	go p.run()
	return nil
}

func (p *program) Stop(s service.Service) error {
	close(p.quit)
	return nil
}

func (p *program) run() {
	cfg, err := config.Load(config.Path())
	if err != nil {
		log.Fatalf("load config (run 'sentinel-agent enroll' first): %v", err)
	}

	client := transport.New(cfg.ServerURL, cfg.APIKey, cfg.InsecureSkipTLS)
	buf := buffer.New()
	coll := collector.New()

	ticker := time.NewTicker(time.Duration(cfg.IntervalSecs) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-p.quit:
			return
		case <-ticker.C:
			buf.Add(coll.CollectAll())
			if buf.IsEmpty() {
				continue
			}
			if err := client.Push(buf.Snapshot()); err != nil {
				log.Printf("push failed, will retry next tick: %v", err)
				continue
			}
			buf.Clear()
		}
	}
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "enroll":
			runEnroll(os.Args[2:])
			return
		case "install", "uninstall", "start", "stop", "restart":
			runServiceControl(os.Args[1])
			return
		}
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatalf("service init: %v", err)
	}
	if err := s.Run(); err != nil {
		log.Fatalf("service run: %v", err)
	}
}

func runEnroll(args []string) {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	server := fs.String("server", "", "Sentinel server base URL, e.g. https://observe.kanomcakey.com:4000")
	token := fs.String("token", "", "one-time enrollment token from the Hosts page")
	insecure := fs.Bool("insecure-skip-tls-verify", false, "skip TLS certificate verification (testing only)")
	fs.Parse(args)

	if *server == "" || *token == "" {
		fmt.Println("usage: sentinel-agent enroll --server <url> --token <token>")
		os.Exit(1)
	}

	hostID, apiKey, err := transport.Enroll(*server, *token, *insecure)
	if err != nil {
		log.Fatalf("enrollment failed: %v", err)
	}

	cfg := &config.Config{
		ServerURL:       *server,
		HostID:          hostID,
		APIKey:          apiKey,
		IntervalSecs:    10,
		InsecureSkipTLS: *insecure,
	}
	if err := config.Save(config.Path(), cfg); err != nil {
		log.Fatalf("failed to save config to %s: %v", config.Path(), err)
	}

	fmt.Printf("enrolled successfully as host %s\nconfig written to %s\n", hostID, config.Path())
	fmt.Println("next: sentinel-agent install   (registers and starts the background service)")
}

func runServiceControl(action string) {
	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatalf("service init: %v", err)
	}
	if err := service.Control(s, action); err != nil {
		log.Fatalf("%s failed: %v", action, err)
	}
	fmt.Printf("service %s: ok\n", action)
}
