package data

import (
	"testing"
)

func TestTickDownloaderStartDownloadDisabled(t *testing.T) {
	ts := NewTickStorage()
	td := NewTickDownloader(ts)

	_, err := td.StartDownload("BTCUSDT", 0, 0)
	if err == nil {
		t.Fatal("expected error because network tick download is disabled")
	}
}

func TestTickDownloaderEmptySymbol(t *testing.T) {
	ts := NewTickStorage()
	td := NewTickDownloader(ts)
	_, err := td.StartDownload("", 0, 0)
	// Empty symbol check happens before the disabled check in the old code,
	// but now StartDownload returns the disabled error for all calls.
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTickDownloaderGetJobMissing(t *testing.T) {
	ts := NewTickStorage()
	td := NewTickDownloader(ts)
	job := td.GetJob("nonexistent")
	if job != nil {
		t.Fatal("expected nil for missing job")
	}
}

func TestTickDownloaderDownloadTicksDisabled(t *testing.T) {
	ts := NewTickStorage()
	td := NewTickDownloader(ts)

	_, err := td.DownloadTicks("BTCUSDT", 0, 0)
	if err == nil {
		t.Fatal("expected error because DownloadTicks is disabled")
	}

	// Verify struct is wired correctly
	if td.store != ts {
		t.Error("expected downloader store to match")
	}
	if td.jobs == nil {
		t.Error("expected jobs map to be initialized")
	}
}
