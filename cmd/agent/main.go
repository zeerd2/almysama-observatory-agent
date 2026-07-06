package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/zeerd2/almysama-observatory-agent/internal/collector"
	"github.com/zeerd2/almysama-observatory-agent/internal/config"
	"github.com/zeerd2/almysama-observatory-agent/internal/reporter"
)

var version = "dev"

func main() {
	configPath := flag.String("config", "/etc/almysama-agent/config.json", "config file path")
	once := flag.Bool("once", false, "send one report and exit")
	showVersion := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if cfg.Server == "" {
		log.Fatal("server is required")
	}

	client := reporter.New(cfg)
	if cfg.AgentID == "" || cfg.AgentSecret == "" {
		if cfg.InstallToken == "" {
			log.Fatal("install_token is required for first enrollment")
		}
		identity := collector.CollectIdentity(cfg.Name, version)
		payload := map[string]interface{}{
			"install_token": cfg.InstallToken,
			"name":          identity.Name,
			"hostname":      identity.Hostname,
			"machine_id":    identity.MachineID,
			"os":            identity.OS,
			"arch":          identity.Arch,
			"kernel":        identity.Kernel,
			"agent_version": identity.AgentVersion,
		}
		resp, err := client.Enroll(payload)
		if err != nil {
			log.Fatalf("enroll: %v", err)
		}
		cfg.AgentID = resp.AgentID
		cfg.AgentSecret = resp.AgentSecret
		cfg.InstallToken = ""
		if err := config.Save(*configPath, cfg); err != nil {
			log.Fatalf("save enrolled config: %v", err)
		}
		log.Printf("enrolled as %s", cfg.AgentID)
	}

	client = reporter.New(cfg)
	if pulled, err := client.PullConfig(); err == nil {
		if pulled.ReportIntervalSeconds > 0 {
			cfg.ReportIntervalSeconds = pulled.ReportIntervalSeconds
		}
		if pulled.HeartbeatIntervalSeconds > 0 {
			cfg.HeartbeatIntervalSeconds = pulled.HeartbeatIntervalSeconds
		}
		_ = config.Save(*configPath, cfg)
	}

	if *once {
		report := collector.CollectReport(cfg.Name, version)
		if err := client.Report(report); err != nil {
			log.Fatalf("report: %v", err)
		}
		log.Print("report sent")
		return
	}

	runLoop(cfg, client, *configPath)
}

func runLoop(cfg *config.Config, client *reporter.Client, configPath string) {
	heartbeatTicker := time.NewTicker(time.Duration(cfg.HeartbeatIntervalSeconds) * time.Second)
	reportTicker := time.NewTicker(time.Duration(cfg.ReportIntervalSeconds) * time.Second)
	configTicker := time.NewTicker(10 * time.Minute)
	defer heartbeatTicker.Stop()
	defer reportTicker.Stop()
	defer configTicker.Stop()

	sendHeartbeat := func() {
		if err := client.Heartbeat(map[string]string{"time": time.Now().UTC().Format(time.RFC3339)}); err != nil {
			log.Printf("heartbeat failed: %v", err)
		}
	}
	sendReport := func() {
		report := collector.CollectReport(cfg.Name, version)
		if err := client.Report(report); err != nil {
			log.Printf("report failed: %v", err)
		}
	}

	sendHeartbeat()
	sendReport()

	for {
		select {
		case <-heartbeatTicker.C:
			sendHeartbeat()
		case <-reportTicker.C:
			sendReport()
		case <-configTicker.C:
			if pulled, err := client.PullConfig(); err == nil {
				changed := false
				if pulled.ReportIntervalSeconds > 0 && pulled.ReportIntervalSeconds != cfg.ReportIntervalSeconds {
					cfg.ReportIntervalSeconds = pulled.ReportIntervalSeconds
					reportTicker.Reset(time.Duration(cfg.ReportIntervalSeconds) * time.Second)
					changed = true
				}
				if pulled.HeartbeatIntervalSeconds > 0 && pulled.HeartbeatIntervalSeconds != cfg.HeartbeatIntervalSeconds {
					cfg.HeartbeatIntervalSeconds = pulled.HeartbeatIntervalSeconds
					heartbeatTicker.Reset(time.Duration(cfg.HeartbeatIntervalSeconds) * time.Second)
					changed = true
				}
				if changed {
					_ = config.Save(configPath, cfg)
				}
			}
		}
	}
}

func init() {
	log.SetFlags(log.LstdFlags | log.LUTC | log.Lmsgprefix)
	log.SetPrefix("almysama-agent ")
	log.SetOutput(os.Stdout)
}
