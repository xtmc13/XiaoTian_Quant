// Package agentkanban 看板任务卡插件（对标 hermes-agent kanban）：
// 助手与用户共用一块任务看板，跟踪多步任务/计划的进度。
package agentkanban

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/xiaotian-quant/gateway/internal/agentprofiles"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ErrNotFound 卡片不存在或不属于当前用户。
var ErrNotFound = errors.New("kanban card not found")

// Card 一张看板任务卡。
type Card struct {
	ID          string `json:"id"`
	UserID      int64  `json:"-"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Column      string `json:"column"`     // todo | doing | done
	Assignee    string `json:"assignee"`   // user | agent（空 = 未指派）
	CreatedBy   string `json:"created_by"` // user | agent
	Comment     string `json:"comment"`
	ProfileID   int64  `json:"profile_id"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

// Repo 看板卡存储（xt_agent_kanban_cards）。
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

// NewID 生成看板卡 id。
func NewID() string {
	return fmt.Sprintf("kb_%d", time.Now().UnixNano())
}

var validColumns = map[string]bool{"todo": true, "doing": true, "done": true}

// ValidColumn 判断列名是否合法。
func ValidColumn(col string) bool { return validColumns[col] }

// Create 写入卡片；column 非法回落 todo，created_by 默认 user。
func (r *Repo) Create(c *Card) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	if !validColumns[c.Column] {
		c.Column = "todo"
	}
	if c.CreatedBy == "" {
		c.CreatedBy = "user"
	}
	now := time.Now().Unix()
	c.CreatedAt, c.UpdatedAt = now, now
	_, err = db.Exec(`INSERT INTO xt_agent_kanban_cards
		(id, user_id, title, description, "column", assignee, created_by, comment, profile_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.UserID, c.Title, c.Description, c.Column, c.Assignee, c.CreatedBy, c.Comment, c.ProfileID, c.CreatedAt, c.UpdatedAt)
	return err
}

const cardCols = `id, user_id, title, description, "column", assignee, created_by, comment, profile_id, created_at, updated_at`

func scanCard(row interface{ Scan(...any) error }) (*Card, error) {
	var c Card
	err := row.Scan(&c.ID, &c.UserID, &c.Title, &c.Description, &c.Column, &c.Assignee, &c.CreatedBy, &c.Comment, &c.ProfileID, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListByUser 列出用户卡片：按列 todo→doing→done 排序，列内 updated_at 降序；
// column 非空时只取该列。档案隔离：仅返回全局行（profile_id=0）与当前激活档案的行。
func (r *Repo) ListByUser(userID int64, column string) ([]*Card, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	q := `SELECT ` + cardCols + ` FROM xt_agent_kanban_cards WHERE user_id = ? AND profile_id IN (0, ?)`
	args := []any{userID, agentprofiles.NewRepo().ActiveProfileID(userID)}
	if column != "" {
		q += ` AND "column" = ?`
		args = append(args, column)
	}
	q += ` ORDER BY CASE "column" WHEN 'todo' THEN 0 WHEN 'doing' THEN 1 ELSE 2 END ASC, updated_at DESC`
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Card{}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetByID 取卡（含用户校验）；不存在或他人卡返回 ErrNotFound。
func (r *Repo) GetByID(id string, userID int64) (*Card, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	c, err := scanCard(db.QueryRow(`SELECT `+cardCols+` FROM xt_agent_kanban_cards WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// Update 更新卡片（含用户校验）；不存在或他人卡返回 ErrNotFound。
func (r *Repo) Update(c *Card) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	if !validColumns[c.Column] {
		return fmt.Errorf("invalid column: %s", c.Column)
	}
	c.UpdatedAt = time.Now().Unix()
	res, err := db.Exec(`UPDATE xt_agent_kanban_cards
		SET title = ?, description = ?, "column" = ?, assignee = ?, comment = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`,
		c.Title, c.Description, c.Column, c.Assignee, c.Comment, c.UpdatedAt, c.ID, c.UserID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete 删除卡片（含用户校验）；不存在或他人卡返回 ErrNotFound。
func (r *Repo) Delete(id string, userID int64) error {
	db, err := r.db()
	if err != nil {
		return err
	}
	res, err := db.Exec(`DELETE FROM xt_agent_kanban_cards WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
