package bot

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ── Discord Bot ────────────────────────────────────────────────

// DiscordBot sends notifications via webhook and handles slash commands.
type DiscordBot struct {
	webhookURL  string
	publicKey   string
	commands    Commands
	provider    BotStateProvider
	client      *http.Client
	enabled     bool
}

// DiscordConfig configures the Discord bot.
type DiscordConfig struct {
	WebhookURL string
	PublicKey  string // for slash command verification
	Token      string // bot token (for advanced features)
}

// NewDiscordBot creates a new Discord bot.
func NewDiscordBot(cfg DiscordConfig, provider BotStateProvider, cmds Commands) *DiscordBot {
	if cfg.WebhookURL == "" && cfg.PublicKey == "" {
		return nil
	}
	return &DiscordBot{
		webhookURL: cfg.WebhookURL,
		publicKey:  cfg.PublicKey,
		commands:   cmds,
		provider:   provider,
		client:     &http.Client{Timeout: 10 * time.Second},
		enabled:    true,
	}
}

// Send sends a message to the Discord webhook.
func (d *DiscordBot) Send(content string) error {
	if d == nil || !d.enabled || d.webhookURL == "" {
		return nil
	}

	payload := map[string]any{
		"content": content,
	}
	body, _ := json.Marshal(payload)

	resp, err := d.client.Post(d.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord error %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// SendEmbed sends a rich embed message to Discord.
func (d *DiscordBot) SendEmbed(title, description string, color int, fields map[string]string) error {
	if d == nil || !d.enabled || d.webhookURL == "" {
		return nil
	}

	embed := map[string]any{
		"title":       title,
		"description": description,
		"color":       color,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	if len(fields) > 0 {
		var embedFields []map[string]any
		for name, value := range fields {
			embedFields = append(embedFields, map[string]any{
				"name":   name,
				"value":  value,
				"inline": true,
			})
		}
		embed["fields"] = embedFields
	}

	payload := map[string]any{"embeds": []map[string]any{embed}}
	body, _ := json.Marshal(payload)

	resp, err := d.client.Post(d.webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("discord embed: %w", err)
	}
	defer resp.Body.Close()
	return nil
}

// NotifyRisk sends a risk alert as a Discord embed.
func (d *DiscordBot) NotifyRisk(level, message string) {
	color := 0xFFA500 // orange for WARN
	switch level {
	case "CRITICAL":
		color = 0xFF0000 // red
	case "INFO":
		color = 0x3498DB // blue
	}
	d.SendEmbed("Risk Alert: "+level, message, color, nil)
}

// NotifyTrade sends a trade notification.
func (d *DiscordBot) NotifyTrade(symbol, side string, price, qty float64) {
	color := 0x2ECC71 // green for buy
	if side == "SELL" || side == "SHORT" {
		color = 0xE74C3C // red for sell
	}
	fields := map[string]string{
		"Symbol":   symbol,
		"Side":     side,
		"Price":    fmt.Sprintf("%.2f", price),
		"Quantity": fmt.Sprintf("%.4f", qty),
	}
	d.SendEmbed("Trade Executed", side+" "+symbol, color, fields)
}

// ── Slash Command Handler ─────────────────────────────────────

// HandleInteraction verifies and handles Discord slash commands.
func (d *DiscordBot) HandleInteraction(w http.ResponseWriter, r *http.Request) {
	if d.publicKey == "" {
		http.Error(w, "not configured", http.StatusNotImplemented)
		return
	}

	// Verify signature
	if !d.verifySignature(r) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	body, _ := io.ReadAll(r.Body)
	var interaction struct {
		Type int    `json:"type"`
		Data struct {
			Name    string `json:"name"`
			Options []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"options"`
		} `json:"data"`
		Token    string `json:"token"`
		ID       string `json:"id"`
	}
	json.Unmarshal(body, &interaction)

	// PING (type 1) — Discord verification
	if interaction.Type == 1 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"type": 1})
		return
	}

	// Slash command (type 2)
	if interaction.Type == 2 {
		var response string
		switch interaction.Data.Name {
		case "status":
			if d.provider != nil {
				rs := d.provider.GetRiskStatus()
				eq := d.provider.GetEquity()
				response = fmt.Sprintf("**Status** | Equity: $%.2f | Drawdown: %.1f%% | CB: %s",
					eq, rs.DrawdownPct, rs.CircuitBreaker)
			}
		case "profit":
			if d.provider != nil {
				eq := d.provider.GetEquity()
				response = fmt.Sprintf("**Equity**: $%.2f", eq)
			}
		case "positions":
			if d.provider != nil {
				positions := d.provider.GetOpenPositions()
				if len(positions) == 0 {
					response = "No open positions"
				} else {
					for _, p := range positions {
						response += fmt.Sprintf("`%s` %s: %.2f @ %.2f PnL: %.2f\n",
							p.Symbol, p.Side, p.Quantity, p.EntryPrice, p.PnL)
					}
				}
			}
		case "pause":
			if d.commands.OnPause != nil {
				d.commands.OnPause()
				response = "⏸️ Trading paused"
			}
		case "resume":
			if d.commands.OnResume != nil {
				d.commands.OnResume()
				response = "▶️ Trading resumed"
			}
		default:
			response = "Unknown command. Available: /status, /profit, /positions, /pause, /resume"
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"type": 4,
			"data": map[string]string{"content": response},
		})
		return
	}
}

// verifySignature verifies Discord's Ed25519 signature.
func (d *DiscordBot) verifySignature(r *http.Request) bool {
	signature := r.Header.Get("X-Signature-Ed25519")
	timestamp := r.Header.Get("X-Signature-Timestamp")

	if signature == "" || timestamp == "" {
		return false
	}

	pubKey, err := hex.DecodeString(d.publicKey)
	if err != nil {
		return false
	}

	sig, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}

	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body)) // restore body

	msg := append([]byte(timestamp), body...)
	return ed25519.Verify(pubKey, msg, sig)
}

// IsEnabled returns whether the Discord bot is active.
func (d *DiscordBot) IsEnabled() bool {
	return d != nil && d.enabled
}

// ConfigFromEnv creates a DiscordConfig from environment variables.
func DiscordConfigFromEnv() DiscordConfig {
	return DiscordConfig{
		WebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
		PublicKey:  os.Getenv("DISCORD_PUBLIC_KEY"),
		Token:      os.Getenv("DISCORD_BOT_TOKEN"),
	}
}
