package agenttelegram

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// Link 一条 Telegram ↔ 平台用户绑定。
type Link struct {
	UserID           int64  `json:"user_id"`
	TelegramChatID   int64  `json:"telegram_chat_id"`
	TelegramUsername string `json:"telegram_username"`
	CreatedAt        int64  `json:"created_at"`
}

// Repo 绑定与配对码存储。
type Repo struct{}

// NewRepo 基于网关默认 SQLite。
func NewRepo() *Repo { return &Repo{} }

func (r *Repo) db() (*sql.DB, error) {
	db := store.GetDB()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	return db, nil
}

// ErrNotLinked / ErrCodeInvalid 业务错误。
var (
	ErrNotLinked   = errors.New("未绑定 Telegram")
	ErrCodeInvalid = errors.New("配对码无效或已过期")
)

// ── 配对码 ──

// PairCodeTTL 配对码有效期。
const PairCodeTTL = 10 * time.Minute

// CreatePairCode 生成 6 位数字配对码（同用户旧码作废）。
func (r *Repo) CreatePairCode(userID int64) (string, error) {
	db, err := r.db()
	if err != nil {
		return "", err
	}
	// 作废旧码
	if _, err := db.Exec(`UPDATE xt_agent_telegram_pair_codes SET used_at = ? WHERE user_id = ? AND used_at = 0`, time.Now().Unix(), userID); err != nil {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64()+100000)
	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO xt_agent_telegram_pair_codes (code, user_id, created_at, expires_at, used_at) VALUES (?,?,?,?,0)`,
		code, userID, now, now+int64(PairCodeTTL.Seconds()))
	return code, err
}

// ConsumePairCode 校验并消费配对码：成功则建立绑定（telegram_chat_id 换绑到新用户）。
func (r *Repo) ConsumePairCode(code string, chatID int64, username string) (*Link, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var userID int64
	var expiresAt, usedAt int64
	err = db.QueryRow(`SELECT user_id, expires_at, used_at FROM xt_agent_telegram_pair_codes WHERE code = ?`, code).
		Scan(&userID, &expiresAt, &usedAt)
	if err == sql.ErrNoRows {
		return nil, ErrCodeInvalid
	}
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if usedAt != 0 || now > expiresAt {
		return nil, ErrCodeInvalid
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE xt_agent_telegram_pair_codes SET used_at = ? WHERE code = ?`, now, code); err != nil {
		return nil, err
	}
	// chat_id 若绑过别人，先解绑
	if _, err := tx.Exec(`DELETE FROM xt_agent_telegram_links WHERE telegram_chat_id = ?`, chatID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO xt_agent_telegram_links (user_id, telegram_chat_id, telegram_username, created_at) VALUES (?,?,?,?)`,
		userID, chatID, username, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Link{UserID: userID, TelegramChatID: chatID, TelegramUsername: username, CreatedAt: now}, nil
}

// ── 绑定 ──

// GetByUserID 查用户绑定。
func (r *Repo) GetByUserID(userID int64) (*Link, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var l Link
	err = db.QueryRow(`SELECT user_id, telegram_chat_id, telegram_username, created_at FROM xt_agent_telegram_links WHERE user_id = ?`, userID).
		Scan(&l.UserID, &l.TelegramChatID, &l.TelegramUsername, &l.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotLinked
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// GetByChatID 查 chat 绑定（入站消息路由）。
func (r *Repo) GetByChatID(chatID int64) (*Link, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var l Link
	err = db.QueryRow(`SELECT user_id, telegram_chat_id, telegram_username, created_at FROM xt_agent_telegram_links WHERE telegram_chat_id = ?`, chatID).
		Scan(&l.UserID, &l.TelegramChatID, &l.TelegramUsername, &l.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotLinked
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// Unlink 解绑。
func (r *Repo) Unlink(userID int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM xt_agent_telegram_links WHERE user_id = ?`, userID)
	return err
}
