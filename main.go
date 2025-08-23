package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"strconv"
)

// Job represents a single fuzzing request
type Job struct {
	URL      string
	PostData string
	Depth    int
}

// Result represents the outcome of a fuzz request
type Result struct {
	URL        string `json:"url"`
	StatusCode int    `json:"status_code"`
	Length     int    `json:"length"`
	Error      string `json:"error,omitempty"`
}

type Config struct {
	URLTemplate     string
	Wordlist        string
	Method          string
	Headers         map[string]string
	Extensions      []string
	Workers         int
	MinLength       int
	MaxLength       int
	Recursive       bool
	StatusFilter    []int
	RegexFilter     *regexp.Regexp
	PostData        string
	FollowRedirects bool
	Timeout         time.Duration
	SkipTLSVerify   bool
	Proxy           string
	UseTor          bool
	MaxDepth        int
	JSONOutput      bool
	Retries         int
}

// replace FUZZ placeholder
func replacePlaceholder(template, word string) string {
	return strings.ReplaceAll(template, "FUZZ", word)
}

func containsInt(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

// Worker
func worker(jobs chan Job, results chan Result, cfg *Config, client *http.Client, wg *sync.WaitGroup, visited *sync.Map) {
	defer wg.Done()
	for job := range jobs {
		// Avoid revisiting
		if cfg.Recursive {
			if _, loaded := visited.LoadOrStore(job.URL, true); loaded {
				continue
			}
		}

		var bodyReader io.Reader
		if job.PostData != "" {
			bodyReader = strings.NewReader(job.PostData)
		}

		var resp *http.Response
		var err error
		for attempt := 0; attempt <= cfg.Retries; attempt++ {
			req, e := http.NewRequest(cfg.Method, job.URL, bodyReader)
			if e != nil {
				err = e
				continue
			}

			for k, v := range cfg.Headers {
				req.Header.Set(k, replacePlaceholder(v, job.URL))
			}

			resp, err = client.Do(req)
			if err == nil {
				break
			}
			time.Sleep(500 * time.Millisecond) // retry delay
		}

		if err != nil {
			results <- Result{URL: job.URL, Error: err.Error()}
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		length := len(body)

		// Filters
		if len(cfg.StatusFilter) > 0 && !containsInt(cfg.StatusFilter, resp.StatusCode) {
			continue
		}
		if (cfg.MinLength > 0 && length < cfg.MinLength) || (cfg.MaxLength > 0 && length > cfg.MaxLength) {
			continue
		}
		if cfg.RegexFilter != nil && !cfg.RegexFilter.Match(body) {
			continue
		}

		results <- Result{
			URL:        job.URL,
			StatusCode: resp.StatusCode,
			Length:     length,
		}

		// Recursive fuzzing
		if cfg.Recursive && job.Depth < cfg.MaxDepth {
			for _, ext := range cfg.Extensions {
				newJob := Job{
					URL:      replacePlaceholder(cfg.URLTemplate, job.URL+ext),
					PostData: replacePlaceholder(cfg.PostData, job.URL+ext),
					Depth:    job.Depth + 1,
				}
				jobs <- newJob
			}
			// Plain recursion
			newJob := Job{
				URL:      job.URL,
				PostData: replacePlaceholder(cfg.PostData, job.URL),
				Depth:    job.Depth + 1,
			}
			jobs <- newJob
		}
	}
}

// Colored output helper
func printResult(res Result, jsonOutput bool) {
	if jsonOutput {
		data, _ := json.Marshal(res)
		fmt.Println(string(data))
		return
	}

	var color string
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		color = "\033[32m" // green
	case res.StatusCode >= 300 && res.StatusCode < 400:
		color = "\033[36m" // cyan
	case res.StatusCode >= 400 && res.StatusCode < 500:
		color = "\033[33m" // yellow
	case res.StatusCode >= 500:
		color = "\033[31m" // red
	default:
		color = "\033[0m"
	}

	if res.Error != "" {
		fmt.Printf("\033[35m[ERROR]\033[0m %s -> %s\n", res.URL, res.Error)
	} else {
		fmt.Printf("%s%s\033[0m\t%d\t%d\n", color, res.URL, res.StatusCode, res.Length)
	}
}

func main() {
	cfg := &Config{}

	flag.StringVar(&cfg.URLTemplate, "u", "", "URL template with FUZZ placeholder")
	flag.StringVar(&cfg.Wordlist, "w", "", "Wordlist file")
	flag.StringVar(&cfg.Method, "X", "GET", "HTTP method")
	headerStr := flag.String("H", "", "Headers comma-separated, e.g., 'User-Agent: FUZZ'")
	extStr := flag.String("e", "", "Extensions comma-separated, e.g., .php,.html")
	statusStr := flag.String("s", "", "Status codes to filter comma-separated, e.g., 200,301")
	regexStr := flag.String("r", "", "Regex filter for response body")
	flag.IntVar(&cfg.Workers, "t", 10, "Number of concurrent workers")
	flag.IntVar(&cfg.MinLength, "min", 0, "Minimum response length")
	flag.IntVar(&cfg.MaxLength, "max", 0, "Maximum response length")
	flag.StringVar(&cfg.PostData, "d", "", "POST data")
	flag.BoolVar(&cfg.FollowRedirects, "f", true, "Follow redirects")
	flag.DurationVar(&cfg.Timeout, "timeout", 10*time.Second, "Request timeout")
	flag.BoolVar(&cfg.SkipTLSVerify, "k", false, "Skip TLS verification")
	flag.StringVar(&cfg.Proxy, "proxy", "", "HTTP proxy URL")
	flag.BoolVar(&cfg.UseTor, "tor", false, "Use Tor SOCKS5 proxy on 127.0.0.1:9050")
	flag.BoolVar(&cfg.Recursive, "rec", false, "Enable recursive fuzzing")
	flag.IntVar(&cfg.MaxDepth, "depth", 2, "Maximum recursion depth")
	flag.BoolVar(&cfg.JSONOutput, "json", false, "Output results in JSON")
	flag.IntVar(&cfg.Retries, "retries", 1, "Number of retries for failed requests")
	flag.Parse()

	if cfg.URLTemplate == "" || cfg.Wordlist == "" {
		fmt.Println("Usage: gofuzzyourself -u <url> -w <wordlist> [options]")
		os.Exit(1)
	}

	// Headers
	cfg.Headers = make(map[string]string)
	if *headerStr != "" {
		for _, h := range strings.Split(*headerStr, ",") {
			parts := strings.SplitN(h, ":", 2)
			if len(parts) == 2 {
				cfg.Headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	// Extensions
	if *extStr != "" {
		cfg.Extensions = strings.Split(*extStr, ",")
	}

	// Status codes
	if *statusStr != "" {
		for _, s := range strings.Split(*statusStr, ",") {
			if code, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
				cfg.StatusFilter = append(cfg.StatusFilter, code)
			}
		}
	}

	// Regex
	if *regexStr != "" {
		re, err := regexp.Compile(*regexStr)
		if err != nil {
			fmt.Println("Invalid regex:", err)
			os.Exit(1)
		}
		cfg.RegexFilter = re
	}

	// HTTP client
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.SkipTLSVerify},
	}

	// Proxy / Tor
	if cfg.UseTor {
		proxyURL, _ := url.Parse("socks5://127.0.0.1:9050")
		transport.Proxy = http.ProxyURL(proxyURL)
	} else if cfg.Proxy != "" {
		proxyURL, _ := url.Parse(cfg.Proxy)
		transport.Proxy = http.ProxyURL(proxyURL)
	}

	client := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
	}

	if !cfg.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	// Channels
	jobs := make(chan Job, cfg.Workers*2)
	results := make(chan Result, cfg.Workers*2)
	var wg sync.WaitGroup
	visited := sync.Map{}

	// Start workers
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go worker(jobs, results, cfg, client, &wg, &visited)
	}

	// Result printer
	go func() {
		for res := range results {
			printResult(res, cfg.JSONOutput)
		}
	}()

	// Feed initial jobs
	file, err := os.Open(cfg.Wordlist)
	if err != nil {
		fmt.Println("Error opening wordlist:", err)
		os.Exit(1)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		word := scanner.Text()
		if word == "" {
			continue
		}
		// Base
		jobs <- Job{URL: replacePlaceholder(cfg.URLTemplate, word), PostData: replacePlaceholder(cfg.PostData, word), Depth: 0}
		// With extensions
		for _, ext := range cfg.Extensions {
			jobs <- Job{URL: replacePlaceholder(cfg.URLTemplate, word+ext), PostData: replacePlaceholder(cfg.PostData, word+ext), Depth: 0}
		}
	}

	close(jobs)
	wg.Wait()
	close(results)
}
