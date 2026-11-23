/*
 * TgMusicBot - Telegram Music Bot
 * Copyright (c) 2025-2026 Ashok Shau
 *
 * Licensed under GNU GPL v3
 * See https://github.com/AshokShau/TgMusicBot
 */

package bot

import (
	"os"
	"time"

	td "github.com/AshokShau/gotdbot"
)

// restartHandler exits the worker process after confirming the request.
// Heroku's Dyno Manager automatically restarts a dyno when its command exits.
func restartHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}

	_, err := m.ReplyText(c,
		"♻️ <b>Restarting worker...</b>\n\nThe bot will be offline briefly while Heroku starts it again.",
		&td.SendMessageOpts{ParseMode: "HTML"},
	)
	if err != nil {
		c.Logger.Error("Could not send restart confirmation", "error", err)
		return err
	}

	c.Logger.Warn("Worker restart requested",
		"requested_by", m.SenderID(),
	)

	// Give Telegram a moment to deliver the confirmation before exiting.
	time.AfterFunc(2*time.Second, func() {
		os.Exit(0)
	})

	return nil
}
