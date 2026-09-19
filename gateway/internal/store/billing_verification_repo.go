package store

import (
	"database/sql"
	"errors"
	"time"
)

// ── Billing Verification Snapshot（C4.1 链上核验快照）──────────

// BillingVerification 订单最近一次链上核验的快照（每订单一行，upsert）。
// GET /api/billing/orders/:id/verification 的数据源。
type BillingVerification struct {
	OrderID               string `json:"order_id"`
	Chain                 string `json:"chain"`
	TxHash                string `json:"tx_hash"`
	Found                 bool   `json:"found"`
	Valid                 bool   `json:"valid"`
	Confirmed             bool   `json:"confirmed"`
	Confirmations         int64  `json:"confirmations"`
	RequiredConfirmations int64  `json:"required_confirmations"`
	BlockNumber           int64  `json:"block_number"`
	ReceivedMicro         int64  `json:"received_micro"`
	ExpectedMicro         int64  `json:"expected_micro"`
	FailReason            string `json:"fail_reason"`
	CheckedAt             int64  `json:"checked_at"`
}

// UpsertVerification 写入/覆盖订单核验快照。
func (r *BillingRepo) UpsertVerification(v *BillingVerification) error {
	if v.CheckedAt == 0 {
		v.CheckedAt = time.Now().Unix()
	}
	found, valid, confirmed := 0, 0, 0
	if v.Found {
		found = 1
	}
	if v.Valid {
		valid = 1
	}
	if v.Confirmed {
		confirmed = 1
	}
	_, err := db.Exec(`INSERT INTO billing_verification
		(order_id, chain, tx_hash, found, valid, confirmed, confirmations, required_confirmations, block_number, received_micro, expected_micro, fail_reason, checked_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(order_id) DO UPDATE SET
			chain=excluded.chain, tx_hash=excluded.tx_hash, found=excluded.found, valid=excluded.valid,
			confirmed=excluded.confirmed, confirmations=excluded.confirmations,
			required_confirmations=excluded.required_confirmations, block_number=excluded.block_number,
			received_micro=excluded.received_micro, expected_micro=excluded.expected_micro,
			fail_reason=excluded.fail_reason, checked_at=excluded.checked_at`,
		v.OrderID, v.Chain, v.TxHash, found, valid, confirmed, v.Confirmations,
		v.RequiredConfirmations, v.BlockNumber, v.ReceivedMicro, v.ExpectedMicro, v.FailReason, v.CheckedAt)
	return err
}

// GetVerification 读订单最近一次核验快照（不存在返回 nil, nil）。
func (r *BillingRepo) GetVerification(orderID string) (*BillingVerification, error) {
	row := db.QueryRow(`SELECT order_id, chain, tx_hash, found, valid, confirmed, confirmations, required_confirmations, block_number, received_micro, expected_micro, fail_reason, checked_at
		FROM billing_verification WHERE order_id=?`, orderID)
	var v BillingVerification
	var found, valid, confirmed int
	if err := row.Scan(&v.OrderID, &v.Chain, &v.TxHash, &found, &valid, &confirmed,
		&v.Confirmations, &v.RequiredConfirmations, &v.BlockNumber, &v.ReceivedMicro,
		&v.ExpectedMicro, &v.FailReason, &v.CheckedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // 快照不存在（尚未核验过）
		}
		return nil, err
	}
	v.Found = found != 0
	v.Valid = valid != 0
	v.Confirmed = confirmed != 0
	return &v, nil
}
