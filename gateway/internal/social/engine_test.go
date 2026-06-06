package social

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSignal_IsExpired(t *testing.T) {
	now := time.Now().UnixMilli()

	s1 := Signal{ID: "1", ExpiresAt: now + 10000}
	assert.False(t, s1.IsExpired())

	s2 := Signal{ID: "2", ExpiresAt: now - 1000}
	assert.True(t, s2.IsExpired())

	s3 := Signal{ID: "3", ExpiresAt: 0}
	assert.False(t, s3.IsExpired())
}

func TestDefaultCopyConfig(t *testing.T) {
	cfg := DefaultCopyConfig(100, 200)
	assert.Equal(t, 100, cfg.FollowerID)
	assert.Equal(t, 200, cfg.ProviderID)
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 1.0, cfg.Multiplier)
	assert.Equal(t, 0.1, cfg.MaxPosition)
	assert.Equal(t, 0.05, cfg.MaxDailyLoss)
	assert.Equal(t, 0.5, cfg.SlippagePct)
	assert.False(t, cfg.AutoExecute)
}

func TestEngine_RegisterProvider(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 29.99, true)

	stats := eng.GetProviderStats(1)
	assert.NotNil(t, stats)
	assert.Equal(t, 1, stats.ProviderID)
	assert.Equal(t, 29.99, stats.MonthlyFee)
	assert.True(t, stats.IsPublic)
	assert.Equal(t, 0, stats.FollowerCount)
}

func TestEngine_Follow(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)

	cfg := DefaultCopyConfig(100, 1)
	err := eng.Follow(cfg)
	assert.NoError(t, err)

	stats := eng.GetProviderStats(1)
	assert.Equal(t, 1, stats.FollowerCount)

	// Follower configs
	configs := eng.GetFollowerConfigs(100)
	assert.Len(t, configs, 1)
	assert.Equal(t, 1, configs[0].ProviderID)
}

func TestEngine_Follow_PrivateProvider(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, false)

	cfg := DefaultCopyConfig(100, 1)
	err := eng.Follow(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not public")
}

func TestEngine_Follow_ProviderNotFound(t *testing.T) {
	eng := NewEngine()
	cfg := DefaultCopyConfig(100, 999)
	err := eng.Follow(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestEngine_Unfollow(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)
	eng.Follow(DefaultCopyConfig(100, 1))

	eng.Unfollow(100, 1)

	stats := eng.GetProviderStats(1)
	assert.Equal(t, 0, stats.FollowerCount)

	configs := eng.GetFollowerConfigs(100)
	assert.Len(t, configs, 0)
}

func TestEngine_PublishSignal(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)
	eng.Follow(DefaultCopyConfig(100, 1))

	var received []Signal
	eng.OnSignal = func(s Signal) {
		received = append(received, s)
	}

	var copied []struct {
		followerID int
		signal     Signal
	}
	eng.OnCopyTrade = func(fid int, s Signal, cfg CopyConfig) {
		copied = append(copied, struct {
			followerID int
			signal     Signal
		}{fid, s})
	}

	sig := Signal{
		ID:         "sig-1",
		ProviderID: 1,
		Symbol:     "BTCUSDT",
		Direction:  "buy",
		Price:      50000,
		Size:       0.05,
		Confidence: 85,
		Timestamp:  time.Now().UnixMilli(),
		ExpiresAt:  time.Now().Add(time.Hour).UnixMilli(),
	}
	eng.PublishSignal(sig)

	assert.Len(t, received, 1)
	assert.Equal(t, "sig-1", received[0].ID)
	assert.Len(t, copied, 1)
	assert.Equal(t, 100, copied[0].followerID)

	// Provider stats updated
	stats := eng.GetProviderStats(1)
	assert.Equal(t, 1, stats.TotalSignals)
}

func TestEngine_PublishSignal_Expired(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)
	eng.Follow(DefaultCopyConfig(100, 1))

	var copied int
	eng.OnCopyTrade = func(fid int, s Signal, cfg CopyConfig) {
		copied++
	}

	sig := Signal{
		ID:         "sig-1",
		ProviderID: 1,
		Symbol:     "BTCUSDT",
		Direction:  "buy",
		Price:      50000,
		Timestamp:  time.Now().UnixMilli(),
		ExpiresAt:  time.Now().Add(-time.Hour).UnixMilli(),
	}
	eng.PublishSignal(sig)

	assert.Equal(t, 0, copied)
}

func TestEngine_PublishSignal_DisabledFollower(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)
	cfg := DefaultCopyConfig(100, 1)
	cfg.Enabled = false
	eng.Follow(cfg)

	var copied int
	eng.OnCopyTrade = func(fid int, s Signal, cfg CopyConfig) {
		copied++
	}

	sig := Signal{
		ID:         "sig-1",
		ProviderID: 1,
		Symbol:     "BTCUSDT",
		Direction:  "buy",
		Price:      50000,
		Timestamp:  time.Now().UnixMilli(),
		ExpiresAt:  time.Now().Add(time.Hour).UnixMilli(),
	}
	eng.PublishSignal(sig)

	assert.Equal(t, 0, copied)
}

func TestEngine_PublishSignal_SymbolFilter(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)
	cfg := DefaultCopyConfig(100, 1)
	cfg.Symbols = []string{"ETHUSDT"}
	eng.Follow(cfg)

	var copied int
	eng.OnCopyTrade = func(fid int, s Signal, cfg CopyConfig) {
		copied++
	}

	sig := Signal{
		ID:         "sig-1",
		ProviderID: 1,
		Symbol:     "BTCUSDT",
		Direction:  "buy",
		Price:      50000,
		Timestamp:  time.Now().UnixMilli(),
		ExpiresAt:  time.Now().Add(time.Hour).UnixMilli(),
	}
	eng.PublishSignal(sig)

	assert.Equal(t, 0, copied)
}

func TestEngine_RiskCheck_PositionSize(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)
	cfg := DefaultCopyConfig(100, 1)
	cfg.MaxPosition = 0.05 // 5% max
	eng.Follow(cfg)

	var blocked []string
	eng.OnRiskBlock = func(fid int, reason string) {
		blocked = append(blocked, reason)
	}

	sig := Signal{
		ID:         "sig-1",
		ProviderID: 1,
		Symbol:     "BTCUSDT",
		Direction:  "buy",
		Price:      50000,
		Size:       0.1, // 10% with 1x multiplier = exceeds 5%
		Timestamp:  time.Now().UnixMilli(),
		ExpiresAt:  time.Now().Add(time.Hour).UnixMilli(),
	}
	eng.PublishSignal(sig)

	assert.Len(t, blocked, 1)
	assert.Contains(t, blocked[0], "exceeds max")
}

func TestEngine_GetProviderSignals(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)

	now := time.Now().UnixMilli()
	for i := 0; i < 5; i++ {
		eng.PublishSignal(Signal{
			ID:         fmt.Sprintf("sig-%d", i),
			ProviderID: 1,
			Symbol:     "BTCUSDT",
			Direction:  "buy",
			Price:      float64(50000 + i),
			Timestamp:  now,
			ExpiresAt:  now + 3600000,
		})
	}

	signals := eng.GetProviderSignals(1, 3)
	assert.Len(t, signals, 3)
	assert.Equal(t, "sig-4", signals[0].ID)
	assert.Equal(t, "sig-3", signals[1].ID)
	assert.Equal(t, "sig-2", signals[2].ID)
}

func TestEngine_GetPublicProviders(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 29.99, true)
	eng.RegisterProvider(2, 0, false)
	eng.RegisterProvider(3, 49.99, true)

	public := eng.GetPublicProviders()
	assert.Len(t, public, 2)

	ids := make(map[int]bool)
	for _, p := range public {
		ids[p.ProviderID] = true
	}
	assert.True(t, ids[1])
	assert.True(t, ids[3])
	assert.False(t, ids[2])
}

func TestEngine_MaxSignalsBuffer(t *testing.T) {
	eng := NewEngine()
	eng.maxSignals = 3
	eng.RegisterProvider(1, 0, true)

	now := time.Now().UnixMilli()
	for i := 0; i < 5; i++ {
		eng.PublishSignal(Signal{
			ID:         fmt.Sprintf("sig-%d", i),
			ProviderID: 1,
			Symbol:     "BTCUSDT",
			Direction:  "buy",
			Timestamp:  now,
			ExpiresAt:  now + 3600000,
		})
	}

	signals := eng.GetProviderSignals(1, 10)
	assert.Len(t, signals, 3)
	assert.Equal(t, "sig-4", signals[0].ID)
	assert.Equal(t, "sig-3", signals[1].ID)
	assert.Equal(t, "sig-2", signals[2].ID)
}

func TestEngine_MultipleFollowers(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)
	eng.Follow(DefaultCopyConfig(100, 1))
	eng.Follow(DefaultCopyConfig(101, 1))
	eng.Follow(DefaultCopyConfig(102, 1))

	var copied []int
	eng.OnCopyTrade = func(fid int, s Signal, cfg CopyConfig) {
		copied = append(copied, fid)
	}

	sig := Signal{
		ID:         "sig-1",
		ProviderID: 1,
		Symbol:     "BTCUSDT",
		Direction:  "buy",
		Price:      50000,
		Timestamp:  time.Now().UnixMilli(),
		ExpiresAt:  time.Now().Add(time.Hour).UnixMilli(),
	}
	eng.PublishSignal(sig)

	assert.Len(t, copied, 3)
	assert.Contains(t, copied, 100)
	assert.Contains(t, copied, 101)
	assert.Contains(t, copied, 102)
}

func TestEngine_MultipleProviders(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)
	eng.RegisterProvider(2, 0, true)
	eng.Follow(DefaultCopyConfig(100, 1))
	eng.Follow(DefaultCopyConfig(100, 2))

	var copied []struct {
		followerID int
		providerID int
	}
	eng.OnCopyTrade = func(fid int, s Signal, cfg CopyConfig) {
		copied = append(copied, struct {
			followerID int
			providerID int
		}{fid, s.ProviderID})
	}

	eng.PublishSignal(Signal{
		ID:         "sig-1",
		ProviderID: 1,
		Symbol:     "BTCUSDT",
		Direction:  "buy",
		Timestamp:  time.Now().UnixMilli(),
		ExpiresAt:  time.Now().Add(time.Hour).UnixMilli(),
	})
	eng.PublishSignal(Signal{
		ID:         "sig-2",
		ProviderID: 2,
		Symbol:     "ETHUSDT",
		Direction:  "sell",
		Timestamp:  time.Now().UnixMilli(),
		ExpiresAt:  time.Now().Add(time.Hour).UnixMilli(),
	})

	assert.Len(t, copied, 2)
	assert.Equal(t, 100, copied[0].followerID)
	assert.Equal(t, 1, copied[0].providerID)
	assert.Equal(t, 100, copied[1].followerID)
	assert.Equal(t, 2, copied[1].providerID)
}

func TestEngine_FollowerConfigUpdate(t *testing.T) {
	eng := NewEngine()
	eng.RegisterProvider(1, 0, true)
	cfg := DefaultCopyConfig(100, 1)
	cfg.Multiplier = 2.0
	eng.Follow(cfg)

	configs := eng.GetFollowerConfigs(100)
	assert.Len(t, configs, 1)
	assert.Equal(t, 2.0, configs[0].Multiplier)

	// Update config
	cfg2 := DefaultCopyConfig(100, 1)
	cfg2.Multiplier = 0.5
	eng.Follow(cfg2)

	configs = eng.GetFollowerConfigs(100)
	assert.Len(t, configs, 1)
	assert.Equal(t, 0.5, configs[0].Multiplier)
}

func TestContains(t *testing.T) {
	assert.True(t, contains([]string{"a", "b", "c"}, "b"))
	assert.False(t, contains([]string{"a", "b", "c"}, "d"))
	assert.False(t, contains([]string{}, "a"))
}
