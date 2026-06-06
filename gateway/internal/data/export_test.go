package data

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExporterCSV(t *testing.T) {
	initTestDB(t)
	store := NewStorage()
	exporter := NewExporter(store)

	// Seed data
	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704067200000, Open: 42000, High: 43000, Low: 41000, Close: 42500, Volume: 1000},
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704070800000, Open: 42500, High: 43500, Low: 42000, Close: 43000, Volume: 1200},
	}
	store.SaveOHLCV(bars)

	tmpDir := t.TempDir()
	cfg := ExportConfig{
		Symbol:   "BTCUSDT",
		Interval: "1h",
		Format:   FormatCSV,
		OutPath:  filepath.Join(tmpDir, "test.csv"),
	}

	result, err := exporter.Export(cfg)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if !result.Success {
		t.Fatal("export not successful")
	}
	if result.Records != 2 {
		t.Errorf("expected 2 records, got %d", result.Records)
	}

	// Verify file content
	data, err := os.ReadFile(result.FilePath)
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least 3 lines (header + 2 data), got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "symbol,interval,time") {
		t.Errorf("expected CSV header, got: %s", lines[0])
	}
}

func TestExporterJSON(t *testing.T) {
	initTestDB(t)
	store := NewStorage()
	exporter := NewExporter(store)

	bars := []OHLCV{
		{Symbol: "ETHUSDT", Interval: "1d", Time: 1704067200000, Open: 2200, High: 2300, Low: 2100, Close: 2250, Volume: 5000},
	}
	store.SaveOHLCV(bars)

	tmpDir := t.TempDir()
	cfg := ExportConfig{
		Symbol:   "ETHUSDT",
		Interval: "1d",
		Format:   FormatJSON,
		OutPath:  filepath.Join(tmpDir, "test.json"),
	}

	result, err := exporter.Export(cfg)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if result.Records != 1 {
		t.Errorf("expected 1 record, got %d", result.Records)
	}

	// Verify JSON array
	data, _ := os.ReadFile(result.FilePath)
	var parsed []OHLCV
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json parse failed: %v", err)
	}
	if len(parsed) != 1 {
		t.Errorf("expected 1 item in JSON array, got %d", len(parsed))
	}
	if parsed[0].Close != 2250 {
		t.Errorf("expected close 2250, got %f", parsed[0].Close)
	}
}

func TestExporterJSONLines(t *testing.T) {
	initTestDB(t)
	store := NewStorage()
	exporter := NewExporter(store)

	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704067200000, Open: 42000, High: 43000, Low: 41000, Close: 42500, Volume: 1000},
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704070800000, Open: 42500, High: 43500, Low: 42000, Close: 43000, Volume: 1200},
	}
	store.SaveOHLCV(bars)

	tmpDir := t.TempDir()
	cfg := ExportConfig{
		Symbol:   "BTCUSDT",
		Interval: "1h",
		Format:   FormatJSONLines,
		OutPath:  filepath.Join(tmpDir, "test.jsonl"),
	}

	result, err := exporter.Export(cfg)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if result.Records != 2 {
		t.Errorf("expected 2 records, got %d", result.Records)
	}

	// Verify each line is valid JSON
	data, _ := os.ReadFile(result.FilePath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSON lines, got %d", len(lines))
	}
	for i, line := range lines {
		var bar OHLCV
		if err := json.Unmarshal([]byte(line), &bar); err != nil {
			t.Fatalf("line %d not valid JSON: %v", i, err)
		}
	}
}

func TestExporterNoData(t *testing.T) {
	initTestDB(t)
	store := NewStorage()
	exporter := NewExporter(store)

	cfg := ExportConfig{
		Symbol:   "NONEXISTENT",
		Interval: "1h",
		Format:   FormatCSV,
		OutPath:  filepath.Join(t.TempDir(), "empty.csv"),
	}

	result, err := exporter.Export(cfg)
	if err == nil {
		t.Error("expected error for no data")
	}
	if result.Success {
		t.Error("expected unsuccessful result")
	}
}

func TestExporterDefaultPath(t *testing.T) {
	initTestDB(t)
	store := NewStorage()
	exporter := NewExporter(store)

	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704067200000, Open: 42000, High: 43000, Low: 41000, Close: 42500, Volume: 1000},
	}
	store.SaveOHLCV(bars)

	cfg := ExportConfig{
		Symbol:   "BTCUSDT",
		Interval: "1h",
		Format:   FormatCSV,
	}

	result, err := exporter.Export(cfg)
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if result.FilePath == "" {
		t.Error("expected auto-generated file path")
	}
	if _, err := os.Stat(result.FilePath); os.IsNotExist(err) {
		t.Error("file should exist")
	}
}

func TestExporterToWriter(t *testing.T) {
	initTestDB(t)
	store := NewStorage()
	exporter := NewExporter(store)

	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704067200000, Open: 42000, High: 43000, Low: 41000, Close: 42500, Volume: 1000},
	}
	store.SaveOHLCV(bars)

	var buf bytes.Buffer
	cfg := ExportConfig{
		Symbol:   "BTCUSDT",
		Interval: "1h",
		Format:   FormatCSV,
	}

	records, err := exporter.ExportToWriter(&buf, cfg)
	if err != nil {
		t.Fatalf("export to writer failed: %v", err)
	}
	if records != 2 { // header + 1 data row
		t.Errorf("expected 2 records, got %d", records)
	}

	// Verify CSV content
	reader := csv.NewReader(&buf)
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("csv read failed: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
	if rows[0][0] != "symbol" {
		t.Errorf("expected 'symbol' header, got %s", rows[0][0])
	}
}

func TestSupportedFormats(t *testing.T) {
	formats := SupportedFormats()
	if len(formats) != 3 {
		t.Errorf("expected 3 formats, got %d", len(formats))
	}
	seen := make(map[string]bool)
	for _, f := range formats {
		seen[f] = true
	}
	if !seen["csv"] || !seen["json"] || !seen["jsonl"] {
		t.Error("expected csv, json, jsonl")
	}
}
