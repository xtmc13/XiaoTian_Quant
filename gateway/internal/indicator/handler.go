package indicator

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/middleware"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ParseIndicator godoc
// POST /api/indicator/parse
// Parses indicator source code and extracts metadata, params, and strategy config.
func ParseIndicator(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "code is required", "data": nil})
		return
	}

	result := ParseSource(req.Code)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"params":  result,
	})
}

// ValidateIndicator godoc
// POST /api/indicator/validate
// Performs static analysis and returns validation hints.
func ValidateIndicator(c *gin.Context) {
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "code is required"})
		return
	}

	result := ValidateCode(req.Code)
	resp := gin.H{
		"success": result.Success,
	}
	if !result.Success {
		resp["error"] = result.Msg
	}
	if result.Details != "" {
		resp["details"] = result.Details
	}
	c.JSON(http.StatusOK, resp)
}

// SaveIndicator godoc
// POST /api/indicator/save
// Creates or updates an indicator for the current user.
func SaveIndicator(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized", "data": nil})
		return
	}

	var req struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Code        string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "code is required", "data": nil})
		return
	}

	// Auto-extract name/description from code if not provided
	if req.Name == "" || req.Description == "" {
		metaName, metaDesc := ExtractIndicatorMeta(req.Code)
		if req.Name == "" {
			req.Name = metaName
		}
		if req.Description == "" {
			req.Description = metaDesc
		}
	}
	if req.Name == "" {
		req.Name = "Custom Indicator"
	}

	// Parse params and strategy from code
	parsed := ParseSource(req.Code)
	paramsJSON, _ := json.Marshal(parsed.Params)
	strategyJSON, _ := json.Marshal(StrategyConfigToMap(parsed.StrategyConfig))

	now := time.Now().Unix()

	db := store.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "database not available", "data": nil})
		return
	}

	var indicatorID int
	if req.ID > 0 {
		// Update existing
		res, err := db.Exec(
			`UPDATE indicator_codes SET name = ?, description = ?, code = ?, params_json = ?, strategy_json = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
			req.Name, req.Description, req.Code, string(paramsJSON), string(strategyJSON), now, req.ID, uid,
		)
		if err != nil {
			log.Printf("update indicator failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error(), "data": nil})
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			c.JSON(http.StatusForbidden, gin.H{"code": 0, "msg": "indicator not found or not owned", "data": nil})
			return
		}
		indicatorID = req.ID
	} else {
		// Insert new
		res, err := db.Exec(
			`INSERT INTO indicator_codes (user_id, name, description, code, params_json, strategy_json, is_encrypted, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, 0, ?, ?)`,
			uid, req.Name, req.Description, req.Code, string(paramsJSON), string(strategyJSON), now, now,
		)
		if err != nil {
			log.Printf("insert indicator failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error(), "data": nil})
			return
		}
		lastID, _ := res.LastInsertId()
		indicatorID = int(lastID)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":      indicatorID,
		"success": true,
	})
}

// ListIndicators godoc
// GET /api/indicator/list
// Returns all indicators for the current user with community metrics.
func ListIndicators(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized", "data": nil})
		return
	}

	db := store.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "database not available", "data": nil})
		return
	}

	rows, err := db.Query(
		`SELECT id, user_id, name, description, code, params_json, strategy_json, is_encrypted,
			pricing_type, price, purchase_count, avg_rating, rating_count, view_count, review_status,
			created_at, updated_at
		 FROM indicator_codes WHERE user_id = ? ORDER BY updated_at DESC`,
		uid,
	)
	if err != nil {
		log.Printf("list indicators failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error(), "data": nil})
		return
	}
	defer rows.Close()

	var indicators []SavedIndicator
	for rows.Next() {
		var ind SavedIndicator
		var paramsJSON, strategyJSON sql.NullString
		var desc sql.NullString
		var pricingType, reviewStatus sql.NullString
		var price, avgRating sql.NullFloat64
		var purchaseCount, ratingCount, viewCount sql.NullInt64
		err := rows.Scan(
			&ind.ID, &ind.UserID, &ind.Name, &desc, &ind.Code,
			&paramsJSON, &strategyJSON, &ind.IsEncrypted,
			&pricingType, &price, &purchaseCount, &avgRating, &ratingCount, &viewCount, &reviewStatus,
			&ind.CreatedAt, &ind.UpdatedAt,
		)
		if err != nil {
			continue
		}
		ind.ParamsJSON = paramsJSON.String
		ind.StrategyJSON = strategyJSON.String
		ind.Description = desc.String
		ind.PricingType = pricingType.String
		ind.Price = price.Float64
		ind.PurchaseCount = int(purchaseCount.Int64)
		ind.AvgRating = avgRating.Float64
		ind.RatingCount = int(ratingCount.Int64)
		ind.ViewCount = int(viewCount.Int64)
		ind.ReviewStatus = reviewStatus.String
		if ind.ReviewStatus == "" {
			ind.ReviewStatus = "draft"
		}
		ind.Revenue = float64(ind.PurchaseCount) * ind.Price
		ind.Status = ind.ReviewStatus
		indicators = append(indicators, ind)
	}

	// Fetch current user info for author field
	username := c.GetString("username")
	user := store.FindUserByUsername(username)
	author := map[string]any{
		"username": username,
		"nickname": "",
		"avatar":   "",
	}
	if user != nil {
		if n, ok := user["nickname"].(string); ok {
			author["nickname"] = n
		}
		if a, ok := user["avatar"].(string); ok {
			author["avatar"] = a
		}
	}

	items := make([]map[string]any, 0, len(indicators))
	for _, ind := range indicators {
		items = append(items, map[string]any{
			"id":                    ind.ID,
			"name":                  ind.Name,
			"description":           ind.Description,
			"pricing_type":          ind.PricingType,
			"price":                 ind.Price,
			"vip_free":              false,
			"score":                 0,
			"sample_size":           0,
			"total_return":          ind.TotalReturn,
			"sharpe":                ind.Sharpe,
			"max_drawdown":          ind.MaxDrawdown,
			"applicable_symbols":    []string{},
			"applicable_timeframes": []string{},
			"author":                author,
			"purchase_count":        ind.PurchaseCount,
			"avg_rating":            ind.AvgRating,
			"view_count":            ind.ViewCount,
			"created_at":            ind.CreatedAt,
			"is_purchased":          false,
			"is_own":                true,
			"review_status":         ind.ReviewStatus,
			"status":                ind.Status,
			"revenue":               ind.Revenue,
			"rating_count":          ind.RatingCount,
			"updated_at":            ind.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"items": items,
		"total": len(items),
	})
}

// DeleteIndicator godoc
// DELETE /api/indicator/:id
// Deletes an indicator owned by the current user.
func DeleteIndicator(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized", "data": nil})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "invalid id", "data": nil})
		return
	}

	db := store.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "database not available", "data": nil})
		return
	}

	res, err := db.Exec(
		`DELETE FROM indicator_codes WHERE id = ? AND user_id = ?`,
		id, uid,
	)
	if err != nil {
		log.Printf("delete indicator failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error(), "data": nil})
		return
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "msg": "indicator not found", "data": nil})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
	})
}

// GetIndicator godoc
// GET /api/indicator/:id
// Returns a single indicator by ID (must be owned by current user).
func GetIndicator(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized", "data": nil})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "invalid id", "data": nil})
		return
	}

	db := store.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "database not available", "data": nil})
		return
	}

	var ind SavedIndicator
	var paramsJSON, strategyJSON sql.NullString
	var desc sql.NullString
	var pricingType, reviewStatus sql.NullString
	var price, avgRating sql.NullFloat64
	var purchaseCount, ratingCount, viewCount sql.NullInt64
	err = db.QueryRow(
		`SELECT id, user_id, name, description, code, params_json, strategy_json, is_encrypted,
			pricing_type, price, purchase_count, avg_rating, rating_count, view_count, review_status,
			created_at, updated_at
		 FROM indicator_codes WHERE id = ? AND user_id = ?`,
		id, uid,
	).Scan(
		&ind.ID, &ind.UserID, &ind.Name, &desc, &ind.Code,
		&paramsJSON, &strategyJSON, &ind.IsEncrypted,
		&pricingType, &price, &purchaseCount, &avgRating, &ratingCount, &viewCount, &reviewStatus,
		&ind.CreatedAt, &ind.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "msg": "indicator not found", "data": nil})
		return
	}
	if err != nil {
		log.Printf("get indicator failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error(), "data": nil})
		return
	}

	ind.ParamsJSON = paramsJSON.String
	ind.StrategyJSON = strategyJSON.String
	ind.Description = desc.String
	ind.PricingType = pricingType.String
	ind.Price = price.Float64
	ind.PurchaseCount = int(purchaseCount.Int64)
	ind.AvgRating = avgRating.Float64
	ind.RatingCount = int(ratingCount.Int64)
	ind.ViewCount = int(viewCount.Int64)
	ind.ReviewStatus = reviewStatus.String
	if ind.ReviewStatus == "" {
		ind.ReviewStatus = "draft"
	}
	ind.Status = ind.ReviewStatus
	ind.Revenue = float64(ind.PurchaseCount) * ind.Price

	// Fetch author info
	username := c.GetString("username")
	user := store.FindUserByUsername(username)
	authorName := username
	if user != nil {
		if n, ok := user["nickname"].(string); ok && n != "" {
			authorName = n
		}
	}

	c.JSON(http.StatusOK, map[string]any{
		"id":                    ind.ID,
		"name":                  ind.Name,
		"description":           ind.Description,
		"code":                  ind.Code,
		"pricing_type":          ind.PricingType,
		"price":                 ind.Price,
		"score":                 0,
		"total_return":          ind.TotalReturn,
		"sharpe":                ind.Sharpe,
		"max_drawdown":          ind.MaxDrawdown,
		"win_rate":              0,
		"profit_factor":         0,
		"sample_size":           0,
		"applicable_symbols":    []string{},
		"applicable_timeframes": []string{},
		"author_id":             ind.UserID,
		"author_name":           authorName,
		"purchase_count":        ind.PurchaseCount,
		"avg_rating":            ind.AvgRating,
		"rating_count":          ind.RatingCount,
		"view_count":            ind.ViewCount,
		"created_at":            ind.CreatedAt,
		"updated_at":            ind.UpdatedAt,
		"is_purchased":          false,
		"is_own":                true,
	})
}

// ApplyParamDefaults godoc
// POST /api/indicator/applyParamDefaults
// Applies tuned parameter values back into # @param lines in source code.
func ApplyParamDefaults(c *gin.Context) {
	var req struct {
		Code           string         `json:"code" binding:"required"`
		IndicatorParams map[string]any `json:"indicatorParams"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": "code is required", "data": nil})
		return
	}

	newCode, changed := applyDefaultsToCode(req.Code, req.IndicatorParams)
	c.JSON(http.StatusOK, gin.H{
		"params": gin.H{
			"code":    newCode,
			"changed": changed,
		},
	})
}

// applyDefaultsToCode updates # @param default values in source code.
func applyDefaultsToCode(code string, params map[string]any) (string, bool) {
	if len(params) == 0 {
		return code, false
	}

	changed := false
	lines := strings.Split(code, "\n")
	for i, line := range lines {
		m := paramRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := m[1]
		typ := m[2]
		rest := m[4] // description + range/values

		if val, ok := params[name]; ok {
			newDefault := formatParamDefault(typ, val)
			if newDefault != "" {
				lines[i] = fmt.Sprintf("# @param %s %s %s %s", name, typ, newDefault, rest)
				changed = true
			}
		}
	}

	return strings.Join(lines, "\n"), changed
}

func formatParamDefault(typ string, val any) string {
	switch v := val.(type) {
	case float64:
		if typ == "int" {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case bool:
		return strconv.FormatBool(v)
	case string:
		return fmt.Sprintf(`"%s"`, v)
	default:
		return fmt.Sprintf(`"%v"`, val)
	}
}

// PublishIndicator godoc
// POST /api/indicator/publish
// Publishes an indicator to the community marketplace.
func PublishIndicator(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	var req struct {
		ID          int     `json:"id" binding:"required"`
		PricingType string  `json:"pricingType"`
		Price       float64 `json:"price"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	db := store.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "database not available"})
		return
	}

	// Get indicator name/description for translation
	var name, description string
	err := db.QueryRow(`SELECT name, description FROM indicator_codes WHERE id = ? AND user_id = ?`, req.ID, uid).Scan(&name, &description)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusForbidden, gin.H{"code": 0, "msg": "indicator not found or not owned"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	// Auto-translate on publish
	srcLang := DetectSourceLang(name + " " + description)
	nameI18n, descI18n, _ := TranslateIndicator(name, description, srcLang)
	nameI18nJSON, _ := json.Marshal(nameI18n)
	descI18nJSON, _ := json.Marshal(descI18n)

	if req.PricingType == "" {
		req.PricingType = "free"
	}
	now := time.Now().Unix()

	_, err = db.Exec(
		`UPDATE indicator_codes SET publish_to_community = 1, pricing_type = ?, price = ?, review_status = 'pending', source_language = ?, name_i18n = ?, description_i18n = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		req.PricingType, req.Price, srcLang, string(nameI18nJSON), string(descI18nJSON), now, req.ID, uid,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true})
}

// InternalCallIndicator godoc
// POST /api/indicator/internal-call
// Internal endpoint for sandbox call_indicator() — executes another indicator's code.
func InternalCallIndicator(c *gin.Context) {
	var req struct {
		IndicatorRef int              `json:"indicator_ref"`
		DfJSON       []map[string]any `json:"df_json"`
		Params       map[string]any   `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	// Get indicator code from DB (published or own)
	db := store.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "database not available"})
		return
	}

	var code string
	err := db.QueryRow(
		`SELECT code FROM indicator_codes WHERE id = ? AND (publish_to_community = 1 OR user_id = 0)`,
		req.IndicatorRef,
	).Scan(&code)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"code": 0, "msg": "indicator not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	// Execute in sandbox
	sandbox := DefaultSandboxConfig()
	result, err := sandbox.Execute(code, req.Params, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	if !result.Success {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": result.Msg, "error": result.Error, "error_type": result.ErrorType})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "success", "data": result.Output})
}

// GetIndicators godoc
// POST /api/indicator/getIndicators
// Legacy alias for ListIndicators — returns all indicators for the current user.
func GetIndicators(c *gin.Context) {
	// Delegate to ListIndicators logic
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized", "data": nil})
		return
	}

	db := store.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "database not available", "data": nil})
		return
	}

	rows, err := db.Query(
		`SELECT id, user_id, name, description, code, params_json, strategy_json, is_encrypted,
			pricing_type, price, purchase_count, avg_rating, rating_count, view_count, review_status,
			created_at, updated_at
		 FROM indicator_codes WHERE user_id = ? ORDER BY updated_at DESC`,
		uid,
	)
	if err != nil {
		log.Printf("list indicators failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error(), "data": nil})
		return
	}
	defer rows.Close()

	var indicators []SavedIndicator
	for rows.Next() {
		var ind SavedIndicator
		var paramsJSON, strategyJSON sql.NullString
		var desc sql.NullString
		var pricingType, reviewStatus sql.NullString
		var price, avgRating sql.NullFloat64
		var purchaseCount, ratingCount, viewCount sql.NullInt64
		err := rows.Scan(
			&ind.ID, &ind.UserID, &ind.Name, &desc, &ind.Code,
			&paramsJSON, &strategyJSON, &ind.IsEncrypted,
			&pricingType, &price, &purchaseCount, &avgRating, &ratingCount, &viewCount, &reviewStatus,
			&ind.CreatedAt, &ind.UpdatedAt,
		)
		if err != nil {
			continue
		}
		ind.ParamsJSON = paramsJSON.String
		ind.StrategyJSON = strategyJSON.String
		ind.Description = desc.String
		ind.PricingType = pricingType.String
		ind.Price = price.Float64
		ind.PurchaseCount = int(purchaseCount.Int64)
		ind.AvgRating = avgRating.Float64
		ind.RatingCount = int(ratingCount.Int64)
		ind.ViewCount = int(viewCount.Int64)
		ind.ReviewStatus = reviewStatus.String
		if ind.ReviewStatus == "" {
			ind.ReviewStatus = "draft"
		}
		ind.Status = ind.ReviewStatus
		ind.Revenue = float64(ind.PurchaseCount) * ind.Price
		indicators = append(indicators, ind)
	}

	c.JSON(http.StatusOK, indicators)
}

// DecryptIndicator godoc
// POST /api/indicator/getDecryptKey
// Returns a decryption key for an encrypted indicator (placeholder — actual encryption TBD).
func DecryptIndicator(c *gin.Context) {
	var req struct {
		UserID      int `json:"user_id"`
		IndicatorID int `json:"indicator_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error(), "success": false})
		return
	}

	// Placeholder: return a deterministic "key" based on IDs
	key := fmt.Sprintf("xt-decrypt-%d-%d-%d", req.UserID, req.IndicatorID, time.Now().Unix())
	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "success", "key": key, "success": true})
}

// GetWatchlist godoc
// GET /api/watchlist
// Returns the user's indicator watchlist.
func GetWatchlist(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	db := store.GetDB()
	if db == nil {
		c.JSON(http.StatusOK, gin.H{"items": []SavedIndicator{}})
		return
	}

	rows, err := db.Query(
		`SELECT i.id, i.user_id, i.name, i.description, i.code, i.params_json, i.strategy_json,
			i.is_encrypted, i.pricing_type, i.price, i.purchase_count, i.avg_rating, i.rating_count,
			i.view_count, i.review_status, i.created_at, i.updated_at
		 FROM indicator_codes i
		 JOIN user_watchlist w ON i.id = w.indicator_id
		 WHERE w.user_id = ? ORDER BY w.created_at DESC`,
		uid,
	)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"items": []SavedIndicator{}})
		return
	}
	defer rows.Close()

	var items []SavedIndicator
	for rows.Next() {
		var ind SavedIndicator
		var paramsJSON, strategyJSON sql.NullString
		var desc sql.NullString
		var pricingType, reviewStatus sql.NullString
		var price, avgRating sql.NullFloat64
		var purchaseCount, ratingCount, viewCount sql.NullInt64
		err := rows.Scan(
			&ind.ID, &ind.UserID, &ind.Name, &desc, &ind.Code,
			&paramsJSON, &strategyJSON, &ind.IsEncrypted,
			&pricingType, &price, &purchaseCount, &avgRating, &ratingCount, &viewCount, &reviewStatus,
			&ind.CreatedAt, &ind.UpdatedAt,
		)
		if err != nil {
			continue
		}
		ind.ParamsJSON = paramsJSON.String
		ind.StrategyJSON = strategyJSON.String
		ind.Description = desc.String
		ind.PricingType = pricingType.String
		ind.Price = price.Float64
		ind.PurchaseCount = int(purchaseCount.Int64)
		ind.AvgRating = avgRating.Float64
		ind.RatingCount = int(ratingCount.Int64)
		ind.ViewCount = int(viewCount.Int64)
		ind.ReviewStatus = reviewStatus.String
		if ind.ReviewStatus == "" {
			ind.ReviewStatus = "draft"
		}
		ind.Status = ind.ReviewStatus
		ind.Revenue = float64(ind.PurchaseCount) * ind.Price
		items = append(items, ind)
	}

	// Fetch current user info for author field
	username := c.GetString("username")
	user := store.FindUserByUsername(username)
	author := map[string]any{
		"username": username,
		"nickname": "",
		"avatar":   "",
	}
	if user != nil {
		if n, ok := user["nickname"].(string); ok && n != "" {
			author["nickname"] = n
		}
		if a, ok := user["avatar"].(string); ok && a != "" {
			author["avatar"] = a
		}
	}

	result := make([]map[string]any, 0, len(items))
	for _, ind := range items {
		result = append(result, map[string]any{
			"id":                    ind.ID,
			"name":                  ind.Name,
			"description":           ind.Description,
			"pricing_type":          ind.PricingType,
			"price":                 ind.Price,
			"vip_free":              false,
			"score":                 0,
			"sample_size":           0,
			"total_return":          ind.TotalReturn,
			"sharpe":                ind.Sharpe,
			"max_drawdown":          ind.MaxDrawdown,
			"applicable_symbols":    []string{},
			"applicable_timeframes": []string{},
			"author":                author,
			"purchase_count":        ind.PurchaseCount,
			"avg_rating":            ind.AvgRating,
			"view_count":            ind.ViewCount,
			"created_at":            ind.CreatedAt,
			"is_purchased":          false,
			"is_own":                true,
			"review_status":         ind.ReviewStatus,
			"status":                ind.Status,
			"revenue":               ind.Revenue,
			"rating_count":          ind.RatingCount,
			"updated_at":            ind.UpdatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"items": result})
}

// AddWatchlist godoc
// POST /api/watchlist
// Adds an indicator to the user's watchlist.
func AddWatchlist(c *gin.Context) {
	userID, _ := c.Get(middleware.UserIDKey)
	uid, ok := userID.(int)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 0, "msg": "unauthorized"})
		return
	}

	var req struct {
		IndicatorID int `json:"indicator_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	db := store.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": "database not available"})
		return
	}

	_, err := db.Exec(
		`INSERT OR IGNORE INTO user_watchlist (user_id, indicator_id, created_at) VALUES (?, ?, ?)`,
		uid, req.IndicatorID, time.Now().Unix(),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "added to watchlist", "success": true})
}

// GetIndicatorKline godoc
// GET /api/indicator/kline
// Returns kline data for an indicator's symbol (proxy to market klines).
func GetIndicatorKline(c *gin.Context) {
	symbol := c.DefaultQuery("symbol", "BTCUSDT")
	_ = c.DefaultQuery("interval", "1h") // reserved for future use
	limit := 200
	if l, err := fmt.Sscanf(c.DefaultQuery("limit", "200"), "%d", &limit); l != 1 || err != nil {
		limit = 200
	}

	// Reuse the existing fetchBinanceKlines helper from handler package
	// Since we're in the indicator package, we call it via a simple HTTP request or duplicate logic.
	// For simplicity, return a placeholder that the frontend can fall back from.
	c.JSON(http.StatusOK, gin.H{
		"klines": []map[string]any{},
		"symbol": symbol,
	})
}
