// Package proxycycle provides a resilient HTTP transport with proxy rotation capabilities.
// It implements the http.RoundTripper interface and supports automatic failover
// between multiple proxy servers.
package proxycycle

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"sync"
	"syscall"
	"time"
)

// Common errors returned by ProxyCycle.
var (
	ErrNoProxyProvided = errors.New("proxycycle: at least one proxy URL is required")
	ErrInvalidProxy    = errors.New("proxycycle: invalid proxy URL")
)

// Options contains configuration for ProxyCycle transport.
// A zero Options value is valid and results in a transport using default values.
type Options struct {
	// ProxyURLs specifies the list of proxy URLs to cycle through.
	// Each URL must include a scheme and host.
	// Supported schemes are: http, https, socks5.
	// The ?insecure=1 query parameter can be used to skip TLS verification.
	ProxyURLs []string

	// MaxRetries specifies the maximum number of retry attempts per request.
	// If zero or negative, defaults to 3.
	MaxRetries int

	// ProxyTimeout specifies the timeout for each proxy attempt.
	// If zero, defaults to 30 seconds.
	ProxyTimeout time.Duration

	// RetryBackoff specifies the delay between retry attempts.
	// If zero, defaults to 2 seconds.
	RetryBackoff time.Duration

	// ProxyFailureDuration specifies how long to mark a proxy as failed.
	// If zero, defaults to 3 seconds.
	ProxyFailureDuration time.Duration

	// Verbose enables detailed logging of proxy operations.
	// By default, logging is disabled.
	Verbose bool

	// Logger is a structured logger for the transport
	Logger *slog.Logger

	// MaxIdleConnsPerHost specifies the max idle connections per host
	MaxIdleConnsPerHost int

	// MinRetryBackoff specifies the minimum backoff duration
	MinRetryBackoff time.Duration

	// MaxRetryBackoff specifies the maximum backoff duration
	MaxRetryBackoff time.Duration
}

// Transport implements http.RoundTripper interface with proxy rotation capabilities.
// It provides automatic failover between multiple proxy servers and handles
// temporary failures gracefully.
type Transport struct {
	options            Options
	current            int
	mu                 sync.RWMutex // protects following fields
	proxies            []*url.URL
	base               http.Transport
	failedProxies      map[string]time.Time
	verbose            bool
	insecureSkipVerify bool
	logger             *slog.Logger
}

// New creates a new ProxyCycle transport with given options.
// It returns an error if no proxy URLs are provided or if any proxy URL is invalid.
func New(opts Options) (*Transport, error) {
	if len(opts.ProxyURLs) == 0 {
		return nil, ErrNoProxyProvided
	}

	// Parse and validate proxy URLs
	proxies := make([]*url.URL, 0, len(opts.ProxyURLs))
	var insecureSkipVerify bool

	for _, rawURL := range opts.ProxyURLs {
		proxyURL, err := url.Parse(rawURL)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidProxy, err)
		}
		if proxyURL.Scheme == "" || proxyURL.Host == "" {
			return nil, fmt.Errorf("%w: missing scheme or host in %q", ErrInvalidProxy, rawURL)
		}

		// Check for insecure parameter in query string
		if proxyURL.Query().Get("insecure") == "1" {
			insecureSkipVerify = true
		}

		proxies = append(proxies, proxyURL)
	}

	// Apply default values
	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	proxyTimeout := opts.ProxyTimeout
	if proxyTimeout <= 0 {
		proxyTimeout = 30 * time.Second
	}

	retryBackoff := opts.RetryBackoff
	if retryBackoff <= 0 {
		retryBackoff = 2 * time.Second
	}

	failureDuration := opts.ProxyFailureDuration
	if failureDuration <= 0 {
		failureDuration = 3 * time.Second
	}

	// Set default logger if none provided
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	// Set default connection values
	maxIdleConnsPerHost := opts.MaxIdleConnsPerHost
	if maxIdleConnsPerHost <= 0 {
		maxIdleConnsPerHost = 10
	}

	t := &Transport{
		options: Options{
			ProxyURLs:            opts.ProxyURLs,
			MaxRetries:           maxRetries,
			ProxyTimeout:         proxyTimeout,
			RetryBackoff:         retryBackoff,
			ProxyFailureDuration: failureDuration,
			Verbose:              opts.Verbose,
			Logger:               opts.Logger,
			MaxIdleConnsPerHost:  maxIdleConnsPerHost,
		},
		proxies:            proxies,
		failedProxies:      make(map[string]time.Time),
		verbose:            opts.Verbose,
		insecureSkipVerify: insecureSkipVerify,
		logger:             opts.Logger,
		base: http.Transport{
			Proxy: nil, // Will be set per request
			DialContext: (&net.Dialer{
				Timeout:   proxyTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: insecureSkipVerify,
			},
			ForceAttemptHTTP2:      true,
			MaxIdleConns:           100,
			MaxIdleConnsPerHost:    maxIdleConnsPerHost,
			IdleConnTimeout:        90 * time.Second,
			TLSHandshakeTimeout:    10 * time.Second,
			ExpectContinueTimeout:  1 * time.Second,
			DisableKeepAlives:      false, // Enable keep-alives
			ResponseHeaderTimeout:  30 * time.Second,
		},
	}

	return t, nil
}

// isFailedResponse determines if a response indicates a proxy failure.
// It checks for common proxy failure status codes.
func isFailedResponse(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusServiceUnavailable,
		http.StatusBadGateway,
		http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// markProxyAsFailed marks a proxy as failed for the configured failure duration.
func (t *Transport) markProxyAsFailed(proxy *url.URL) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failedProxies[proxy.String()] = time.Now()
}

// isProxyFailed checks if a proxy is marked as failed and handles expiration.
func (t *Transport) isProxyFailed(proxy *url.URL) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	failTime, exists := t.failedProxies[proxy.String()]
	if !exists {
		return false
	}

	// Check if the failure has expired
	if time.Since(failTime) > t.options.ProxyFailureDuration {
		delete(t.failedProxies, proxy.String())
		return false
	}
	return true
}

// nextProxy returns the next available proxy URL in rotation.
// It skips failed proxies and resets the failure list if all proxies are failed.
func (t *Transport) nextProxy() *url.URL {
	t.mu.Lock()
	defer t.mu.Unlock()

	startIndex := t.current
	proxyCount := len(t.proxies)

	// Try each proxy once
	for i := 0; i < proxyCount; i++ {
		currentIndex := (startIndex + i) % proxyCount
		proxy := t.proxies[currentIndex]

		// Check failed status within the same lock
		failTime, exists := t.failedProxies[proxy.String()]
		if !exists || time.Since(failTime) > t.options.ProxyFailureDuration {
			if exists {
				delete(t.failedProxies, proxy.String())
			}
			t.current = (currentIndex + 1) % proxyCount
			return proxy
		}
	}

	// If all proxies are failed, reset failed status and return the next one
	t.failedProxies = make(map[string]time.Time)
	proxy := t.proxies[t.current]
	t.current = (t.current + 1) % proxyCount
	return proxy
}

// isRetryableError determines if an error should trigger a retry attempt.
func isRetryableError(err error) bool {
	var (
		netErr     net.Error
		urlErr     *url.Error
		opErr      *net.OpError
		dnsErr     *net.DNSError
		syscallErr syscall.Errno
	)

	switch {
	case errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()):
		return true
	case errors.As(err, &urlErr):
		return isRetryableError(urlErr.Err) // Recursively check wrapped error
	case errors.As(err, &opErr):
		return opErr.Timeout() || opErr.Temporary()
	case errors.As(err, &dnsErr):
		return true // DNS errors are generally temporary
	case errors.As(err, &syscallErr):
		switch syscallErr {
		case syscall.ECONNREFUSED,
			syscall.ECONNRESET,
			syscall.ETIMEDOUT,
			syscall.EPIPE:
			return true
		}
	}
	return false
}

// calculateBackoff calculates the retry backoff with exponential increase and jitter
func (t *Transport) calculateBackoff(attempt int) time.Duration {
	minBackoff := t.options.MinRetryBackoff
	if minBackoff <= 0 {
		minBackoff = 100 * time.Millisecond
	}
	
	maxBackoff := t.options.MaxRetryBackoff
	if maxBackoff <= 0 {
		maxBackoff = 10 * time.Second
	}

	// Calculate exponential backoff
	backoff := minBackoff * time.Duration(1<<uint(attempt))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}

	// Add jitter (±20%)
	jitterRange := float64(backoff) * 0.2
	jitter := time.Duration(rand.Float64()*jitterRange - jitterRange/2)
	backoff += jitter

	return backoff
}

// RoundTrip implements the http.RoundTripper interface.
// It executes the request using a proxy from the rotation pool,
// automatically retrying with different proxies on failure.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil {
		return nil, errors.New("proxycycle: nil Request.URL")
	}

	if req.Header == nil {
		req.Header = make(http.Header)
	}

	var lastErr error
	origHost := req.Host
	if origHost == "" {
		origHost = req.URL.Host
	}

	startTime := time.Now()
	currentProxy := t.nextProxy()

	for attempt := 0; attempt <= t.options.MaxRetries; attempt++ {
		proxyURL := currentProxy.String()
		
		t.logger.Debug("attempting request",
			"proxy", proxyURL,
			"attempt", attempt+1,
		)

		// Set up proxy for this attempt
		t.base.Proxy = http.ProxyURL(currentProxy)

		// Clone request and set up context
		reqCopy := req.Clone(req.Context())
		ctx, cancel := context.WithTimeout(reqCopy.Context(), t.options.ProxyTimeout)
		reqCopy = reqCopy.WithContext(ctx)

		resp, err := t.base.RoundTrip(reqCopy)
		cancel()

		// Handle response
		if err != nil {
			lastErr = err
			if !isRetryableError(err) {
				t.logger.Error("non-retryable error",
					"proxy", proxyURL,
					"error", err,
				)
				return nil, fmt.Errorf("proxycycle: non-retryable error: %w", err)
			}

			t.logger.Warn("retryable error",
				"proxy", proxyURL,
				"error", err,
			)
			t.markProxyAsFailed(currentProxy)
			currentProxy = t.nextProxy()
		} else if isFailedResponse(resp) {
			t.logger.Warn("proxy failed response",
				"proxy", proxyURL,
				"status", resp.StatusCode,
			)
			resp.Body.Close()
			lastErr = fmt.Errorf("proxycycle: proxy returned status %d", resp.StatusCode)
			t.markProxyAsFailed(currentProxy)
			currentProxy = t.nextProxy()
		} else {
			t.logger.Debug("request successful",
				"proxy", proxyURL,
				"duration", time.Since(startTime),
			)
			return resp, nil
		}

		// Apply backoff if not the last attempt
		if attempt < t.options.MaxRetries {
			backoff := t.calculateBackoff(attempt)
			timer := time.NewTimer(backoff)
			
			t.logger.Debug("applying retry backoff",
				"backoff", backoff,
				"attempt", attempt+1,
			)

			select {
			case <-req.Context().Done():
				timer.Stop()
				return nil, req.Context().Err()
			case <-timer.C:
			}
		}
	}

	return nil, fmt.Errorf("proxycycle: all retries failed, last error: %w", lastErr)
}
