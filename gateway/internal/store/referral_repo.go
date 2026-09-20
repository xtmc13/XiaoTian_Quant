package store

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ── Referral Repository（affiliate 推荐码体系，0020 迁移）──

// ReferralCode 推荐码：一人一个，code 全局唯一（'XT'+8 位大写字母数字）。
type ReferralCode struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"user_id"`
	Code          string `json:"code"`
	CommissionPct int    `json:"commission_pct"`
	Active        bool   `json:"active"`
	CreatedAt     int64  `json:"created_at"`
}

// ReferralEarning 单笔佣金入账记录（order_id 唯一约束 = 幂等键）。
type ReferralEarning struct {
	ID             int64  `json:"id"`
	ReferrerUserID int64  `json:"referrer_user_id"`
	ReferredUserID int64  `json:"referred_user_id"`
	OrderID        string `json:"order_id"`
	OrderAmount    int64  `json:"order_amount"` // 微单位（1 USDT = 1_000_000）
	Credits        int64  `json:"credits"`
	Status         string `json:"status"`
	CreatedAt      int64  `json:"created_at"`
	// ReferredUsername 列表查询联表带出（非存储列）。
	ReferredUsername string `json:"referred_username,omitempty"`
}

// ReferralStats 我的推荐汇总。
type ReferralStats struct {
	ReferralCount int   `json:"referral_count"`
	TotalCredits  int64 `json:"total_credits"`
}

// ReferralAdminRow admin 视角的推荐人汇总行。
type ReferralAdminRow struct {
	UserID        int64  `json:"user_id"`
	Username      string `json:"username"`
	Code          string `json:"code"`
	CommissionPct int    `json:"commission_pct"`
	Active        bool   `json:"active"`
	ReferralCount int    `json:"referral_count"`
	TotalCredits  int64  `json:"total_credits"`
	CreatedAt     int64  `json:"created_at"`
}

// ErrReferralSelf 自己推荐自己的守卫错误。
var ErrReferralSelf = errors.New("cannot refer yourself")

type ReferralRepo struct{ mu sync.Mutex }

func NewReferralRepo() *ReferralRepo { return &ReferralRepo{} }

const referralCodeCols = `id, user_id, code, commission_pct, active, created_at`

func scanReferralCode(row interface{ Scan(...any) error }) (*ReferralCode, error) {
	var rc ReferralCode
	var active int
	err := row.Scan(&rc.ID, &rc.UserID, &rc.Code, &rc.CommissionPct, &active, &rc.CreatedAt)
	if err != nil {
		return nil, err
	}
	rc.Active = active == 1
	return &rc, nil
}

// GetCodeByUser 按用户查推荐码；未创建返回 (nil, sql.ErrNoRows)。
func (r *ReferralRepo) GetCodeByUser(userID int64) (*ReferralCode, error) {
	return scanReferralCode(db.QueryRow(`SELECT `+referralCodeCols+` FROM xt_referral_codes WHERE user_id=?`, userID))
}

// GetCodeByCode 按码字符串查（注册校验用）；不存在返回 (nil, sql.ErrNoRows)。
func (r *ReferralRepo) GetCodeByCode(code string) (*ReferralCode, error) {
	return scanReferralCode(db.QueryRow(`SELECT `+referralCodeCols+` FROM xt_referral_codes WHERE code=?`, code))
}

// referralCodeCharset 推荐码随机字符集（大写字母+数字，去掉易混淆的 0/O/I/1）。
const referralCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// generateReferralCode 生成 'XT'+8 位随机大写字母数字（crypto/rand）。
func generateReferralCode() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand 失败时退化为时间戳后缀（理论上不可达，保底不 panic）。
		return fmt.Sprintf("XT%08X", time.Now().UnixNano()&0xFFFFFFFF)
	}
	for i, b := range buf {
		buf[i] = referralCodeCharset[int(b)%len(referralCodeCharset)]
	}
	return "XT" + string(buf)
}

// EnsureCode 取或建推荐码：已有直接返回；没有则按 pct 生成新码。
// code/user_id 唯一冲突（并发双击）时重查并返回已有行。
func (r *ReferralRepo) EnsureCode(userID int64, pct int) (*ReferralCode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rc, err := r.GetCodeByUser(userID); err == nil {
		return rc, nil
	}
	now := time.Now().Unix()
	for i := 0; i < 10; i++ {
		code := generateReferralCode()
		_, err := db.Exec(
			`INSERT INTO xt_referral_codes (user_id, code, commission_pct, active, created_at) VALUES (?,?,?,1,?)`,
			userID, code, pct, now)
		if err == nil {
			return r.GetCodeByUser(userID)
		}
		// 并发下别人已给该用户建过码 → 返回已有；否则换码重试。
		if rc, gerr := r.GetCodeByUser(userID); gerr == nil {
			return rc, nil
		}
	}
	return nil, fmt.Errorf("generate referral code failed after retries")
}

// SetCommission 更新推荐人的佣金比例；行不存在返回 sql.ErrNoRows（调用方先 EnsureCode）。
func (r *ReferralRepo) SetCommission(userID int64, pct int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	res, err := db.Exec(`UPDATE xt_referral_codes SET commission_pct=? WHERE user_id=?`, pct, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CreditEarningTx 单事务完成「佣金记录插入（幂等）+ 推荐人积分增加」：
// order_id 已存在（重复触发）时返回 (false, nil) 且不重复加积分；
// 任一步失败整体回滚，不会出现记录与积分不一致的中间态。
func (r *ReferralRepo) CreditEarningTx(e *ReferralEarning) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tx, err := db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	if e.CreatedAt == 0 {
		e.CreatedAt = time.Now().Unix()
	}
	if e.Status == "" {
		e.Status = "credited"
	}
	res, err := tx.Exec(
		`INSERT OR IGNORE INTO xt_referral_earnings (referrer_user_id, referred_user_id, order_id, order_amount, credits, status, created_at)
		 VALUES (?,?,?,?,?,?,?)`,
		e.ReferrerUserID, e.ReferredUserID, e.OrderID, e.OrderAmount, e.Credits, e.Status, e.CreatedAt)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil // 该订单已入账（幂等）
	}
	if _, err := tx.Exec(`UPDATE xt_users SET credits = credits + ? WHERE id=?`, e.Credits, e.ReferrerUserID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// ReferralStats 汇总：直接推荐人数（referred_by=me）+ 已入账积分合计。
func (r *ReferralRepo) ReferralStats(userID int64) (*ReferralStats, error) {
	var s ReferralStats
	if err := db.QueryRow(`SELECT COUNT(*) FROM xt_users WHERE referred_by=?`, userID).Scan(&s.ReferralCount); err != nil {
		return nil, err
	}
	if err := db.QueryRow(
		`SELECT COALESCE(SUM(credits),0) FROM xt_referral_earnings WHERE referrer_user_id=? AND status='credited'`, userID,
	).Scan(&s.TotalCredits); err != nil {
		return nil, err
	}
	return &s, nil
}

// ListEarnings 我的佣金明细（新→旧），带被推荐人用户名。
func (r *ReferralRepo) ListEarnings(userID int64, limit int) ([]*ReferralEarning, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.Query(
		`SELECT e.id, e.referrer_user_id, e.referred_user_id, e.order_id, e.order_amount, e.credits, e.status, e.created_at,
			COALESCE(u.username,'')
		 FROM xt_referral_earnings e LEFT JOIN xt_users u ON u.id = e.referred_user_id
		 WHERE e.referrer_user_id=? ORDER BY e.created_at DESC, e.id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReferralEarning
	for rows.Next() {
		var e ReferralEarning
		if err := rows.Scan(&e.ID, &e.ReferrerUserID, &e.ReferredUserID, &e.OrderID, &e.OrderAmount,
			&e.Credits, &e.Status, &e.CreatedAt, &e.ReferredUsername); err != nil {
			return nil, err
		}
		out = append(out, &e)
	}
	return out, nil
}

// AdminList 全员推荐关系 + 收益汇总（每个有码的用户一行）。
func (r *ReferralRepo) AdminList() ([]*ReferralAdminRow, error) {
	rows, err := db.Query(
		`SELECT * FROM (
			SELECT c.user_id AS user_id, COALESCE(u.username,'') AS username, c.code AS code,
				c.commission_pct AS commission_pct, c.active AS active, c.created_at AS created_at,
				(SELECT COUNT(*) FROM xt_users x WHERE x.referred_by = c.user_id) AS referral_count,
				COALESCE((SELECT SUM(e.credits) FROM xt_referral_earnings e
					WHERE e.referrer_user_id = c.user_id AND e.status='credited'),0) AS total_credits
			FROM xt_referral_codes c LEFT JOIN xt_users u ON u.id = c.user_id
		) ORDER BY referral_count DESC, total_credits DESC, created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ReferralAdminRow
	for rows.Next() {
		var row ReferralAdminRow
		var active int
		if err := rows.Scan(&row.UserID, &row.Username, &row.Code, &row.CommissionPct, &active,
			&row.CreatedAt, &row.ReferralCount, &row.TotalCredits); err != nil {
			return nil, err
		}
		row.Active = active == 1
		out = append(out, &row)
	}
	return out, nil
}

// ── 推荐关系（xt_users.referred_by）──

// SetReferredBy 记录推荐关系（注册成功后调用）。仅当尚未有推荐人时写入
// （首次推荐为准，防止后续被覆盖）；自己推荐自己返回 ErrReferralSelf。
func SetReferredBy(userID int, referrerID int64) error {
	if int64(userID) == referrerID {
		return ErrReferralSelf
	}
	res, err := db.Exec(`UPDATE xt_users SET referred_by=? WHERE id=? AND referred_by IS NULL`, referrerID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// GetReferredBy 查询用户的推荐人；无推荐人返回 (0, false)。
func GetReferredBy(userID int64) (int64, bool) {
	var v sql.NullInt64
	if err := db.QueryRow(`SELECT referred_by FROM xt_users WHERE id=?`, userID).Scan(&v); err != nil || !v.Valid {
		return 0, false
	}
	return v.Int64, true
}
