package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nguyenvanduocit/proxycycle"
)

func main() {
	// get URI from ENV, separate by comma
	proxyURLs := strings.Split(os.Getenv("PROXY_URLS"), ",")

	transport, err := proxycycle.New(proxycycle.Options{
		ProxyURLs:            proxyURLs,
		MaxRetries:           2,
		Verbose:              false,
		ProxyTimeout:         time.Second * 5,
		ProxyFailureDuration: time.Second * 30,
	})
	if err != nil {
		fmt.Printf("Error creating transport: %v\n", err)
		return
	}

	// Create an HTTP client with our transport
	client := &http.Client{
		Transport: transport,
	}

	for i := 0; i < 10; i++ {
		// Make a request to our test server
		resp, err := client.Get("http://ip-api.com/json")
		if err != nil {
			fmt.Printf("Error making request: %v\n", err)
			return
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Error reading response: %v\n", err)
			return
		}

		// print raw response
		fmt.Printf("Response: %s\n", string(body))
	}
}
