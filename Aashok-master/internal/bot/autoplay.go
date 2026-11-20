/*
 * TgMusicBot - Telegram Music Bot
 *  Copyright (c) 2025-2026 Ashok Shau
 *
 *  Licensed under GNU GPL v3
 *  See https://github.com/AshokShau/TgMusicBot
 */

package bot

import (
	"ashokshau/tgmusic/internal/cache"
	"ashokshau/tgmusic/internal/utils"
	"fmt"
	"html"
	"strings"

	td "github.com/AshokShau/gotdbot"
)

func autoplayHandler(c *td.Client, m *td.Message) error {
	if !adminMode(c, m) {
		return td.EndGroups
	}

	chatID := m.ChatId

	if cache.ChatCache.GetPlayingTrack(chatID) == nil {
		_, err := m.ReplyText(c, "Bot is not streaming in the video chat.", nil)
		return err
	}

	state := cache.ChatCache.GetAutoplay(chatID)
	text := "<b>Autoplay Control</b>\n\nWhen autoplay is enabled, the bot will automatically play recommended songs from supported platforms when the queue is empty."
	button := autoplayButton(state)

	_, err := m.ReplyText(c, text, &td.SendMessageOpts{
		ParseMode:   "HTML",
		ReplyMarkup: button,
	})
	return err
}

func autoplayCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	if !adminModeCB(c, cb) {
		return nil
	}

	chatID := cb.ChatId
	if cache.ChatCache.GetPlayingTrack(chatID) == nil {
		_ = cb.Answer(c, "Bot is not streaming in the video chat.", &td.CallbackAnswerOpts{ShowAlert: true})
		return nil
	}

	state := cache.ChatCache.GetAutoplay(chatID)
	newState := !state
	cache.ChatCache.SetAutoplay(chatID, newState)

	var status string
	if newState {
		status = "ON 🟢"
	} else {
		status = "OFF 🔴"
	}
	_ = cb.Answer(c, fmt.Sprintf("Autoplay is now %s.", status), nil)

	mode := strings.TrimPrefix(cb.DataString(), "autoplay_toggle_")
	if mode == "" || mode == cb.DataString() {
		mode = "play"
	}
	track := cache.ChatCache.GetPlayingTrack(chatID)
	if track != nil {
		text := fmt.Sprintf("<u><b>| Started streaming</b></u>\\n\\n<b>Title:</b> <a href='%s'>%s</a>\\n\\n<b>Duration:</b> %s min\\n<b>Requested by:</b> %s",
			html.EscapeString(track.URL), html.EscapeString(track.Name), utils.SecToMin(track.Duration), html.EscapeString(track.User))
		_, _ = cb.EditMessageText(c, text, &td.EditMessageTextOpts{
			ParseMode: "HTML",
			DisableWebPagePreview: true,
			ReplyMarkup: utils.ControlButtonsWithAutoplay(mode, newState),
		})
	}
	return nil
}

func autoplayButton(state bool) *td.ReplyMarkupInlineKeyboard {
	var text string
	if state {
		text = "Autoplay: ON | ✅"
	} else {
		text = "Autoplay: OFF | ❌"
	}

	return &td.ReplyMarkupInlineKeyboard{
		Rows: [][]td.InlineKeyboardButton{
			{
				{
					Text: text,
					Type: &td.InlineKeyboardButtonTypeCallback{
						Data: []byte("autoplay_toggle"),
					},
				},
			},
		},
	}
}
