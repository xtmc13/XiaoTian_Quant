package experiment

import (
	"errors"
	"strings"
	"testing"
)

func TestRunExperimentBarsProviderFallback(t *testing.T) {
	defer SetBarsProvider(nil)
	SetBarsProvider(func(symbol, interval string) ([]map[string]any, error) {
		return nil, errors.New("feed down")
	})
	_, err := RunExperiment(ExperimentRequest{
		Code: "# @param fast int 12 快线 range=5:50:1\noutput = {'name':'t'}",
	})
	if err == nil || !strings.Contains(err.Error(), "no bar data provided") {
		t.Fatalf("expected provider-backed no-data error, got %v", err)
	}

	SetBarsProvider(func(symbol, interval string) ([]map[string]any, error) {
		return []map[string]any{
			{"timestamp": float64(1), "open": 1.0, "high": 1.0, "low": 1.0, "close": 1.0, "volume": 1.0},
		}, nil
	})
	// provider 返回 1 根 bar 也应被消费（不再报 no bar data provided）
	res, err := RunExperiment(ExperimentRequest{
		Code: "# @param fast int 12 快线 range=5:50:1\noutput = {'name':'t'}",
	})
	if err != nil || res == nil {
		t.Fatalf("provider data not consumed: res=%v err=%v", res, err)
	}
}
