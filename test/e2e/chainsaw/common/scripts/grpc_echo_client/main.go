package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	pb "github.com/moul/pb/grpcbin/go-grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type config struct {
	ProxyIP     string
	ProxyPort   int
	Host        string
	Message     string
	UseTLS      bool
	InsecureTLS bool
	MaxRetries  int
	RetryDelay  time.Duration
	CallTimeout time.Duration
	DialTimeout time.Duration
}

type output struct {
	Success      bool   `json:"success"`
	Error        string `json:"error,omitempty"`
	Address      string `json:"address"`
	Host         string `json:"host"`
	ProxyIP      string `json:"proxy_ip"`
	ProxyPort    int    `json:"proxy_port"`
	Message      string `json:"message"`
	Response     string `json:"response,omitempty"`
	RetryAttempt int    `json:"retry_attempt"`
	MaxRetries   int    `json:"max_retries"`
	UseTLS       bool   `json:"use_tls"`
	InsecureTLS  bool   `json:"insecure_tls"`
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		writeOutput(output{Success: false, Error: err.Error()})
		os.Exit(1)
	}

	address := fmt.Sprintf("%s:%d", cfg.ProxyIP, cfg.ProxyPort)
	lastErr := ""

	for attempt := range cfg.MaxRetries {
		attempt++
		response, err := callDummyUnary(cfg, address)
		if err == nil {
			writeOutput(output{
				Success:      true,
				Address:      address,
				Host:         cfg.Host,
				ProxyIP:      cfg.ProxyIP,
				ProxyPort:    cfg.ProxyPort,
				Message:      cfg.Message,
				Response:     response,
				RetryAttempt: attempt,
				MaxRetries:   cfg.MaxRetries,
				UseTLS:       cfg.UseTLS,
				InsecureTLS:  cfg.InsecureTLS,
			})
			return
		}

		lastErr = err.Error()
		if attempt < cfg.MaxRetries {
			time.Sleep(cfg.RetryDelay)
		}
	}

	writeOutput(output{
		Success:      false,
		Error:        lastErr,
		Address:      address,
		Host:         cfg.Host,
		ProxyIP:      cfg.ProxyIP,
		ProxyPort:    cfg.ProxyPort,
		Message:      cfg.Message,
		RetryAttempt: cfg.MaxRetries,
		MaxRetries:   cfg.MaxRetries,
		UseTLS:       cfg.UseTLS,
		InsecureTLS:  cfg.InsecureTLS,
	})
	os.Exit(1)
}

func loadConfig() (config, error) {
	proxyIP := os.Getenv("PROXY_IP")
	if proxyIP == "" {
		return config{}, fmt.Errorf("PROXY_IP is required")
	}

	host := os.Getenv("GRPC_HOST")
	if host == "" {
		return config{}, fmt.Errorf("GRPC_HOST is required")
	}

	proxyPort, err := getenvInt("PROXY_PORT", 443)
	if err != nil {
		return config{}, err
	}
	maxRetries, err := getenvInt("MAX_RETRIES", 180)
	if err != nil {
		return config{}, err
	}
	retryDelaySeconds, err := getenvInt("RETRY_DELAY", 1)
	if err != nil {
		return config{}, err
	}
	callTimeoutSeconds, err := getenvInt("CALL_TIMEOUT", 5)
	if err != nil {
		return config{}, err
	}
	dialTimeoutSeconds, err := getenvInt("DIAL_TIMEOUT", 5)
	if err != nil {
		return config{}, err
	}
	useTLS, err := getenvBool("USE_TLS", true)
	if err != nil {
		return config{}, err
	}
	insecureTLS, err := getenvBool("INSECURE_TLS", true)
	if err != nil {
		return config{}, err
	}

	return config{
		ProxyIP:     proxyIP,
		ProxyPort:   proxyPort,
		Host:        host,
		Message:     getenvOrDefault("REQUEST_MESSAGE", "kong"),
		UseTLS:      useTLS,
		InsecureTLS: insecureTLS,
		MaxRetries:  maxRetries,
		RetryDelay:  time.Duration(retryDelaySeconds) * time.Second,
		CallTimeout: time.Duration(callTimeoutSeconds) * time.Second,
		DialTimeout: time.Duration(dialTimeoutSeconds) * time.Second,
	}, nil
}

func callDummyUnary(cfg config, address string) (string, error) {
	clientOpts := []grpc.DialOption{grpc.WithAuthority(cfg.Host)}
	if cfg.UseTLS {
		clientOpts = append(clientOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			MinVersion:         tls.VersionTLS12,
			ServerName:         cfg.Host,
			//nolint:gosec // E2E test helper intentionally accepts the self-signed certificate generated during the test.
			InsecureSkipVerify: cfg.InsecureTLS,
		})))
	} else {
		clientOpts = append(clientOpts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(address, clientOpts...)
	if err != nil {
		return "", fmt.Errorf("failed to dial GRPC server: %w", err)
	}
	defer func() {
		_ = conn.Close()
	}()

	callCtx, cancelCall := context.WithTimeout(context.Background(), cfg.CallTimeout)
	defer cancelCall()

	resp, err := pb.NewGRPCBinClient(conn).DummyUnary(callCtx, &pb.DummyMessage{FString: cfg.Message})
	if err != nil {
		return "", fmt.Errorf("failed to send GRPC request: %w", err)
	}
	if resp.GetFString() != cfg.Message {
		return "", fmt.Errorf("unexpected response from GRPC server: %s", resp.GetFString())
	}

	return resp.GetFString(), nil
}

func getenvInt(key string, defaultValue int) (int, error) {
	value := getenvOrDefault(key, strconv.Itoa(defaultValue))
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}

func getenvBool(key string, defaultValue bool) (bool, error) {
	value := getenvOrDefault(key, strconv.FormatBool(defaultValue))
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %w", key, err)
	}
	return parsed, nil
}

func getenvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func writeOutput(out output) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(out)
}




