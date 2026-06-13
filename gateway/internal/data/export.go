package data

import (
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// ── Export ───────────────────────────────────────────────────────

// ExportFormat defines the output format for data export.
type ExportFormat string

const (
	FormatCSV       ExportFormat = "csv"
	FormatJSON      ExportFormat = "json"
	FormatJSONLines ExportFormat = "jsonl" // ndjson, one object per line
)

// ExportConfig configures a data export operation.
type ExportConfig struct {
	Symbol    string       `json:"symbol"`
	Interval  string       `json:"interval"`
	FromMs    int64        `json:"from_ms"`
	ToMs      int64        `json:"to_ms"`
	Format    ExportFormat `json:"format"`
	OutPath   string       `json:"out_path"`   // output file path (optional)
	Compress  bool         `json:"compress"`   // gzip compression (not yet implemented)
}

// ExportResult holds the outcome of a data export.
type ExportResult struct {
	Success   bool   `json:"success"`
	Format    string `json:"format"`
	Records   int    `json:"records"`
	FilePath  string `json:"file_path"`
	Bytes     int64  `json:"bytes"`
	Error     string `json:"error,omitempty"`
}

// Exporter handles data export in multiple formats.
type Exporter struct {
	store *Storage
}

// NewExporter creates a new data exporter.
func NewExporter(store *Storage) *Exporter {
	return &Exporter{store: store}
}

// Export writes OHLCV data to a file in the specified format.
func (e *Exporter) Export(cfg ExportConfig) (*ExportResult, error) {
	result := &ExportResult{Format: string(cfg.Format)}

	// Load data
	bars := e.store.LoadOHLCV(cfg.Symbol, cfg.Interval, cfg.FromMs, cfg.ToMs)
	if len(bars) == 0 {
		result.Error = "no data found for the given range"
		return result, fmt.Errorf("no data: %s %s", cfg.Symbol, cfg.Interval)
	}

	// Determine output path
	outPath := cfg.OutPath
	if outPath == "" {
		outPath = e.defaultPath(cfg)
	}

	// Ensure directory exists
	dir := filepath.Dir(outPath)
	if dir != "." && dir != "" {
		os.MkdirAll(dir, 0755)
	}

	// Write based on format
	var err error
	switch cfg.Format {
	case FormatCSV:
		err = e.writeCSV(outPath, bars)
	case FormatJSON:
		err = e.writeJSON(outPath, bars)
	case FormatJSONLines:
		err = e.writeJSONLines(outPath, bars)
	default:
		err = fmt.Errorf("unsupported format: %s", cfg.Format)
	}

	if err != nil {
		result.Error = err.Error()
		return result, err
	}

	// Apply gzip compression if requested
	if cfg.Compress {
		var gzName string
		gzName, err = compressToGzip(outPath)
		if err == nil {
			os.Remove(outPath) // remove original uncompressed file
			outPath = gzName
		}
	}

	// Get file size
	info, _ := os.Stat(outPath)
	var size int64
	if info != nil {
		size = info.Size()
	}

	result.Success = true
	result.Records = len(bars)
	result.FilePath = outPath
	result.Bytes = size
	return result, nil
}

// compressToGzip creates a gzip-compressed copy of the source file and returns the gz path.
func compressToGzip(srcPath string) (string, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return "", err
	}
	defer src.Close()

	gzPath := srcPath + ".gz"
	dst, err := os.Create(gzPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	gw := gzip.NewWriter(dst)
	defer gw.Close()

	if _, err := io.Copy(gw, src); err != nil {
		return "", err
	}
	return gzPath, nil
}

// ExportToWriter writes data to an io.Writer (useful for HTTP responses).
func (e *Exporter) ExportToWriter(w io.Writer, cfg ExportConfig) (int, error) {
	bars := e.store.LoadOHLCV(cfg.Symbol, cfg.Interval, cfg.FromMs, cfg.ToMs)
	if len(bars) == 0 {
		return 0, fmt.Errorf("no data found")
	}

	switch cfg.Format {
	case FormatCSV:
		return e.writeCSVToWriter(w, bars)
	case FormatJSON:
		return e.writeJSONToWriter(w, bars)
	case FormatJSONLines:
		return e.writeJSONLinesToWriter(w, bars)
	default:
		return 0, fmt.Errorf("unsupported format: %s", cfg.Format)
	}
}

// ── CSV ────────────────────────────────────────────────────────

func (e *Exporter) writeCSV(path string, bars []OHLCV) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = e.writeCSVToWriter(f, bars)
	return err
}

func (e *Exporter) writeCSVToWriter(w io.Writer, bars []OHLCV) (int, error) {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header
	if err := cw.Write([]string{"symbol", "interval", "time", "open", "high", "low", "close", "volume"}); err != nil {
		return 0, err
	}

	records := 1 // header
	for _, b := range bars {
		row := []string{
			b.Symbol,
			b.Interval,
			strconv.FormatInt(b.Time, 10),
			fmt.Sprintf("%.8f", b.Open),
			fmt.Sprintf("%.8f", b.High),
			fmt.Sprintf("%.8f", b.Low),
			fmt.Sprintf("%.8f", b.Close),
			fmt.Sprintf("%.4f", b.Volume),
		}
		if err := cw.Write(row); err != nil {
			return records, err
		}
		records++
	}
	return records, nil
}

// ── JSON ─────────────────────────────────────────────────────

func (e *Exporter) writeJSON(path string, bars []OHLCV) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = e.writeJSONToWriter(f, bars)
	return err
}

func (e *Exporter) writeJSONToWriter(w io.Writer, bars []OHLCV) (int, error) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(bars); err != nil {
		return 0, err
	}
	return len(bars), nil
}

// ── JSON Lines (ndjson) ────────────────────────────────────────

func (e *Exporter) writeJSONLines(path string, bars []OHLCV) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = e.writeJSONLinesToWriter(f, bars)
	return err
}

func (e *Exporter) writeJSONLinesToWriter(w io.Writer, bars []OHLCV) (int, error) {
	enc := json.NewEncoder(w)
	count := 0
	for _, b := range bars {
		if err := enc.Encode(b); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// ── Helpers ────────────────────────────────────────────────────

func (e *Exporter) defaultPath(cfg ExportConfig) string {
	ts := time.Now().Format("20060102_150405")
	ext := string(cfg.Format)
	fromStr := ""
	toStr := ""
	if cfg.FromMs > 0 {
		fromStr = fmt.Sprintf("_%d", cfg.FromMs/1000)
	}
	if cfg.ToMs > 0 {
		toStr = fmt.Sprintf("_%d", cfg.ToMs/1000)
	}
	return fmt.Sprintf("data_export/%s_%s%s%s_%s.%s",
		cfg.Symbol, cfg.Interval, fromStr, toStr, ts, ext)
}

// SupportedFormats returns all supported export formats.
func SupportedFormats() []string {
	return []string{"csv", "json", "jsonl"}
}
