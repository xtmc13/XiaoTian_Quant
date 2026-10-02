package agentfiles

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// Checkpoint 一次写入前备份（xt_agent_file_checkpoints）。
type Checkpoint struct {
	ID             string `json:"id"`
	UserID         int64  `json:"-"`
	Path           string `json:"path"` // 沙箱根内相对路径
	BackupPath     string `json:"-"`
	Size           int64  `json:"size"`
	ConversationID string `json:"conversation_id"`
	CreatedAt      int64  `json:"created_at"`
}

// keepCheckpoints 每用户保留的最近检查点数（超出的行与备份文件一并清理）。
const keepCheckpoints = 100

// Repo 检查点存储（xt_agent_file_checkpoints）。
type Repo struct{}

// NewRepo 基于网关默认 SQLite。
func NewRepo() *Repo { return &Repo{} }

// NewCheckpointID 生成检查点 id。
func NewCheckpointID() string {
	return fmt.Sprintf("cp_%d", time.Now().UnixNano())
}

func (r *Repo) db() (*sql.DB, error) {
	db := store.GetDB()
	if db == nil {
		return nil, errors.New("database not initialized")
	}
	return db, nil
}

// createCheckpoint 写入前备份：目标文件存在时复制到 <root>/.checkpoints/<cp_id>.bak 并落库，
// 随后按用户清理超出保留数的旧检查点。目标不存在（新建文件）返回 nil 不建档。
func (p *Plugin) createCheckpoint(userID int64, rel, abs, conversationID string) (*Checkpoint, error) {
	info, err := os.Stat(abs)
	if err != nil || info.IsDir() {
		return nil, nil // 新建文件无需备份
	}
	r := p.Repo
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	cp := &Checkpoint{
		ID:             NewCheckpointID(),
		UserID:         userID,
		Path:           rel,
		Size:           info.Size(),
		ConversationID: conversationID,
		CreatedAt:      time.Now().Unix(),
	}
	cp.BackupPath = filepath.Join(p.Root, checkpointsDir, cp.ID+".bak")
	if err := os.MkdirAll(filepath.Dir(cp.BackupPath), 0755); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(cp.BackupPath, data, 0600); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`INSERT INTO xt_agent_file_checkpoints
		(id, user_id, path, backup_path, size, conversation_id, created_at) VALUES (?,?,?,?,?,?,?)`,
		cp.ID, cp.UserID, cp.Path, cp.BackupPath, cp.Size, cp.ConversationID, cp.CreatedAt); err != nil {
		return nil, err
	}
	r.prune(db, userID)
	return cp, nil
}

// prune 每用户仅保留最近 keepCheckpoints 条，旧行删除并移除备份文件（失败静默）。
func (r *Repo) prune(db *sql.DB, userID int64) {
	rows, err := db.Query(`SELECT id, backup_path FROM xt_agent_file_checkpoints WHERE user_id = ? ORDER BY created_at DESC, id DESC`, userID)
	if err != nil {
		return
	}
	defer rows.Close()
	var stale []struct{ id, backup string }
	i := 0
	for rows.Next() {
		var id, backup string
		if err := rows.Scan(&id, &backup); err != nil {
			return
		}
		i++
		if i > keepCheckpoints {
			stale = append(stale, struct{ id, backup string }{id, backup})
		}
	}
	for _, s := range stale {
		_, _ = db.Exec(`DELETE FROM xt_agent_file_checkpoints WHERE id = ?`, s.id)
		_ = os.Remove(s.backup)
	}
}

const cpCols = "id, user_id, path, backup_path, size, conversation_id, created_at"

func scanCheckpoint(row interface{ Scan(...any) error }) (*Checkpoint, error) {
	var cp Checkpoint
	err := row.Scan(&cp.ID, &cp.UserID, &cp.Path, &cp.BackupPath, &cp.Size, &cp.ConversationID, &cp.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &cp, nil
}

// ListByUser 列出用户检查点（新的在前）。
func (r *Repo) ListByUser(userID int64, limit int) ([]*Checkpoint, error) {
	db, err := r.db()
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = keepCheckpoints
	}
	rows, err := db.Query(`SELECT `+cpCols+` FROM xt_agent_file_checkpoints WHERE user_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Checkpoint{}
	for rows.Next() {
		cp, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, cp)
	}
	return out, rows.Err()
}

// ErrCheckpointNotFound 检查点不存在或不属于当前用户。
var ErrCheckpointNotFound = errors.New("checkpoint not found")

// Rollback 回滚：把备份内容写回原路径（保留检查点行），返回恢复的相对路径。
func (p *Plugin) Rollback(userID int64, checkpointID string) (string, error) {
	r := p.Repo
	db, err := r.db()
	if err != nil {
		return "", err
	}
	cp, err := scanCheckpoint(db.QueryRow(`SELECT `+cpCols+` FROM xt_agent_file_checkpoints WHERE id = ? AND user_id = ?`, checkpointID, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrCheckpointNotFound
	}
	if err != nil {
		return "", err
	}
	abs, rel, err := p.resolve(cp.Path)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(cp.BackupPath)
	if err != nil {
		return "", fmt.Errorf("备份文件缺失: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, data, 0644); err != nil {
		return "", err
	}
	return rel, nil
}
