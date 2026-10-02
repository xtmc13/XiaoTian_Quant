package agentweixin

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// LoginState 微信机器人登录态（xt_agent_weixin_state 单行表）。
type LoginState struct {
	BotToken      string `json:"bot_token"`
	BaseURL       string `json:"baseurl"`
	BotID         string `json:"bot_id"`
	GetUpdatesBuf string `json:"get_updates_buf"`
	LoggedInAt    int64  `json:"logged_in_at"`
}

// Link 一条微信 ↔ 平台用户绑定（wxid 形如 "xxx@im.wechat"）。
type Link struct {
	UserID int64 `json:"user_id"`
	WXID   string `json:"wxid"`
	// ConversationID 会话绑定；空 = 无会话（无状态执行）。
	ConversationID string `json:"conversation_id"`
	// Model 本绑定的模型覆盖（"provider" 或 "provider:model"）；空 = 跟随网关默认。
	Model     string `json:"model"`
	CreatedAt int64  `json:"created_at"`
}

// Repo 登录态 / 绑定 / 配对码存储。
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
	ErrNotLinked   = errors.New("未绑定微信")
	ErrCodeInvalid = errors.New("配对码无效或已过期")
)

// PairCodeTTL 配对码有效期。
const PairCodeTTL = 10 * time.Minute

// ── 机器人登录态（单行表 id=1） ──

// SaveLogin 覆盖写入登录态（QR 确认后调用）。
func (r *Repo) SaveLogin(st *LoginState) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO xt_agent_weixin_state (id, bot_token, baseurl, bot_id, get_updates_buf, logged_in_at)
		VALUES (1,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET bot_token=excluded.bot_token, baseurl=excluded.baseurl,
			bot_id=excluded.bot_id, get_updates_buf=excluded.get_updates_buf, logged_in_at=excluded.logged_in_at`,
		st.BotToken, st.BaseURL, st.BotID, st.GetUpdatesBuf, st.LoggedInAt)
	return err
}

// LoadLogin 读取登录态；无记录返回 (nil, nil)。
func (r *Repo) LoadLogin() (*LoginState, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var st LoginState
	err = db.QueryRow(`SELECT bot_token, baseurl, bot_id, get_updates_buf, logged_in_at FROM xt_agent_weixin_state WHERE id = 1`).
		Scan(&st.BotToken, &st.BaseURL, &st.BotID, &st.GetUpdatesBuf, &st.LoggedInAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

// SaveSyncBuf 持久化长轮询游标（必须在收到新游标后立即保存，否则重复收消息）。
func (r *Repo) SaveSyncBuf(buf string) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_agent_weixin_state SET get_updates_buf = ? WHERE id = 1`, buf)
	return err
}

// ClearLogin 清登录态（解绑/掉线后调用）。
func (r *Repo) ClearLogin() error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM xt_agent_weixin_state WHERE id = 1`)
	return err
}

// ── 配对码与绑定（仿 agentqq repo） ──

// CreatePairCode 生成 6 位数字配对码（同用户旧码作废）。
func (r *Repo) CreatePairCode(userID int64) (string, error) {
	db, err := r.db()
	if err != nil {
		return "", err
	}
	if _, err := db.Exec(`UPDATE xt_agent_weixin_pair_codes SET used_at = ? WHERE user_id = ? AND used_at = 0`, time.Now().Unix(), userID); err != nil {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		return "", err
	}
	code := fmt.Sprintf("%06d", n.Int64()+100000)
	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO xt_agent_weixin_pair_codes (code, user_id, created_at, expires_at, used_at) VALUES (?,?,?,?,0)`,
		code, userID, now, now+int64(PairCodeTTL.Seconds()))
	return code, err
}

// ConsumePairCode 校验并消费配对码：成功则建立绑定（wxid 换绑到新用户）。
func (r *Repo) ConsumePairCode(code, wxid string) (*Link, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var userID int64
	var expiresAt, usedAt int64
	err = db.QueryRow(`SELECT user_id, expires_at, used_at FROM xt_agent_weixin_pair_codes WHERE code = ?`, code).
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
	if _, err := tx.Exec(`UPDATE xt_agent_weixin_pair_codes SET used_at = ? WHERE code = ?`, now, code); err != nil {
		return nil, err
	}
	// wxid 若绑过别人，先解绑
	if _, err := tx.Exec(`DELETE FROM xt_agent_weixin_links WHERE wxid = ?`, wxid); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO xt_agent_weixin_links (user_id, wxid, created_at) VALUES (?,?,?)`,
		userID, wxid, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Link{UserID: userID, WXID: wxid, CreatedAt: now}, nil
}

// GetByWXID 查 wxid 绑定（入站消息路由）。
func (r *Repo) GetByWXID(wxid string) (*Link, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var l Link
	err = db.QueryRow(`SELECT user_id, wxid, conversation_id, model, created_at FROM xt_agent_weixin_links WHERE wxid = ?`, wxid).
		Scan(&l.UserID, &l.WXID, &l.ConversationID, &l.Model, &l.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotLinked
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// GetByUserID 查用户绑定。
func (r *Repo) GetByUserID(userID int64) (*Link, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	var l Link
	err = db.QueryRow(`SELECT user_id, wxid, conversation_id, model, created_at FROM xt_agent_weixin_links WHERE user_id = ?`, userID).
		Scan(&l.UserID, &l.WXID, &l.ConversationID, &l.Model, &l.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, ErrNotLinked
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// SetConversationID 设置/清除会话绑定（空串 = 清除，/new 用）。
func (r *Repo) SetConversationID(wxid, conversationID string) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_agent_weixin_links SET conversation_id = ? WHERE wxid = ?`, conversationID, wxid)
	return err
}

// SetModel 设置/清除模型覆盖（空串 = 清除，/model default 用）。
func (r *Repo) SetModel(wxid, model string) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_agent_weixin_links SET model = ? WHERE wxid = ?`, model, wxid)
	return err
}

// Unlink 解绑（清除该用户的微信绑定；机器人登录态由 Bot.Unlink 处理）。
func (r *Repo) Unlink(userID int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	_, err = db.Exec(`DELETE FROM xt_agent_weixin_links WHERE user_id = ?`, userID)
	return err
}
