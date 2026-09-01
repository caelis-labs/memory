package steward

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const maxProviderConfigBytes = 128 << 10

// Config is owner-controlled process configuration. It is distinct from
// durable profile policy so provider credentials never enter SQLite.
type Config struct {
	Workers      int              `json:"workers"`
	LeaseSeconds int              `json:"lease_seconds"`
	PollMS       int              `json:"poll_ms"`
	RetryBaseMS  int              `json:"retry_base_ms"`
	MaxAttempts  int              `json:"max_attempts"`
	Providers    []ProviderConfig `json:"providers"`
}

// ProviderConfig defines one fixed HTTP provider route.
type ProviderConfig struct {
	Ref            string `json:"ref"`
	Endpoint       string `json:"endpoint"`
	CredentialFile string `json:"credential_file,omitempty"`
	TimeoutMS      int    `json:"timeout_ms"`
}

// LoadConfig reads strict owner-only configuration and all optional credential
// files, returning a ready immutable provider set.
func LoadConfig(path string) (Config, map[string]Provider, Options, error) {
	value, err := readOwnerOnlyFile(path, "Steward provider configuration", maxProviderConfigBytes)
	if err != nil {
		return Config{}, nil, Options{}, err
	}
	var config Config
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, nil, Options{}, fmt.Errorf("decode Steward provider configuration: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Config{}, nil, Options{}, fmt.Errorf("Steward provider configuration has trailing content")
	}
	options := Options{
		Workers: config.Workers, LeaseDuration: time.Duration(config.LeaseSeconds) * time.Second,
		PollInterval: time.Duration(config.PollMS) * time.Millisecond,
		RetryBase:    time.Duration(config.RetryBaseMS) * time.Millisecond, MaxAttempts: config.MaxAttempts,
	}
	if err := options.Validate(); err != nil {
		return Config{}, nil, Options{}, err
	}
	if len(config.Providers) == 0 || len(config.Providers) > 64 {
		return Config{}, nil, Options{}, fmt.Errorf("Steward providers must contain 1..64 entries")
	}
	providers := make(map[string]Provider, len(config.Providers))
	for _, providerConfig := range config.Providers {
		if !validProviderReference(providerConfig.Ref) {
			return Config{}, nil, Options{}, fmt.Errorf("Steward provider reference is invalid")
		}
		if _, exists := providers[providerConfig.Ref]; exists {
			return Config{}, nil, Options{}, fmt.Errorf("duplicate Steward provider reference")
		}
		credential := ""
		if providerConfig.CredentialFile != "" {
			secret, err := readOwnerOnlyFile(providerConfig.CredentialFile, "Steward provider credential", 16<<10)
			if err != nil {
				return Config{}, nil, Options{}, err
			}
			credential = strings.TrimSpace(string(secret))
			if credential == "" || strings.ContainsAny(credential, "\r\n") {
				return Config{}, nil, Options{}, fmt.Errorf("Steward provider credential is invalid")
			}
		}
		provider, err := NewHTTPProvider(providerConfig.Endpoint, credential, time.Duration(providerConfig.TimeoutMS)*time.Millisecond)
		if err != nil {
			return Config{}, nil, Options{}, err
		}
		providers[providerConfig.Ref] = provider
	}
	return config, providers, options, nil
}

func validProviderReference(value string) bool {
	if value == "" || len(value) > 256 || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '_' && char != '-' && char != '.' {
			return false
		}
	}
	return true
}
