package main

import (
	"fmt"
	"io/ioutil"
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

	rootCmd.Flags().StringVarP(&proxyURLs, "proxies", "p", "", "Comma-separated list of proxy URLs")
	rootCmd.Flags().StringVarP(&targetURL, "url", "u", "http://ip-api.com/json", "Target URL to request")
	rootCmd.Flags().IntVarP(&maxRetries, "retries", "r", 2, "Maximum number of retries per request")
	rootCmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	rootCmd.Flags().DurationVarP(&proxyTimeout, "timeout", "t", 5*time.Second, "Timeout for each proxy attempt")
	rootCmd.Flags().DurationVarP(&proxyFailureDuration, "failure-duration", "f", 30*time.Second, "How long to mark a proxy as failed")
	rootCmd.Flags().IntVarP(&repeat, "repeat", "n", 1, "Number of times to repeat the request")

	rootCmd.MarkFlagRequired("proxies")

	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run() error {
	proxyList := strings.Split(proxyURLs, ",")

	transport, err := proxycycle.New(proxycycle.Options{
		ProxyURLs:            proxyList,
		MaxRetries:           maxRetries,
		Verbose:              verbose,
		ProxyTimeout:         proxyTimeout,
		ProxyFailureDuration: proxyFailureDuration,
	})
	if err != nil {
		return fmt.Errorf("error creating transport: %v", err)
	}

	client := &http.Client{
		Transport: transport,
	}

	for i := 0; i < repeat; i++ {
		resp, err := client.Get(targetURL)
		if err != nil {
			return fmt.Errorf("error making request: %v", err)
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("error reading response: %v", err)
		}

		fmt.Printf("Response %d: %s\n", i+1, string(body))
	}

	return nil
}
