package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nguyenvanduocit/proxycycle"
	"github.com/spf13/cobra"
)

var (
	proxyURLs            string
	targetURL            string
	maxRetries           int
	verbose              bool
	proxyTimeout         time.Duration
	proxyFailureDuration time.Duration
	repeat               int
	maxIdleConnsPerHost  int
	minRetryBackoff      time.Duration
	maxRetryBackoff      time.Duration
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "proxycycle",
		Short: "ProxyCycle is a tool for making HTTP requests through rotating proxies",
		Long: `ProxyCycle allows you to make HTTP requests through a list of rotating proxies,
with automatic retry and failure handling capabilities.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run()
		},
	}

	rootCmd.Flags().StringVarP(&proxyURLs, "proxies", "p", "", "Comma-separated list of proxy URLs (with optional auth: http://user:pass@host:port)")
	rootCmd.Flags().StringVarP(&targetURL, "url", "u", "http://ip-api.com/json", "Target URL to request")
	rootCmd.Flags().IntVarP(&maxRetries, "retries", "r", 2, "Maximum number of retries per request")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	rootCmd.Flags().DurationVarP(&proxyTimeout, "timeout", "t", 5*time.Second, "Timeout for each proxy attempt")
	rootCmd.Flags().DurationVarP(&proxyFailureDuration, "failure-duration", "f", 30*time.Second, "How long to mark a proxy as failed")
	rootCmd.Flags().IntVarP(&repeat, "repeat", "n", 1, "Number of times to repeat the request")

	rootCmd.Flags().IntVar(&maxIdleConnsPerHost, "max-idle-conns", 10, "Maximum number of idle connections per host")
	rootCmd.Flags().DurationVar(&minRetryBackoff, "min-backoff", 100*time.Millisecond, "Minimum retry backoff duration")
	rootCmd.Flags().DurationVar(&maxRetryBackoff, "max-backoff", 10*time.Second, "Maximum retry backoff duration")

	rootCmd.MarkFlagRequired("proxies")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	var logHandler slog.Handler
	if verbose {
		logHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
	} else {
		logHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}
	logger := slog.New(logHandler)

	proxyList := strings.Split(proxyURLs, ",")

	transport, err := proxycycle.New(proxycycle.Options{
		ProxyURLs:            proxyList,
		MaxRetries:           maxRetries,
		Logger:               logger,
		ProxyTimeout:         proxyTimeout,
		ProxyFailureDuration: proxyFailureDuration,
		MaxIdleConnsPerHost:  maxIdleConnsPerHost,
		MinRetryBackoff:      minRetryBackoff,
		MaxRetryBackoff:      maxRetryBackoff,
	})
	if err != nil {
		return fmt.Errorf("error creating transport: %v", err)
	}

	client := &http.Client{
		Transport: transport,
	}

	for i := 0; i < repeat; i++ {
		logger.Info("making request", "attempt", i+1, "target", targetURL)

		resp, err := client.Get(targetURL)
		if err != nil {
			return fmt.Errorf("error making request: %v", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("error reading response: %v", err)
		}

		logger.Info("request completed",
			"attempt", i+1,
			"status", resp.StatusCode,
			"body_length", len(body),
		)

		if verbose {
			fmt.Printf("Response %d: %s\n", i+1, string(body))
		}
	}

	return nil
}
