// Command probe is a tiny static HTTP health probe for distroless images (no shell, curl or wget).
// Usage: airval-probe [url]   (default http://127.0.0.1:8080/readyz). Exits 0 on a 2xx response.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	url := "http://127.0.0.1:8080/readyz"
	if len(os.Args) > 1 {
		url = os.Args[1]
	}
	c := http.Client{Timeout: 4 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		fmt.Fprintln(os.Stderr, resp.Status)
		os.Exit(1)
	}
}
