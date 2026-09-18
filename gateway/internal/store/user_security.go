package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ── 用户安全状态（A3.1 MFA / A3.3 token_version 吊销 / A3.4 强制改密） ──
// 迁移见 migrations/sql/0003_user_mfa_security.sql。

// UserSecurityFlags 是鉴权中间件每请求需要的用户安全字段。
type UserSecurityFlags struct {
	TokenVersion       int
	MustChangePassword bool
	TOTPEnabled        bool
	IsActive           bool
}

// GetUserSecurityFlags 查询用户的鉴权安全字段；用户不存在返回 found=false。
func GetUserSecurityFlags(userID int) (flags UserSecurityFlags, found bool) {
	if db == nil {
		return flags, false
	}
	row := db.QueryRow(`SELECT token_version, must_change_password, totp_enabled, is_active FROM xt_users WHERE id=?`, userID)
	var tv, mcp, totp, active int
	if err := row.Scan(&tv, &mcp, &totp, &active); err != nil {
		return flags, false
	}
	return UserSecurityFlags{
		TokenVersion:       tv,
		MustChangePassword: mcp == 1,
		TOTPEnabled:        totp == 1,
		IsActive:           active == 1,
	}, true
}

// FindUserByID 按 ID 查询用户基础信息（MFA 第二步签发令牌时使用）。
func FindUserByID(userID int) map[string]any {
	if db == nil {
		return nil
	}
	row := db.QueryRow("SELECT id, username, nickname, role FROM xt_users WHERE id=?", userID)
	var id int
	var username, nickname, role string
	if err := row.Scan(&id, &username, &nickname, &role); err != nil {
		return nil
	}
	return map[string]any{
		"id": id, "username": username, "nickname": nickname, "role": role,
	}
}

// GetUserTokenVersion 返回用户当前 token_version（JWT tv 校验用）。
func GetUserTokenVersion(userID int) (int, error) {
	if db == nil {
		return 0, fmt.Errorf("database not initialized")
	}
	var tv int
	err := db.QueryRow(`SELECT token_version FROM xt_users WHERE id=?`, userID).Scan(&tv)
	return tv, err
}

// RevokeUserTokens 使用户既有 JWT 全部失效（token_version+1）。
func RevokeUserTokens(userID int) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE xt_users SET token_version=token_version+1 WHERE id=?`, userID)
	return err
}

// SetMustChangePassword 设置/清除强制改密标记。
func SetMustChangePassword(userID int, must bool) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	v := 0
	if must {
		v = 1
	}
	_, err := db.Exec(`UPDATE xt_users SET must_change_password=? WHERE id=?`, v, userID)
	return err
}

// ── MFA / TOTP ──

// GetUserTOTP 返回用户的 TOTP 状态；用户不存在返回 found=false。
func GetUserTOTP(userID int) (secret string, enabled bool, found bool) {
	if db == nil {
		return "", false, false
	}
	row := db.QueryRow(`SELECT totp_secret, totp_enabled FROM xt_users WHERE id=?`, userID)
	var enc int
	if err := row.Scan(&secret, &enc); err != nil {
		return "", false, false
	}
	return secret, enc == 1, true
}

// SetUserTOTPSecret 暂存 setup 阶段生成的 TOTP 密钥（尚未启用）。
func SetUserTOTPSecret(userID int, secret string) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE xt_users SET totp_secret=?, totp_enabled=0 WHERE id=?`, secret, userID)
	return err
}

// EnableUserTOTP 启用 MFA 并加密保存一次性备用码 JSON 数组。
func EnableUserTOTP(userID int, backupCodes []string) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	raw, err := json.Marshal(backupCodes)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE xt_users SET totp_enabled=1, mfa_backup_codes=? WHERE id=?`,
		encryptBackupCodes(string(raw)), userID)
	return err
}

// DisableUserTOTP 关闭 MFA 并清除密钥与备用码。
func DisableUserTOTP(userID int) error {
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	_, err := db.Exec(`UPDATE xt_users SET totp_enabled=0, totp_secret='', mfa_backup_codes='' WHERE id=?`, userID)
	return err
}

// ConsumeBackupCode 校验一次性备用码：命中即从列表移除（一次性）并返回 true。
// 加密存储的备用码列表解密失败视为无可用备用码。
func ConsumeBackupCode(userID int, code string) bool {
	if db == nil {
		return false
	}
	var stored string
	if err := db.QueryRow(`SELECT mfa_backup_codes FROM xt_users WHERE id=?`, userID).Scan(&stored); err != nil {
		return false
	}
	var codes []string
	if err := json.Unmarshal([]byte(decryptBackupCodes(stored)), &codes); err != nil || len(codes) == 0 {
		return false
	}
	kept := make([]string, 0, len(codes))
	used := false
	for _, c := range codes {
		if !used && c == code {
			used = true
			continue
		}
		kept = append(kept, c)
	}
	if !used {
		return false
	}
	raw, _ := json.Marshal(kept)
	db.Exec(`UPDATE xt_users SET mfa_backup_codes=? WHERE id=?`, encryptBackupCodes(string(raw)), userID)
	return true
}

// encryptBackupCodes 用 XIAOTIAN_CONFIG_KEY 派生密钥加密备用码 JSON；
// 未配置密钥时保持明文（与 config.yaml 密钥字段的既有降级行为一致）。
func encryptBackupCodes(plain string) string {
	key := configEncryptionKey()
	if key == nil {
		return plain
	}
	return encryptString(plain, key)
}

func decryptBackupCodes(stored string) string {
	key := configEncryptionKey()
	if key == nil {
		return stored
	}
	return decryptString(stored, key)
}

// ── 风险登录 IP 记录 ──

// UpdateLastLoginIP 记录本次登录 IP 并返回上一次的 IP（首次登录返回空串）。
func UpdateLastLoginIP(userID int, ip string) (prev string) {
	if db == nil {
		return ""
	}
	row := db.QueryRow(`SELECT last_login_ip FROM xt_users WHERE id=?`, userID)
	_ = row.Scan(&prev)
	db.Exec(`UPDATE xt_users SET last_login_ip=? WHERE id=?`, ip, userID)
	return prev
}

// ── MFA 登录临时令牌 ──

// mfaTokenTTL 是密码校验通过后临时 MFA 令牌的有效期（第二步换正式 JWT）。
const mfaTokenTTL = 5 * time.Minute

// GenerateMFAToken 签发短时效临时令牌（仅用于 /auth/mfa/verify 换正式 JWT）。
func GenerateMFAToken(userID int) (string, error) {
	claims := jwt.MapClaims{
		"sub":     fmt.Sprintf("mfa:%d", userID),
		"user_id": userID,
		"purpose": "mfa_login",
		"iat":     time.Now().Unix(),
		"exp":     time.Now().Add(mfaTokenTTL).Unix(),
		"jti":     randomHex(8),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret))
}

// VerifyMFAToken 校验临时令牌并返回 userID；过期/伪造/用途不符返回错误。
func VerifyMFAToken(tokenStr string) (int, error) {
	claims, err := VerifyJWT(tokenStr)
	if err != nil {
		return 0, err
	}
	if claims["purpose"] != "mfa_login" {
		return 0, errors.New("invalid token purpose")
	}
	uid, ok := claims["user_id"].(float64)
	if !ok || uid <= 0 {
		return 0, errors.New("invalid token subject")
	}
	return int(uid), nil
}
