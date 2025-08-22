package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

func printBanner() {
	banner := `
  ___  _____  ____  __  __  ____  ____ 
 / __)(  _  )( ___)(  )(  )(_   )(_   )
( (_-. )(_)(  )__)  )(__)(  / /_  / /_ 
 \___/(_____)(__)  (______)(____)(____)

`
	fmt.Println(banner)
}

func fuzzUrl(url, wordlist string, workers int) {
	// Get the current time
	start := time.Now()

	// Open the wordlist
	file, err := os.Open(wordlist)
	if err != nil {
		fmt.Println("Error opening wordlist:", err)
		return
	}
	defer file.Close()

	lines := []string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading wordlist:", err)
		return
	}

	var wg sync.WaitGroup
	jobs := make(chan string, len(lines))

	// Worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for line := range jobs {
				req, err := http.NewRequest("GET", url+line, nil)
				if err != nil {
					continue
				}
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					continue
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode == 200 {
					fmt.Printf("%s%s\t%d\t%d\n", url, line, resp.StatusCode, len(body))
				}
			}
		}()
	}

	// Send jobs
	for _, line := range lines {
		jobs <- line
	}
	close(jobs)

	wg.Wait()

	// Get the elapsed time
	elapsed := time.Since(start)
	fmt.Printf("Time taken: %v\n", elapsed)
}

func main() {
	// Print the banner
	printBanner()

	// If the arguments are not provided
	if len(os.Args) < 3 {
		fmt.Println("Usage: " + os.Args[0] + " <url> <wordlist> [workers]")
		os.Exit(1)
	}

	// Get the url and wordlist from the command line
	url := os.Args[1]
	wordlist := os.Args[2]

	// Default number of workers
	workers := 10
	if len(os.Args) >= 4 {
		if w, err := strconv.Atoi(os.Args[3]); err == nil && w > 0 {
			workers = w
		}
	}

	// Call the fuzzUrl function
	fuzzUrl(url, wordlist, workers)
}
