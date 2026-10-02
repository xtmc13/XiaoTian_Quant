// Package agentprofiles Agent 档案（profiles）：同一用户可有多套长期知识空间，
// 记忆/技能/看板按当前激活档案隔离（profile_id=0 为全局行，任何档案下可见）。
// 会话与定时任务不做档案隔离——会话全局可见，档案隔离的是长期知识。
package agentprofiles

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// Profile 一个档案。
type Profile struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default"`
	CreatedAt int64  `json:"created_at"`
}

// AdminUserProfiles 管理员视角：一个用户及其全部档案与激活档案 id。
type AdminUserProfiles struct {
	UserID   int64     `json:"user_id"`
	Username string    `json:"username"`
	Profiles []Profile `json:"profiles"`
	ActiveID int64     `json:"-"`
}

// ErrDefaultProfile 默认档案不可删除。
var ErrDefaultProfile = errors.New("默认档案不可删除")

// ErrNotFound 档案不存在或不属于当前用户。
var ErrNotFound = errors.New("profile not found")

// Repo 档案存储（xt_agent_profiles + xt_agent_profile_state）。
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

// DefaultProfileName 首个档案的固定名称。
const DefaultProfileName = "默认"

// ensureDefault 惰性初始化：用户首次读取时创建默认档案（is_default=1）并置为激活。
func (r *Repo) ensureDefault(db *sql.DB, userID int64) error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM xt_agent_profiles WHERE user_id = ? AND is_default = 1`, userID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	res, err := db.Exec(`INSERT INTO xt_agent_profiles (user_id, name, is_default, created_at) VALUES (?,?,1,?)`,
		userID, DefaultProfileName, time.Now().Unix())
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO xt_agent_profile_state (user_id, active_profile_id) VALUES (?,?)
		ON CONFLICT(user_id) DO NOTHING`, userID, id)
	return err
}

// defaultProfileID 取用户默认档案 id（调用前需 ensureDefault）。
func defaultProfileID(db *sql.DB, userID int64) (int64, error) {
	var id int64
	err := db.QueryRow(`SELECT id FROM xt_agent_profiles WHERE user_id = ? AND is_default = 1 ORDER BY id LIMIT 1`, userID).Scan(&id)
	return id, err
}

// ActiveProfileID 返回用户当前激活档案 id（惰性确保默认档案；
// 任何异常或状态指向已删档案时回落默认档案）。档案隔离读路径每次请求调用一次。
func (r *Repo) ActiveProfileID(userID int64) int64 {
	db, err := r.db()
	if err != nil || userID <= 0 {
		return 0
	}
	if err := r.ensureDefault(db, userID); err != nil {
		return 0
	}
	defID, err := defaultProfileID(db, userID)
	if err != nil {
		return 0
	}
	var active int64
	if err := db.QueryRow(`SELECT active_profile_id FROM xt_agent_profile_state WHERE user_id = ?`, userID).Scan(&active); err != nil {
		return defID
	}
	if active <= 0 {
		return defID
	}
	// 激活状态可能指向已删除的档案：校验存在性，失效则回落默认。
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM xt_agent_profiles WHERE id = ? AND user_id = ?`, active, userID).Scan(&n); err != nil || n == 0 {
		_ = r.setActive(db, userID, defID)
		return defID
	}
	return active
}

func (r *Repo) setActive(db *sql.DB, userID, profileID int64) error {
	_, err := db.Exec(`INSERT INTO xt_agent_profile_state (user_id, active_profile_id) VALUES (?,?)
		ON CONFLICT(user_id) DO UPDATE SET active_profile_id = excluded.active_profile_id`, userID, profileID)
	return err
}

// ListByUser 列出用户全部档案与激活档案 id（惰性确保默认档案）。
func (r *Repo) ListByUser(userID int64) ([]Profile, int64, error) {
	db, err := r.db()
	if err != nil {
		return nil, 0, err
	}
	if err := r.ensureDefault(db, userID); err != nil {
		return nil, 0, err
	}
	profiles, err := r.listByUserNoEnsure(db, userID)
	if err != nil {
		return nil, 0, err
	}
	return profiles, r.ActiveProfileID(userID), nil
}

func (r *Repo) listByUserNoEnsure(db *sql.DB, userID int64) ([]Profile, error) {
	rows, err := db.Query(`SELECT id, user_id, name, is_default, created_at FROM xt_agent_profiles WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Profile{}
	for rows.Next() {
		var p Profile
		var def int
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &def, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.IsDefault = def == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// Create 新建档案（不切换激活）；名称为空报错。
func (r *Repo) Create(userID int64, name string) (int64, error) {
	db, err := r.db()
	if err != nil {
		return 0, err
	}
	if name == "" {
		return 0, errors.New("档案名称不能为空")
	}
	if err := r.ensureDefault(db, userID); err != nil {
		return 0, err
	}
	res, err := db.Exec(`INSERT INTO xt_agent_profiles (user_id, name, is_default, created_at) VALUES (?,?,0,?)`,
		userID, name, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Activate 激活自己的某个档案（仅档案属主可操作；管理员切换也走自己的档案）。
func (r *Repo) Activate(userID, profileID int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM xt_agent_profiles WHERE id = ? AND user_id = ?`, profileID, userID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return r.setActive(db, userID, profileID)
}

// Delete 删除档案：默认档案不可删（ErrDefaultProfile）；删除激活档案后激活状态回落默认。
// asAdmin=true 时可删任意用户的档案（同样不可删对方的默认档案）。
func (r *Repo) Delete(requesterUserID, profileID int64, asAdmin bool) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	var ownerID int64
	var def int
	err = db.QueryRow(`SELECT user_id, is_default FROM xt_agent_profiles WHERE id = ?`, profileID).Scan(&ownerID, &def)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !asAdmin && ownerID != requesterUserID {
		return ErrNotFound // 他人档案按不存在处理，不泄露存在性
	}
	if def == 1 {
		return ErrDefaultProfile
	}
	if _, err := db.Exec(`DELETE FROM xt_agent_profiles WHERE id = ?`, profileID); err != nil {
		return err
	}
	// 删除的是属主当前激活档案：激活状态回落默认档案。
	var active int64
	if err := db.QueryRow(`SELECT active_profile_id FROM xt_agent_profile_state WHERE user_id = ?`, ownerID).Scan(&active); err == nil && active == profileID {
		defID, derr := defaultProfileID(db, ownerID)
		if derr != nil {
			return derr
		}
		return r.setActive(db, ownerID, defID)
	}
	return nil
}

// AdminListAll 管理员视角：全部用户及其档案（每个用户惰性确保默认档案）。
func (r *Repo) AdminListAll() ([]AdminUserProfiles, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT id, username FROM xt_users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	type urow struct {
		id   int64
		name string
	}
	users := []urow{}
	for rows.Next() {
		var u urow
		if err := rows.Scan(&u.id, &u.name); err != nil {
			rows.Close()
			return nil, err
		}
		users = append(users, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]AdminUserProfiles, 0, len(users))
	for _, u := range users {
		if err := r.ensureDefault(db, u.id); err != nil {
			return nil, fmt.Errorf("ensure default for user %d: %w", u.id, err)
		}
		profiles, err := r.listByUserNoEnsure(db, u.id)
		if err != nil {
			return nil, err
		}
		out = append(out, AdminUserProfiles{UserID: u.id, Username: u.name, Profiles: profiles, ActiveID: r.ActiveProfileID(u.id)})
	}
	return out, nil
}
