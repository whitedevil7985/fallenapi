/*
 * TgMusicBot - Telegram Music Bot
 * Copyright (c) 2025-2026 Ashok Shau
 *
 * Licensed under GNU GPL v3
 * See https://github.com/AshokShau/TgMusicBot
 */

package bot

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	td "github.com/AshokShau/gotdbot"
)

const herokuAPIBase = "https://api.heroku.com"

// restartHandler restarts the Heroku worker formation. Access is restricted
// to the Owner and Sudo users already loaded into config.DEVS.
func restartHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	apiKey := strings.TrimSpace(os.Getenv("HEROKU_API_KEY"))
	appName := strings.TrimSpace(os.Getenv("HEROKU_APP_NAME"))
	if apiKey == "" || appName == "" {
		_, err := m.ReplyText(c,
			"⚠️ Restart is not configured. Set HEROKU_API_KEY and HEROKU_APP_NAME in Heroku Config Vars.",
			nil,
		)
		return err
	}

	endpoint := herokuAPIBase + "/apps/" + url.PathEscape(appName) + "/formations/worker"
	req, err := http.NewRequest(http.MethodDelete, endpoint, nil)
	if err != nil {
		_, _ = m.ReplyText(c, "❌ Could not prepare the Heroku restart request.", nil)
		return err
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/vnd.heroku+json; version=3")
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		c.Logger.Error("Heroku worker restart request failed", "app", appName, "error", err)
		_, _ = m.ReplyText(c, "❌ Heroku restart request failed. Check the API key, app name, and network connection.", nil)
		return nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		c.Logger.Error("Heroku rejected worker restart request",
			"app", appName,
			"status", resp.StatusCode,
			"response", strings.TrimSpace(string(body)),
		)
		_, _ = m.ReplyText(c,
			fmt.Sprintf("❌ Heroku rejected the restart request (HTTP %d). Check the API key permissions and app name.", resp.StatusCode),
			nil,
		)
		return nil
	}

	c.Logger.Warn("Heroku worker restart accepted",
		"app", appName,
		"requested_by", m.SenderID(),
		"status", resp.StatusCode,
	)

	_, err = m.ReplyText(c,
		"♻️ <b>Worker restart accepted!</b>\n\nThe bot will go offline briefly and reconnect automatically.",
		&td.SendMessageOpts{ParseMode: "HTML"},
	)
	return err
}
