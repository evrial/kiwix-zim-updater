package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
	"golang.org/x/time/rate"
)

const (
	Reset  = "\033[0m"
	Red    = "\033[1;31m"
	Green  = "\033[1;32m"
	Yellow = "\033[1;33m"
	Blue   = "\033[1;34m"
)

// Updated regex to support full date formats like _2023-05.zim and _2023-05-15.zim
var dateRegex = regexp.MustCompile(`_(\d{4})-(\d{2})(?:-\d{2})?[a-z]?\.zim$`)

var customHTTPClient = &http.Client{
	Timeout: 0,
	Transport: &http.Transport{
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	},
}

type ZimMetaData struct {
	FileName string
	BaseName string
	URL      string
	Size     int64
	Year     int
	Month    int
}

type Config struct {
	Command    string // "update" or "search"
	ZimDir     string
	AutoUpdate bool
	Workers    int
	RateLimit  int64
	CheckSize  bool
	Search     string
}

type Task struct {
	LocalFile string
	Remote    ZimMetaData
}

type OPDSFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []OPDSEntry `xml:"entry"`
}

type OPDSEntry struct {
	Links []OPDSLink `xml:"link"`
}

type OPDSLink struct {
	Rel    string `xml:"rel,attr"`
	Type   string `xml:"type,attr"`
	Href   string `xml:"href,attr"`
	Length int64  `xml:"length,attr"`
}

func NewRateLimiter(rateMB int64) *rate.Limiter {
	if rateMB <= 0 {
		return nil
	}
	bytesPerSec := rate.Limit(rateMB * 1024 * 1024)
	burst := 256 * 1024
	return rate.NewLimiter(bytesPerSec, burst)
}

// LipGloss Styles for UI components & Progress Bars
var (
	titleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	promptStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	selectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	filledBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))  // Cyan
	emptyBarStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238")) // Dark Grey
	statsStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
)

func renderProgressBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	} else if percent > 1 {
		percent = 1
	}

	filledLen := int(math.Round(percent * float64(width)))
	emptyLen := width - filledLen

	filled := filledBarStyle.Render(strings.Repeat("█", filledLen))
	empty := emptyBarStyle.Render(strings.Repeat("░", emptyLen))

	return "[" + filled + empty + "]"
}

type fuzzyModel struct {
	items    []ZimMetaData
	matches  []fuzzy.Match
	query    string
	cursor   int
	selected map[int]bool
	canceled bool
	height   int
}

func initialFuzzyModel(items []ZimMetaData, initialQuery string) fuzzyModel {
	m := fuzzyModel{
		items:    items,
		query:    initialQuery,
		selected: make(map[int]bool),
		height:   25,
	}
	m.filter()
	return m
}

func (m *fuzzyModel) filter() {
	var targets []string
	for _, item := range m.items {
		targets = append(targets, item.FileName)
	}

	if m.query == "" {
		m.matches = make([]fuzzy.Match, len(m.items))
		for i := range m.items {
			m.matches[i] = fuzzy.Match{Index: i}
		}
	} else {
		m.matches = fuzzy.Find(m.query, targets)
	}

	m.cursor = 0
}

func (m fuzzyModel) Init() tea.Cmd {
	return nil
}

func (m *fuzzyModel) toggleSelection() {
	if len(m.matches) > 0 && m.cursor < len(m.matches) {
		idx := m.matches[m.cursor].Index
		if m.selected[idx] {
			delete(m.selected, idx)
		} else {
			m.selected[idx] = true
		}
	}
}

func (m fuzzyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.cursor < len(m.matches)-1 {
				m.cursor++
			}
		}

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.canceled = true
			return m, tea.Quit

		case tea.KeyEnter:
			if len(m.selected) == 0 && len(m.matches) > 0 && m.cursor < len(m.matches) {
				m.selected[m.matches[m.cursor].Index] = true
			}
			return m, tea.Quit

		case tea.KeyUp, tea.KeyCtrlP:
			if m.cursor > 0 {
				m.cursor--
			}

		case tea.KeyDown:
			if m.cursor < len(m.matches)-1 {
				m.cursor++
			}

		case tea.KeyCtrlA:
			for _, match := range m.matches {
				m.selected[match.Index] = true
			}

		case tea.KeyEsc:
			m.selected = make(map[int]bool)

		case tea.KeySpace:
			m.toggleSelection()

		case tea.KeyRunes:
			// Treat standalone space rune as selection trigger (Windows fallback)
			if len(msg.Runes) == 1 && msg.Runes[0] == ' ' {
				m.toggleSelection()
				return m, nil
			}

			// Filter out control codes and spaces from query additions
			validRunes := make([]rune, 0, len(msg.Runes))
			for _, r := range msg.Runes {
				if r > 32 && r != 127 { // Keeps only printable non-space characters
					validRunes = append(validRunes, r)
				}
			}

			if len(validRunes) > 0 {
				m.query += string(validRunes)
				m.filter()
			}

		case tea.KeyBackspace:
			if len(m.query) > 0 {
				m.query = m.query[:len(m.query)-1]
				m.filter()
			}
		}
	}

	return m, nil
}

func (m fuzzyModel) View() string {
	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Search Kiwix ZIM Catalog") + "\n")
	sb.WriteString(promptStyle.Render("> ") + m.query + "\n\n")

	if len(m.matches) == 0 {
		sb.WriteString(dimStyle.Render("  No matching ZIM entries found.") + "\n")
		return sb.String()
	}

	start := 0
	if m.cursor >= m.height {
		start = m.cursor - m.height + 1
	}
	end := start + m.height
	if end > len(m.matches) {
		end = len(m.matches)
	}

	for i := start; i < end; i++ {
		matchIdx := m.matches[i].Index
		item := m.items[matchIdx]

		sizeMB := float64(item.Size) / (1024 * 1024)
		var sizeStr string
		if item.Size >= 1024*1024*1024 {
			sizeStr = fmt.Sprintf("%.2f GiB", sizeMB/1024)
		} else {
			sizeStr = fmt.Sprintf("%.1f MiB", sizeMB)
		}

		checked := " "
		if m.selected[matchIdx] {
			checked = "x"
		}

		line := fmt.Sprintf("[%s] %-55s (%s)", checked, item.FileName, sizeStr)

		if i == m.cursor {
			sb.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", line)) + "\n")
		} else {
			sb.WriteString(fmt.Sprintf("  %s\n", line))
		}
	}

	selectedCount := len(m.selected)
	sb.WriteString("\n" + dimStyle.Render(fmt.Sprintf("[%d/%d matches | %d selected] • Space: Toggle • Ctrl+A: Select All • Esc: Clear • Enter: Confirm", len(m.matches), len(m.items), selectedCount)))
	return sb.String()
}

func processDownloadTask(ctx context.Context, cfg *Config, task Task, limiter *rate.Limiter, activeLocks *map[string]bool, lockMu *sync.Mutex, workerID int) {
	destPath := filepath.Join(cfg.ZimDir, task.Remote.FileName)
	lockPath := destPath + ".~lock"

	lockMu.Lock()
	(*activeLocks)[lockPath] = true
	lockMu.Unlock()

	defer func() {
		os.Remove(lockPath)
		lockMu.Lock()
		delete(*activeLocks, lockPath)
		lockMu.Unlock()
	}()

	fmt.Printf("[W%d] Starting download: %s\n", workerID, task.Remote.FileName)

	err := downloadWithRetry(ctx, task.Remote.URL, destPath, lockPath, limiter, workerID)
	if err != nil {
		fmt.Printf("\n%s[W%d] Download failed for %s: %v%s\n", Red, workerID, task.Remote.FileName, err, Reset)
		return
	}

	fmt.Printf("\n[W%d] Verifying checksum for %s...\n", workerID, task.Remote.FileName)
	if err := verifySHA256(ctx, task.Remote.URL, destPath); err != nil {
		fmt.Printf("%s[W%d] Checksum verification failed for %s: %v%s\n", Red, workerID, task.Remote.FileName, err, Reset)
		return
	}

	if task.LocalFile != "" && task.LocalFile != task.Remote.FileName {
		oldPath := filepath.Join(cfg.ZimDir, task.LocalFile)
		_ = os.Remove(oldPath)
		fmt.Printf("%s[W%d] Removed outdated version: %s%s\n", Yellow, workerID, task.LocalFile, Reset)
	}

	fmt.Printf("%s[W%d] Successfully downloaded: %s%s\n", Green, workerID, task.Remote.FileName, Reset)
}

func downloadWithRetry(ctx context.Context, url, destPath, lockPath string, limiter *rate.Limiter, workerID int) error {
	maxRetries := 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			baseBackoff := time.Duration(math.Pow(2, float64(attempt-1))) * time.Second
			jitter := time.Duration(rand.Float64() * float64(baseBackoff))
			sleepDuration := baseBackoff + jitter

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sleepDuration):
			}
		}

		lastErr = downloadChunk(ctx, url, destPath, lockPath, limiter, workerID)
		if lastErr == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	return lastErr
}

func downloadChunk(ctx context.Context, url, destPath, lockPath string, limiter *rate.Limiter, workerID int) error {
	_ = os.WriteFile(lockPath, []byte(""), 0644)

	var initialSize int64
	if stat, err := os.Stat(destPath); err == nil {
		initialSize = stat.Size()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Kiwix-Zim-Updater)")

	if initialSize > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", initialSize))
	}

	resp, err := customHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	flags := os.O_CREATE | os.O_WRONLY
	if resp.StatusCode == http.StatusPartialContent {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		initialSize = 0
	}

	out, err := os.OpenFile(destPath, flags, 0644)
	if err != nil {
		return err
	}
	defer out.Close()

	totalSize := resp.ContentLength
	if resp.StatusCode == http.StatusPartialContent {
		totalSize += initialSize
	}

	currentSize := initialSize
	buf := make([]byte, 32*1024)
	startTime := time.Now()
	lastUpdate := time.Now()

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if limiter != nil {
				maxBurst := limiter.Burst()
				for remaining := n; remaining > 0; {
					chunk := remaining
					if chunk > maxBurst {
						chunk = maxBurst
					}
					if err := limiter.WaitN(ctx, chunk); err != nil {
						return err
					}
					remaining -= chunk
				}
			}

			_, writeErr := out.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}
			currentSize += int64(n)

			if time.Since(lastUpdate) >= 100*time.Millisecond || readErr == io.EOF {
				lastUpdate = time.Now()
				elapsed := time.Since(startTime).Seconds()
				speed := float64(currentSize-initialSize) / (1024 * 1024 * elapsed)
				if elapsed == 0 {
					speed = 0
				}

				var percent float64
				if totalSize > 0 {
					percent = float64(currentSize) / float64(totalSize)
				}

				bar := renderProgressBar(percent, 25)
				currentMB := float64(currentSize) / (1024 * 1024)
				totalMB := float64(totalSize) / (1024 * 1024)

				stats := statsStyle.Render(fmt.Sprintf("%5.1f%% (%6.1f/%6.1f MiB) @ %5.2f MiB/s", percent*100, currentMB, totalMB, speed))
				fmt.Printf("\r\033[K  [W%d] %s %s", workerID, bar, stats)
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

func getRemoteFileSize(ctx context.Context, url string) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Kiwix-Zim-Updater)")

	resp, err := customHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusFound {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.ContentLength, nil
}

func verifySHA256(ctx context.Context, fileURL, destPath string) error {
	shaURL := fileURL + ".sha256"
	req, err := http.NewRequestWithContext(ctx, "GET", shaURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Kiwix-Zim-Updater)")

	resp, err := customHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch sha256 sidecar file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sha256 sidecar file returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	expectedHash := strings.Fields(string(body))[0]

	file, err := os.Open(destPath)
	if err != nil {
		return err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	computedHash := hex.EncodeToString(hasher.Sum(nil))

	if !strings.EqualFold(computedHash, expectedHash) {
		return fmt.Errorf("hash mismatch (Expected: %s, Got: %s)", expectedHash, computedHash)
	}
	return nil
}

func fetchCatalog(ctx context.Context) ([]ZimMetaData, error) {
	url := "https://opds.library.kiwix.org/catalog/v2/entries?count=-1"

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Kiwix-Zim-Updater)")

	resp, err := customHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	var feed OPDSFeed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, fmt.Errorf("xml decoding failed: %w", err)
	}

	var results []ZimMetaData
	seen := make(map[string]bool)

	for _, entry := range feed.Entries {
		for _, link := range entry.Links {
			if strings.Contains(link.Rel, "acquisition") || link.Type == "application/x-zim" || strings.Contains(link.Href, ".zim") {
				rawHref := link.Href
				if strings.HasSuffix(rawHref, ".meta4") {
					rawHref = strings.TrimSuffix(rawHref, ".meta4")
				}

				if !strings.HasSuffix(rawHref, ".zim") {
					continue
				}

				href := rawHref
				if !strings.HasPrefix(href, "http") {
					href = "https://download.kiwix.org/zim/" + strings.TrimPrefix(href, "/")
				}

				fileName := filepath.Base(href)
				baseName, year, month := parseZimName(fileName)
				if baseName == "" || seen[fileName] {
					continue
				}

				seen[fileName] = true
				results = append(results, ZimMetaData{
					FileName: fileName,
					BaseName: baseName,
					URL:      href,
					Size:     link.Length,
					Year:     year,
					Month:    month,
				})
			}
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return strings.ToLower(results[i].FileName) < strings.ToLower(results[j].FileName)
	})
	return results, nil
}

func handleCatalogSearch(remoteZims []ZimMetaData, query string) ([]Task, error) {
	p := tea.NewProgram(
		initialFuzzyModel(remoteZims, query),
		tea.WithMouseCellMotion(),
	)
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run interactive search: %w", err)
	}

	m, ok := finalModel.(fuzzyModel)
	if !ok || m.canceled || len(m.selected) == 0 {
		return nil, fmt.Errorf("selection cancelled")
	}

	var tasks []Task
	for idx := range m.selected {
		tasks = append(tasks, Task{
			LocalFile: "",
			Remote:    m.items[idx],
		})
	}

	return tasks, nil
}

func parseZimName(filename string) (basename string, year int, month int) {
	matches := dateRegex.FindStringSubmatch(filename)
	if len(matches) < 3 {
		return "", 0, 0
	}
	loc := dateRegex.FindStringIndex(filename)
	basename = filename[:loc[0]]
	year, _ = strconv.Atoi(matches[1])
	month, _ = strconv.Atoi(matches[2])
	return
}

func isRemoteNewer(remote ZimMetaData, localYear int, localMonth int) bool {
	if remote.Year > localYear {
		return true
	}
	if remote.Year == localYear && remote.Month > localMonth {
		return true
	}
	return false
}

func absDiff(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}

func askConfirmation(prompt string) bool {
	var response string
	fmt.Print(prompt)
	_, err := fmt.Scanln(&response)
	if err != nil {
		return false
	}
	response = strings.ToLower(strings.TrimSpace(response))
	return response == "y" || response == "yes"
}

func parseFlags() *Config {
	cfg := &Config{
		Command: "update", // Default mode
	}

	// 1. Shared / Global Flag Definitions
	var zimDir string
	var autoUpdate bool
	var workers int
	var rateLimit int64
	var checkSize bool

	// 2. Define FlagSet for "search" subcommand
	searchCmd := flag.NewFlagSet("search", flag.ExitOnError)
	searchCmd.StringVar(&zimDir, "dir", ".", "Path to directory containing .zim files")
	searchCmd.IntVar(&workers, "j", 1, "Number of concurrent worker downloads")
	searchCmd.Int64Var(&rateLimit, "limit", 0, "Bandwidth rate limit in MiB/s per download (0 = unlimited)")
	searchCmd.BoolVar(&autoUpdate, "a", false, "Automatic download without confirmation prompt")

	// 3. Define FlagSet for default "update" mode
	updateCmd := flag.NewFlagSet("update", flag.ExitOnError)
	updateCmd.StringVar(&zimDir, "dir", ".", "Path to directory containing .zim files")
	updateCmd.BoolVar(&autoUpdate, "a", false, "Automatic update without confirmation prompt")
	updateCmd.IntVar(&workers, "j", 1, "Number of concurrent worker downloads")
	updateCmd.Int64Var(&rateLimit, "limit", 0, "Bandwidth rate limit in MiB/s per download (0 = unlimited)")
	updateCmd.BoolVar(&checkSize, "check-size", false, "Perform HTTP HEAD requests to verify remote file size integrity")

	if len(os.Args) < 2 {
		// Default invocation: ./zim-updater
		_ = updateCmd.Parse(os.Args[1:])
	} else {
		switch os.Args[1] {
		case "search":
			cfg.Command = "search"
			_ = searchCmd.Parse(os.Args[2:])

			// If an optional search string is provided as a positional argument after 'search'
			// Example: ./zim-updater search wikipedia
			if searchCmd.NArg() > 0 {
				cfg.Search = searchCmd.Arg(0)
			}

		case "update":
			cfg.Command = "update"
			_ = updateCmd.Parse(os.Args[2:])
			if updateCmd.NArg() > 0 {
				zimDir = updateCmd.Arg(0)
			}

		default:
			// Fallback: If 1st argument isn't "search" or "update", treat as standard flags
			// Example: ./zim-updater -dir /path/to/zims
			_ = updateCmd.Parse(os.Args[1:])
			if updateCmd.NArg() > 0 {
				zimDir = updateCmd.Arg(0)
			}
		}
	}

	cfg.ZimDir = zimDir
	cfg.AutoUpdate = autoUpdate
	cfg.Workers = workers
	cfg.RateLimit = rateLimit
	cfg.CheckSize = checkSize

	if cfg.Workers < 1 {
		cfg.Workers = 1
	}

	return cfg
}

func main() {
	cfg := parseFlags()

	absPath, err := filepath.Abs(cfg.ZimDir)
	if err != nil || cfg.ZimDir == "" {
		log.Fatalf("%s✗ Error: Valid Kiwix ZIM directory required.%s\n", Red, Reset)
	}
	cfg.ZimDir = absPath

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("%s- Fetching remote catalog...%s\n", Blue, Reset)
	fetchStart := time.Now()
	remoteZims, err := fetchCatalog(ctx)
	if err != nil {
		log.Fatalf("%s✗ Failed to fetch catalog: %v%s\n", Red, err, Reset)
	}
	fetchDuration := time.Since(fetchStart).Round(time.Millisecond)
	fmt.Printf("%s  ✓ Loaded %d remote ZIM entries [%v]%s\n\n", Green, len(remoteZims), fetchDuration, Reset)

	var tasks []Task

	switch cfg.Command {
	case "search":
		tasks, err = handleCatalogSearch(remoteZims, cfg.Search)
		if err != nil {
			fmt.Printf("%s%v%s\n", Yellow, err, Reset)
			return
		}
	case "update":
		tasks, err = runUpdateMode(ctx, cfg, remoteZims)
		if err != nil {
			log.Fatalf("%s✗ Update scan failed: %v%s\n", Red, err, Reset)
		}
	}

	if len(tasks) == 0 {
		fmt.Printf("%s✓ Everything is up to date / No downloads selected.%s\n", Green, Reset)
		return
	}

	var totalDownloadSize int64
	for i := range tasks {
		if tasks[i].Remote.Size == 0 {
			sz, err := getRemoteFileSize(ctx, tasks[i].Remote.URL)
			if err != nil {
				log.Fatalf("%s✗ Failed to get remote file size for %s: %v%s\n", Red, tasks[i].Remote.FileName, err, Reset)
			}
			tasks[i].Remote.Size = sz
		}
		totalDownloadSize += tasks[i].Remote.Size
	}
	if err := checkDiskSpace(cfg.ZimDir, totalDownloadSize); err != nil {
		log.Fatalf("%s✗ Disk Space Error: %v%s\n", Red, err, Reset)
	}

	if !cfg.AutoUpdate {
		fmt.Printf("%sFound %d updates/downloads (Total size: %.2f GiB).%s\n", Yellow, len(tasks), float64(totalDownloadSize)/(1024*1024*1024), Reset)
		if !askConfirmation("Do you want to proceed with downloading? [y/N]: ") {
			fmt.Println("Aborted by user.")
			return
		}
	}

	activeLocks := make(map[string]bool)
	var lockMu sync.Mutex
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Printf("\n\n%s[!] Interrupt received. Cancelling context and purging lock files...%s\n", Red, Reset)
		cancel()
		lockMu.Lock()
		for lockPath := range activeLocks {
			os.Remove(lockPath)
		}
		lockMu.Unlock()
	}()

	fmt.Printf("\n%s- Processing downloads (Workers: %d)...%s\n", Yellow, cfg.Workers, Reset)
	limiter := NewRateLimiter(cfg.RateLimit)
	taskChan := make(chan Task, len(tasks))
	for _, t := range tasks {
		taskChan <- t
	}
	close(taskChan)

	var wg sync.WaitGroup
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					processDownloadTask(ctx, cfg, task, limiter, &activeLocks, &lockMu, workerID)
				}
			}
		}(i)
	}
	wg.Wait()

	if ctx.Err() != nil {
		fmt.Printf("\n%s[!] Execution terminated cleanly following interrupt.%s\n", Yellow, Reset)
	}
}

func runUpdateMode(ctx context.Context, cfg *Config, remoteZims []ZimMetaData) ([]Task, error) {
	fmt.Printf("%s- Scanning local ZIM files...%s\n", Blue, Reset)
	scanStart := time.Now()

	localEntries, err := os.ReadDir(cfg.ZimDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	type ScanItem struct {
		LocalFile  string
		LocalSize  int64
		LocalBase  string
		LocalYear  int
		LocalMonth int
		Matched    ZimMetaData
	}

	var scanItems []ScanItem
	for _, entry := range localEntries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zim") || strings.HasPrefix(entry.Name(), ".~lock") || strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}

		localFile := entry.Name()
		info, err := entry.Info()
		if err != nil {
			continue
		}
		localBase, localYear, localMonth := parseZimName(localFile)

		var matched *ZimMetaData
		for _, r := range remoteZims {
			if r.BaseName == localBase {
				matched = &r
				break
			}
		}

		if matched == nil {
			fmt.Printf("  %s %s (No remote match found)\n", Yellow, localFile)
			continue
		}

		scanItems = append(scanItems, ScanItem{
			LocalFile:  localFile,
			LocalSize:  info.Size(),
			LocalBase:  localBase,
			LocalYear:  localYear,
			LocalMonth: localMonth,
			Matched:    *matched,
		})
	}

	var tasks []Task

	if !cfg.CheckSize {
		for _, item := range scanItems {
			newer := isRemoteNewer(item.Matched, item.LocalYear, item.LocalMonth)
			if newer {
				fmt.Printf("  %s Update found for %s -> %s\n", Green, item.LocalFile, item.Matched.FileName)
				tasks = append(tasks, Task{LocalFile: item.LocalFile, Remote: item.Matched})
			} else {
				fmt.Printf("  %s Up to date: %s\n", Green, item.LocalFile)
			}
		}
	} else {
		type Result struct {
			Item       ScanItem
			RemoteSize int64
			IsNewer    bool
			Err        error
			Duration   time.Duration
		}

		itemChan := make(chan ScanItem, len(scanItems))
		resultChan := make(chan Result, len(scanItems))

		for _, item := range scanItems {
			itemChan <- item
		}
		close(itemChan)

		numScanWorkers := 8
		var scanWg sync.WaitGroup

		for w := 0; w < numScanWorkers; w++ {
			scanWg.Add(1)
			go func() {
				defer scanWg.Done()
				for item := range itemChan {
					lookupStart := time.Now()
					newer := isRemoteNewer(item.Matched, item.LocalYear, item.LocalMonth)
					remoteSize, err := getRemoteFileSize(ctx, item.Matched.URL)

					resultChan <- Result{
						Item:       item,
						RemoteSize: remoteSize,
						IsNewer:    newer,
						Err:        err,
						Duration:   time.Since(lookupStart),
					}
				}
			}()
		}

		go func() {
			scanWg.Wait()
			close(resultChan)
		}()

		var printMu sync.Mutex
		for res := range resultChan {
			item := res.Item
			if res.RemoteSize > 0 {
				item.Matched.Size = res.RemoteSize
			}

			printMu.Lock()
			if res.IsNewer {
				fmt.Printf("  %s Update found for %s -> %s [%v]%s\n",
					Green, item.LocalFile, item.Matched.FileName, res.Duration.Round(time.Millisecond), Reset)
				tasks = append(tasks, Task{LocalFile: item.LocalFile, Remote: item.Matched})
			} else if res.Err == nil && res.RemoteSize > 0 && absDiff(item.LocalSize, res.RemoteSize) > 1024 {
				fmt.Printf("  %s Size mismatch / corrupted file detected: %s (Local: %.1f MiB, Remote: %.1f MiB) [%v]%s\n",
					Red, item.LocalFile, float64(item.LocalSize)/(1024*1024), float64(res.RemoteSize)/(1024*1024), res.Duration.Round(time.Millisecond), Reset)
				tasks = append(tasks, Task{LocalFile: item.LocalFile, Remote: item.Matched})
			} else {
				fmt.Printf("  %s Up to date: %s [%v]%s\n",
					Green, item.LocalFile, res.Duration.Round(time.Millisecond), Reset)
			}
			printMu.Unlock()
		}
	}

	totalScanTime := time.Since(scanStart)
	fmt.Printf("%s  ✓ Scanned %d local ZIM files in %v%s\n\n", Green, len(scanItems), totalScanTime.Round(time.Millisecond), Reset)

	return tasks, nil
}