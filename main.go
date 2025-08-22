package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

func printBanner() {
	banner := `
  ________      ___________                   _____.___.                                 .__   _____ 
 /  _____/  ____\_   _____/_ _________________\__  |   | ____  __ _________  ______ ____ |  |_/ ____\
/   \  ___ /  _ \|    __)|  |  \___   /\___   //   |   |/  _ \|  |  \_  __ \/  ___// __ \|  |\   __\ 
\    \_\  (  <_> )     \ |  |  //    /  /    / \____   (  <_> )  |  /|  | \/\___ \\  ___/|  |_|  |   
 \______  /\____/\___  / |____//_____ \/_____ \/ ______|\____/|____/ |__|  /____  >\___  >____/__|   
        \/           \/              \/      \/\/                               \/     \/            

`
	fmt.Println(banner)
}

func fuzzUrl(url, wordlist string, workers int, extensions []string) {
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
	jobs := make(chan string, len(lines)*len(extensions)+len(lines))

	// Worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				req, err := http.NewRequest("GET", url+path, nil)
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
					fmt.Printf("%s%s\t%d\t%d\n", url, path, resp.StatusCode, len(body))
				}
			}
		}()
	}

	// Send jobs: bare word and word+extensions
	for _, line := range lines {
		jobs <- line
		for _, ext := range extensions {
			jobs <- line + ext
		}
	}
	close(jobs)

	wg.Wait()

	// Get the elapsed time
	elapsed := time.Since(start)
	fmt.Printf("Time taken: %v\n", elapsed)
}

func main() {
	printBanner()

	if len(os.Args) < 3 {
		fmt.Println("Usage: " + os.Args[0] + " <url> <wordlist> [workers] [extensions]")
		os.Exit(1)
	}

	url := os.Args[1]
	wordlist := os.Args[2]

	workers := 10
	if len(os.Args) >= 4 {
		if w, err := strconv.Atoi(os.Args[3]); err == nil && w > 0 {
			workers = w
		}
	}

	extensions := []string{}
	if len(os.Args) >= 5 {
		extensions = strings.Split(os.Args[4], ",")
	}

	fuzzUrl(url, wordlist, workers, extensions)
}
