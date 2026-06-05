package data

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func initTestDB(t *testing.T) {
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db failed: %v", err)
	}
	// Clean ohlcv_data table before each test
	db := store.GetDB()
	if db != nil {
		db.Exec("DELETE FROM ohlcv_data")
		db.Exec("DELETE FROM ohlcv_download_log")
	}
}

func TestOHLCVToBar(t *testing.T) {
	o := OHLCV{
		Symbol:   "BTCUSDT",
		Interval: "1h",
		Time:     1704067200000,
		Open:     42000,
		High:     43000,
		Low:      41000,
		Close:    42500,
		Volume:   1000,
	}

	bar := o.ToBar()
	if bar.Symbol != "BTCUSDT" {
		t.Errorf("expected symbol BTCUSDT, got %s", bar.Symbol)
	}
	if bar.Close != 42500 {
		t.Errorf("expected close 42500, got %f", bar.Close)
	}
	if bar.Time != 1704067200000 {
		t.Errorf("expected time 1704067200000, got %d", bar.Time)
	}
}

func TestDownloaderLoadBarsForBacktest(t *testing.T) {
	initTestDB(t)
	store := NewStorage()
	d := NewDownloader(store)

	// Save some test data
	bars := []OHLCV{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704067200000, Open: 42000, High: 43000, Low: 41000, Close: 42500, Volume: 1000},
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704070800000, Open: 42500, High: 43500, Low: 42000, Close: 43000, Volume: 1200},
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704074400000, Open: 43000, High: 44000, Low: 42500, Close: 43500, Volume: 800},
	}
	_, err := store.SaveOHLCV(bars)
	if err != nil {
		t.Fatalf("save failed: %v", err)
	}

	// Load all
	loaded := d.LoadBarsForBacktest("BTCUSDT", "1h", 0, 0)
	if len(loaded) != 3 {
		t.Errorf("expected 3 bars, got %d", len(loaded))
	}

	// Load with time filter
	loaded = d.LoadBarsForBacktest("BTCUSDT", "1h", 1704070000000, 1704075000000)
	if len(loaded) != 2 {
		t.Errorf("expected 2 bars with filter, got %d", len(loaded))
	}
}

func TestDownloaderGetDataInfo(t *testing.T) {
	initTestDB(t)
	store := NewStorage()
	d := NewDownloader(store)

	// Empty info
	info := d.GetDataInfo("BTCUSDT", "1h")
	if info.Symbol != "BTCUSDT" || info.Interval != "1h" {
		t.Error("info symbol/interval mismatch")
	}
	if info.Count != 0 {
		t.Errorf("expected count 0, got %d", info.Count)
	}

	// Save data and check
	bars := []OHLCV{
		{Symbol: "ETHUSDT", Interval: "1d", Time: 1704067200000, Open: 2200, High: 2300, Low: 2100, Close: 2250, Volume: 5000},
	}
	_, _ = store.SaveOHLCV(bars)

	info = d.GetDataInfo("ETHUSDT", "1d")
	if info.Count != 1 {
		t.Errorf("expected count 1, got %d", info.Count)
	}
}

func TestDownloaderSaveBars(t *testing.T) {
	initTestDB(t)
	store := NewStorage()
	d := NewDownloader(store)

	bars := []model.Bar{
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704067200000, Open: 42000, High: 43000, Low: 41000, Close: 42500, Volume: 1000},
		{Symbol: "BTCUSDT", Interval: "1h", Time: 1704070800000, Open: 42500, High: 43500, Low: 42000, Close: 43000, Volume: 1200},
	}

	err := d.SaveBars(bars)
	if err != nil {
		t.Fatalf("save bars failed: %v", err)
	}

	loaded := d.LoadBarsForBacktest("BTCUSDT", "1h", 0, 0)
	if len(loaded) != 2 {
		t.Errorf("expected 2 bars after save, got %d", len(loaded))
	}
}

func TestDownloadConfigValidation(t *testing.T) {
	cfg := DownloadConfig{
		Symbols:   []string{"BTCUSDT"},
		Intervals: []string{"1h"},
		StartDate: "2024-01-01",
		EndDate:   "2024-01-31",
	}

	if len(cfg.Symbols) == 0 {
		t.Error("expected at least one symbol")
	}
	if len(cfg.Intervals) == 0 {
		t.Error("expected at least one interval")
	}

	_, err := time.Parse("2006-01-02", cfg.StartDate)
	if err != nil {
		t.Errorf("invalid start date: %v", err)
	}
}
