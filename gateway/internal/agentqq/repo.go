package agentqq

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// Link 一条 QQ ↔ 平台用户绑定。
type Link struct {
	UserID int64  `json:"user_id"`
	OpenID string `json:"open_id"`
	// ChatType 最近一次会话类型：c2c（私聊）/ group（群聊）/ channel（频道）。
	ChatType string `json:"chat_type"`
	// ChatID 回发目标：c2c=user_openid，group=group_openid，channel=channel_id。
	ChatID    string `json:"chat_id"`
	CreatedAt int64  `json:"created_at"`
	// ConversationID 会话绑定；空 = 无会话（无状态执行）。
	ConversationID string `json:"conversation_id"`
	// Model 本绑定的模型覆盖（"provider" 或 "provider:model"）；空 = 跟随网关默认。
	Model string `json:"model"`
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
	ErrNotLinked   = errors.New("未绑定 QQ")
	ErrCodeInvalid = errors.New("配对码无效或已过期")
)

// PairCodeTTL 配对码有效期。
const PairCodeTTL = 10 * time.Minute

// CreatePairCode 生成 6 位数字配对码（同用户旧码作废）。
func (r *Repo) CreatePairCode(userID int64) (string, error) {
	db, err := r.db()
	if err != nil {
		return "", err
	}
	// 作废旧码
	if _, err := db.Exec(`UPDATE xt_agent_qq_pair_codes SET used_at = ? WHERE user_id = ? AND used_at = 0`, time.Now().Unix(), userID); err != nil {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64()+100000)
	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO xt_agent_qq_pair_codes (code, user_id, created_at, expires_at, used_at) VALUES (?,?,?,?,0)`,
		code, userID, now, now+int64(PairCodeTTL.Seconds()))
	return code, err
}

// ConsumePairCode 校验并消费配对码：成功则建立绑定（open_id 换绑到新用户）。
func (r *Repo) ConsumePairCode(code, openID, chatType, chatID string) (*Link, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var userID int64
	var expiresAt, usedAt int64
	err = db.QueryRow(`SELECT user_id, expires_at, used_at FROM xt_agent_qq_pair_codes WHERE code = ?`, code).
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
	if _, err := tx.Exec(`UPDATE xt_agent_qq_pair_codes SET used_at = ? WHERE code = ?`, now, code); err != nil {
		return nil, err
	}
	// open_id 若绑过别人，先解绑
	if _, err := tx.Exec(`DELETE FROM xt_agent_qq_links WHERE open_id = ?`, openID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO xt_agent_qq_links (user_id, open_id, chat_type, chat_id, created_at) VALUES (?,?,?,?,?)`,
		userID, openID, chatType, chatID, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Link{UserID: userID, OpenID: openID, ChatType: chatType, ChatID: chatID, CreatedAt: now}, nil
}

// GetByUserID 查用户绑定。
func (r *Repo) GetByUserID(userID int64) (*Link, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var l Link
	err = db.QueryRow(`SELECT user_id, open_id, chat_type, chat_id, created_at, conversation_id, model FROM xt_agent_qq_links WHERE user_id = ?`, userID).
		Scan(&l.UserID, &l.OpenID, &l.ChatType, &l.ChatID, &l.CreatedAt, &l.ConversationID, &l.Model)
	if err == sql.ErrNoRows {
		return nil, ErrNotLinked
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// GetByOpenID 查 open_id 绑定（入站消息路由）。
func (r *Repo) GetByOpenID(openID string) (*Link, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var l Link
	err = db.QueryRow(`SELECT user_id, open_id, chat_type, chat_id, created_at, conversation_id, model FROM xt_agent_qq_links WHERE open_id = ?`, openID).
		Scan(&l.UserID, &l.OpenID, &l.ChatType, &l.ChatID, &l.CreatedAt, &l.ConversationID, &l.Model)
	if err == sql.ErrNoRows {
		return nil, ErrNotLinked
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// SetChatContext 更新回发目标会话上下文（同一 open_id 换群/换频道时）。
func (r *Repo) SetChatContext(openID, chatType, chatID string) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_agent_qq_links SET chat_type = ?, chat_id = ? WHERE open_id = ?`, chatType, chatID, openID)
	return err
}

// SetConversationID 设置/清除会话绑定（空串 = 清除，/new 用）。
func (r *Repo) SetConversationID(openID, conversationID string) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_agent_qq_links SET conversation_id = ? WHERE open_id = ?`, conversationID, openID)
	return err
}

// SetModel 设置/清除模型覆盖（空串 = 清除，/model default 用）。
func (r *Repo) SetModel(openID, model string) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_agent_qq_links SET model = ? WHERE open_id = ?`, model, openID)
	return err
}

// Unlink 解绑。
func (r *Repo) Unlink(userID int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM xt_agent_qq_links WHERE user_id = ?`, userID)
	return err
}
