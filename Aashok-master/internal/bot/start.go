/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package bot

import (
	"ashokshau/tgmusic/internal/config"
	"ashokshau/tgmusic/internal/utils"
	"fmt"
	"runtime"
	"strings"
	"time"

	td "github.com/AshokShau/gotdbot"
)

func pingHandler(c *td.Client, m *td.Message) error {
	deleteCmd(c, m)

	start := time.Now()

	msg, err := m.ReplyText(c, "Pinging… please wait…", nil)
	if err != nil {
		return err
	}

	latency := time.Since(start).Milliseconds()
	uptime := getFormattedDuration(time.Since(startTime))

	response := fmt.Sprintf(
		"<b>📊 System Performance Metrics</b>\n\n"+
			"<b>Bot Latency:</b> <code>%d ms</code>\n"+
			"<b>Uptime:</b> <code>%s</code>\n"+
			"<b>Go Routines:</b> <code>%d</code>\n",
		latency, uptime, runtime.NumGoroutine(),
	)

	_, err = msg.EditText(c, response, &td.EditMessageTextOpts{ParseMode: "HTML"})
	return err
}

func startHandler(c *td.Client, m *td.Message) error {
	chatID := m.ChatId
	go storeChatToDB(chatID)

	deleteCmd(c, m)

	args := Args(m)
	if m.IsPrivate() && (args == "help" || strings.HasPrefix(args, "help_")) {
		return helpHandler(c, m)
	}

	if m.IsPrivate() {
		response := fmt.Sprintf(
			"<b>Welcome, %s!</b>\n\n"+
				"<b>%s</b> lets you stream high-quality music and video directly in Telegram voice and video chats.\n\n"+
				"<b>Supported platforms:</b> YouTube, Spotify, Apple Music, SoundCloud, Deezer, Twitch, and many more.\n\n"+
				"Use the buttons below to add the bot to your group or explore the available commands.",
			firstName(c, m),
			c.Me().FirstName,
		)

		_, err := m.ReplyText(c, response, &td.SendMessageOpts{
			ParseMode:       "HTML",
			Url:             config.StartImg,
			ForceLargeMedia: true,
			ShowAboveText:   true,
			ReplyMarkup:     utils.AddMeMarkup(c.Me().Usernames.EditableUsername),
		})

		return err
	}

	uptime := getFormattedDuration(time.Since(startTime))
	response := fmt.Sprintf(
		"<b>%s is ready!</b>\n\n"+
			"<b>Uptime:</b> <code>%s</code>\n\n"+
			"<i>A feature-rich music bot for your group video chats. Play your favorite tracks seamlessly.</i>",
		c.Me().FirstName,
		uptime,
	)

	_, err := m.ReplyText(c, response, &td.SendMessageOpts{
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
		ReplyMarkup:           utils.SupportBtn(),
	})
	return err
}
