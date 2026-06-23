package store

import "testing"

func TestArbitrageTradeRepoNoDB(t *testing.T) {
	repo := NewArbitrageTradeRepo()
	// Without a database connection, operations may error but must not panic.
	_ = repo.Create(&ArbitrageTradeRecord{ID: "test", Symbol: "BTCUSDT"})
	_, _ = repo.GetByID("test")
	_, _ = repo.ListActive()
	_, _ = repo.ListHistory(10)
}
