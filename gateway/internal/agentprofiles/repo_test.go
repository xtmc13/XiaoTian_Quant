package agentprofiles

import (
	"testing"

	"github.com/xiaotian-quant/gateway/internal/store"
)

func setupProfilesTestDB(t *testing.T) {
	t.Helper()
	t.Setenv("DB_PATH", t.TempDir()+"/gateway.db")
	t.Setenv("SECRET_KEY", "test-secret-key-agent-profiles")
	if err := store.InitDB(); err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(store.CloseDB)
}

func createUser(t *testing.T, username, role string) int64 {
	t.Helper()
	res, err := store.GetDB().Exec(`INSERT INTO xt_users (username, password_hash, role) VALUES (?,?,?)`, username, "x", role)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestLazyDefaultProfile(t *testing.T) {
	setupProfilesTestDB(t)
	repo := NewRepo()

	// 首次读取惰性创建 默认 档案并置为激活
	profiles, active, err := repo.ListByUser(101)
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 || profiles[0].Name != DefaultProfileName || !profiles[0].IsDefault {
		t.Fatalf("惰性默认档案: %+v", profiles)
	}
	if active != profiles[0].ID {
		t.Fatalf("激活档案 = %d, want %d", active, profiles[0].ID)
	}
	// 幂等：再次读取不重复创建
	profiles, _, _ = repo.ListByUser(101)
	if len(profiles) != 1 {
		t.Fatalf("重复读取不应重复建档: %+v", profiles)
	}
}

func TestActivateAndDeleteRules(t *testing.T) {
	setupProfilesTestDB(t)
	repo := NewRepo()

	id, err := repo.Create(102, "工作")
	if err != nil {
		t.Fatal(err)
	}
	// 激活自己的档案
	if err := repo.Activate(102, id); err != nil {
		t.Fatal(err)
	}
	if got := repo.ActiveProfileID(102); got != id {
		t.Fatalf("激活后 = %d, want %d", got, id)
	}
	// 激活他人/不存在档案 404
	if err := repo.Activate(103, id); err != ErrNotFound {
		t.Errorf("激活他人档案应 ErrNotFound: %v", err)
	}

	// 默认档案不可删
	profiles, _, _ := repo.ListByUser(102)
	defID := profiles[0].ID
	if err := repo.Delete(102, defID, false); err != ErrDefaultProfile {
		t.Errorf("删默认档案应 ErrDefaultProfile: %v", err)
	}
	// 管理员也不能删他人默认档案
	if err := repo.Delete(1, defID, true); err != ErrDefaultProfile {
		t.Errorf("管理员删他人默认档案应 ErrDefaultProfile: %v", err)
	}
	// 他人档案按不存在处理
	if err := repo.Delete(103, id, false); err != ErrNotFound {
		t.Errorf("删他人档案应 ErrNotFound: %v", err)
	}

	// 删除激活档案：激活状态回落默认
	if err := repo.Delete(102, id, false); err != nil {
		t.Fatal(err)
	}
	if got := repo.ActiveProfileID(102); got != defID {
		t.Fatalf("删除激活档案后应回落默认 %d, got %d", defID, got)
	}

	// 管理员可删他人非默认档案
	pid, _ := repo.Create(102, "临时")
	if err := repo.Delete(1, pid, true); err != nil {
		t.Errorf("管理员删他人非默认档案: %v", err)
	}
	if ps := mustProfiles(t, repo, 102); len(ps) != 1 {
		t.Fatalf("删除后档案列表: %+v", ps)
	}
}

func mustProfiles(t *testing.T, repo *Repo, uid int64) []Profile {
	t.Helper()
	ps, _, err := repo.ListByUser(uid)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestAdminListAll(t *testing.T) {
	setupProfilesTestDB(t)
	repo := NewRepo()
	uid1 := createUser(t, "prof_admin", "admin")
	uid2 := createUser(t, "prof_user", "user")

	all, err := repo.AdminListAll()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[int64]AdminUserProfiles{}
	for _, u := range all {
		byID[u.UserID] = u
	}
	// 每个用户都惰性建有默认档案，并带激活标记
	for _, uid := range []int64{uid1, uid2} {
		u, ok := byID[uid]
		if !ok {
			t.Fatalf("admin_all 缺少用户 %d", uid)
		}
		if len(u.Profiles) != 1 || !u.Profiles[0].IsDefault || u.ActiveID != u.Profiles[0].ID {
			t.Errorf("用户 %d 档案: %+v active=%d", uid, u.Profiles, u.ActiveID)
		}
	}
	if byID[uid1].Username != "prof_admin" || byID[uid2].Username != "prof_user" {
		t.Errorf("username 字段: %+v", all)
	}
}
