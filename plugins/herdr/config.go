package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"herdr-console/herdr"
)

// config is the Plugin's configuration: one JSON object, not secret,
// changed only while the Plugin is disabled. Unknown keys are refused.
type config struct {
	// HerdrBin is the herdr binary: a name on PATH or a path. Default
	// "herdr", then ~/.local/bin/herdr.
	HerdrBin string `json:"herdr_bin"`
	// HerdrSocket is the herdr server's API socket. Default
	// ~/.config/herdr/herdr.sock.
	HerdrSocket string `json:"herdr_socket"`
	// Crew are session names (herdr name or terminal title, any case) that
	// always need the two-step confirm, even when the console opened them.
	// The console also refuses to start a session under one of these names.
	// Default: none.
	Crew []string `json:"crew"`
	// ConsoleWorkspace is the herdr workspace label for sessions the
	// console opens.
	ConsoleWorkspace string `json:"console_workspace"`
}

func defaultConfig() config {
	home := herdr.Home()
	return config{
		HerdrBin:         "herdr",
		HerdrSocket:      filepath.Join(home, ".config", "herdr", "herdr.sock"),
		Crew:             []string{},
		ConsoleWorkspace: "Console",
	}
}

func loadConfig(path string) (config, error) {
	cfg := defaultConfig()
	if path != "" {
		raw, err := os.ReadFile(path)
		switch {
		case os.IsNotExist(err):
		case err != nil:
			return cfg, err
		default:
			dec := json.NewDecoder(bytes.NewReader(raw))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&cfg); err != nil && err != io.EOF {
				return cfg, fmt.Errorf("refused: %w", err)
			}
		}
	}
	if strings.TrimSpace(cfg.ConsoleWorkspace) == "" {
		return cfg, fmt.Errorf("refused: console_workspace must not be empty")
	}
	if cfg.Crew == nil {
		cfg.Crew = []string{}
	}
	if strings.HasPrefix(cfg.HerdrSocket, "~/") {
		cfg.HerdrSocket = filepath.Join(herdr.Home(), cfg.HerdrSocket[2:])
	}
	if cfg.HerdrSocket == "" || !filepath.IsAbs(cfg.HerdrSocket) {
		return cfg, fmt.Errorf("refused: herdr_socket must be an absolute path")
	}
	cfg.HerdrBin = herdr.FindBin(cfg.HerdrBin)
	return cfg, nil
}
