package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AppRoom           string   `yaml:"app_room"`
	RelayListen       string   `yaml:"relay_listen"`
	RelayAddr         string   `yaml:"relay_addr"`
	EnableRelayClient bool     `yaml:"enable_relay_client"`
	EnableHolePunch   bool     `yaml:"enable_holepunch"`
	EnableUPnP        bool     `yaml:"enable_upnp"`
	AnnounceAddrs     []string `yaml:"announce_addrs"`
}

func loadConfig() Config {
	var cfg Config
	if b, err := os.ReadFile("config.yaml"); err == nil {
		_ = yaml.Unmarshal(b, &cfg)
	}
	return cfg
}
