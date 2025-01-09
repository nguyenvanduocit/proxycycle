# ProxyCycle

ProxyCycle is a resilient HTTP transport implementation for Go that provides automatic proxy rotation and failure handling capabilities. It's designed to make your HTTP requests more reliable when working with multiple proxy servers.

## Features

- 🔄 Automatic proxy rotation
- ⚡ Configurable retry mechanism
- 🛡️ Proxy failure detection and temporary blacklisting
- ⏱️ Configurable timeouts and backoff strategies
- 📝 Optional verbose logging
- 🔒 Support for secure and insecure proxy configurations
- 🖥️ Command-line interface (CLI)

## Installation

### As a Package
```bash
go get github.com/nguyenvanduocit/proxycycle
```

### As a CLI Tool
```bash
go install github.com/nguyenvanduocit/proxycycle/cmd/proxycycle@latest
```

## Usage

### CLI Usage

The CLI tool provides a simple way to make HTTP requests through rotating proxies:

```bash
# Basic usage
proxycycle --proxies "http://proxy1.com:8080,http://proxy2.com:8080" --url "https://api.example.com"

# With all options
proxycycle \
  --proxies "http://proxy1.com:8080,http://proxy2.com:8080" \
  --url "https://api.example.com" \
  --retries 3 \
  --timeout 5s \
  --failure-duration 30s \
  --repeat 5 \
  --verbose

# Show help
proxycycle --help
```

Available CLI options:
- `--proxies, -p`: Comma-separated list of proxy URLs (required)
- `--url, -u`: Target URL to request (default: http://ip-api.com/json)
- `--retries, -r`: Maximum number of retries per request (default: 2)
- `--timeout, -t`: Timeout for each proxy attempt (default: 5s)
- `--failure-duration, -f`: How long to mark a proxy as failed (default: 30s)
- `--repeat, -n`: Number of times to repeat the request (default: 1)
- `--verbose, -v`: Enable verbose logging

### Package Usage

Here's how to use ProxyCycle as a Go package:

```go
package main

import (
    "github.com/nguyenvanduocit/proxycycle"
    "net/http"
    "time"
)

func main() {
    // Initialize the transport with a list of proxies
    transport, err := proxycycle.New(proxycycle.Options{
        ProxyURLs: []string{
            "http://proxy1.example.com:8080",
            "http://proxy2.example.com:8080",
            "socks5://proxy3.example.com:1080",
        },
        MaxRetries:           3,
        ProxyTimeout:         30 * time.Second,
        RetryBackoff:         2 * time.Second,
        ProxyFailureDuration: 3 * time.Second,
        Verbose:              true,
    })
    if err != nil {
        panic(err)
    }

    // Create an HTTP client with the transport
    client := &http.Client{
        Transport: transport,
    }

    // Use the client as normal
    resp, err := client.Get("https://api.example.com")
    // Handle response...
}
```

## Configuration Options

The `Options` struct provides several configuration parameters:

- `ProxyURLs` (required): List of proxy URLs to cycle through
- `MaxRetries` (default: 3): Maximum number of retries per request
- `ProxyTimeout` (default: 30s): Timeout for each proxy attempt
- `RetryBackoff` (default: 2s): Delay between retry attempts
- `ProxyFailureDuration` (default: 3s): How long to mark a proxy as failed
- `Verbose` (default: false): Enable detailed logging

## Proxy URL Formats

ProxyCycle supports various proxy URL formats:

- HTTP: `http://host:port`
- HTTPS: `https://host:port`
- SOCKS5: `socks5://host:port`

To skip TLS verification for a proxy, add `?insecure=1` to the URL:

```
http://proxy.example.com:8080?insecure=1
```

## Features in Detail

### Proxy Rotation

ProxyCycle automatically rotates through the provided list of proxies. If a proxy fails, it's temporarily marked as failed and skipped in the rotation.

### Failure Handling

- Automatically detects proxy failures
- Temporarily blacklists failed proxies
- Retries requests with different proxies
- Supports configurable retry strategies

### Connection Management

- Automatic connection pooling
- Configurable timeouts
- Keep-alive management
- Support for HTTP/2

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
