package marketplace

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 订阅实例化（迁移 0060 归因闭环的前半段）──────────────────────
// 订阅者把条目关联的源对象配置克隆成自己名下的新实例，盈亏发生在订阅者
// 账户，实例写 origin_listing_id 供平仓归因。AI 机器人实例的信号来自
// 订阅者自己的 per-user AI 扫描（xt_ai_signals 按 user_id），实例化语义是
// 配置模板克隆，不是作者信号的实时转发。初始均为 stopped + paper，由
// 订阅者自行启动/切换实盘。

var (
	// ErrSubscriptionNotActive 订阅非 active（API 层映射 409）。
	ErrSubscriptionNotActive = errors.New("subscription is not active")
	// ErrListingNotListed 条目非 listed 状态（API 层映射 409）。
	ErrListingNotListed = errors.New("listing is not listed")
	// ErrSourceObjectMissing 条目关联的源实例/机器人已不存在（API 层映射 409）。
	ErrSourceObjectMissing = errors.New("listing source object is gone")
)

// instantiateID 生成 AI 机器人实例 id（"aibot-" 前缀 + 8 位随机 hex，
// 与 handler.AIBotInstanceCreate 的 id 形态一致）；网格机器人 id 由
// store.GridRepo.Create 缺省分配，不走这里。
func instantiateID(prefix string) string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(b)
}

// Instantiate 订阅实例化：校验订阅属于 userID 且 active、条目仍 listed，
// 按条目 object_kind 克隆源对象配置创建订阅者名下新实例（origin_listing_id
// 溯源）。幂等：该用户对这条目已克隆过则返回既有实例（created=false）。
// 属主校验由调用方（handler）完成。
func (s *Service) Instantiate(userID int64, sub *store.MarketSubscriptionRecord) (kind, instanceID string, created bool, err error) {
	if sub.Status != store.MarketSubActive {
		return "", "", false, fmt.Errorf("%w: status=%s", ErrSubscriptionNotActive, sub.Status)
	}
	l := s.repo.GetByID(sub.ListingID)
	if l == nil {
		return "", "", false, ErrListingNotFound
	}
	if l.Status != StatusListed {
		return "", "", false, fmt.Errorf("%w: status=%s", ErrListingNotListed, l.Status)
	}
	switch l.ObjectKind {
	case "", ObjectKindAIBot:
		return s.instantiateAIBot(userID, l)
	case ObjectKindGridBot:
		return s.instantiateGridBot(userID, l)
	default:
		return "", "", false, fmt.Errorf("unsupported object_kind %q", l.ObjectKind)
	}
}

func (s *Service) instantiateAIBot(userID int64, l *store.MarketListingRecord) (string, string, bool, error) {
	if cur := store.FindAIBotInstanceByOrigin(int(userID), l.ID); cur != nil {
		return ObjectKindAIBot, getString(cur, "id", ""), false, nil
	}
	objectID := l.ObjectID
	if objectID == "" {
		objectID = l.BotInstanceID
	}
	src := store.GetAIBotInstanceByID(objectID, int(l.AuthorUserID))
	if src == nil {
		return "", "", false, ErrSourceObjectMissing
	}
	now := time.Now().Unix()
	inst := map[string]any{
		"id":                instantiateID("aibot"),
		"user_id":           int(userID),
		"catalog_id":        l.ID, // catalog 条目与 listing 同 id（approve 时 upsert）
		"name":              getString(src, "name", l.Name),
		"strategy_type":     getString(src, "strategy_type", ""),
		"symbol":            getString(src, "symbol", "BTCUSDT"),
		"market_type":       getString(src, "market_type", "spot"),
		"status":            "stopped",
		"execution_mode":    "paper", // 不继承作者的实盘/交易所绑定，订阅者自行切换
		"config_json":       getString(src, "config_json", "{}"),
		"exchange_id":       "",
		"initial_balance":   getFloat(src, "initial_balance", 10000),
		"origin_listing_id": l.ID,
		"created_at":        now,
		"updated_at":        now,
	}
	store.SaveAIBotInstance(inst)
	id := getString(inst, "id", "")
	if got := store.GetAIBotInstanceByID(id, int(userID)); got == nil {
		return "", "", false, fmt.Errorf("persist cloned ai bot instance failed")
	}
	return ObjectKindAIBot, id, true, nil
}

func (s *Service) instantiateGridBot(userID int64, l *store.MarketListingRecord) (string, string, bool, error) {
	if cur, err := s.gridRepo.GetByOrigin(userID, l.ID); err != nil {
		return "", "", false, err
	} else if cur != nil {
		return ObjectKindGridBot, cur.ID, false, nil
	}
	src, err := s.gridRepo.GetByIDForUser(l.ObjectID, l.AuthorUserID)
	if err != nil {
		return "", "", false, err
	}
	if src == nil {
		return "", "", false, ErrSourceObjectMissing
	}
	rec := &store.GridBotRecord{
		UserID:          userID,
		Name:            src.Name,
		Symbol:          src.Symbol,
		LowerPrice:      src.LowerPrice,
		UpperPrice:      src.UpperPrice,
		GridCount:       src.GridCount,
		Investment:      src.Investment,
		Status:          "stopped",
		Exchange:        "paper", // 不继承作者的交易所绑定，订阅者自行切换
		FeeRate:         src.FeeRate,
		Mode:            src.Mode,
		Leverage:        src.Leverage,
		MarginMode:      src.MarginMode,
		InitialEquity:   src.InitialEquity,
		OriginListingID: l.ID,
	}
	if err := s.gridRepo.Create(rec); err != nil {
		return "", "", false, err
	}
	return ObjectKindGridBot, rec.ID, true, nil
}
