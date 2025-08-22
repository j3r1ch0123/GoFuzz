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

func fuzzUrl(url, wordlist, method string, workers int, extensions []string, headers map[string]string, statusCodeFilter []int) {
	start := time.Now()

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
				req, err := http.NewRequest(method, url+path, nil)
				if err != nil {
					continue
				}

				// Apply custom headers
				for k, v := range headers {
					req.Header.Set(k, v)
				}

				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					continue
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				// Filtering logic
				if len(statusCodeFilter) == 0 || containsInt(statusCodeFilter, resp.StatusCode) {
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

	elapsed := time.Since(start)
	fmt.Printf("Time taken: %v\n", elapsed)
}

func containsInt(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

func parseHeaders(headerArg string) map[string]string {
	headers := make(map[string]string)
	if headerArg == "" {
		return headers
	}
	pairs := strings.Split(headerArg, ",")
	for _, h := range pairs {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			headers[key] = val
		}
	}
	return headers
}

func main() {
	printBanner()

	if len(os.Args) < 3 {
		fmt.Println("Usage: " + os.Args[0] + " <url> <wordlist> [workers] [extensions] [method] [headers] [statusCodes]")
		fmt.Println("Example: " + os.Args[0] + " http://target/ words.txt 20 .php,.bak GET \"User-Agent: fuzzzilla\" 200,302")
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

	method := "GET"
	if len(os.Args) >= 6 {
		method = strings.ToUpper(os.Args[5])
	}

	headers := map[string]string{}
	if len(os.Args) >= 7 {
		headers = parseHeaders(os.Args[6])
	}

	statusCodeFilter := []int{}
	if len(os.Args) >= 8 {
		codeStrs := strings.Split(os.Args[7], ",")
		for _, cs := range codeStrs {
			if c, err := strconv.Atoi(strings.TrimSpace(cs)); err == nil {
				statusCodeFilter = append(statusCodeFilter, c)
			}
		}
	}

	fuzzUrl(url, wordlist, method, workers, extensions, headers, statusCodeFilter)
}
