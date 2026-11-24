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
	"ashokshau/tgmusic/internal/db"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	td "github.com/AshokShau/gotdbot"
)

var (
	broadcastCancelFlag atomic.Bool
	broadcastInProgress atomic.Bool

	broadcastMu       sync.Mutex
	pendingBroadcasts = make(map[string]*pendingBroadcast)
	activeBroadcastID string
)

type pendingBroadcast struct {
	ownerID  int64
	reply    *td.Message
	status   *td.Message
	targets  []int64
	groups   map[int64]bool
	copyMode bool
	mode     string
}

func getFloodWait(err error) int {
	if err == nil {
		return 0
	}

	type retryError interface {
		GetRetryAfter() int
	}

	if re, ok := err.(retryError); ok {
		return re.GetRetryAfter()
	}
	if tdErr, ok := err.(*td.Error); ok {
		return tdErr.GetRetryAfter()
	}
	if tdErr, ok := err.(td.Error); ok {
		return tdErr.GetRetryAfter()
	}
	return 0
}

func broadcastKeyboard(id string) *td.ReplyMarkupInlineKeyboard {
	button := func(label, data string) td.InlineKeyboardButton {
		return td.InlineKeyboardButton{
			Text: label,
			Type: &td.InlineKeyboardButtonTypeCallback{Data: []byte(data)},
		}
	}
	return &td.ReplyMarkupInlineKeyboard{
		Rows: [][]td.InlineKeyboardButton{
			{
				button("✅ Confirm & Send", "broadcast_confirm_"+id),
				button("✖ Cancel", "broadcast_cancel_"+id),
			},
		},
	}
}

func broadcastProgressKeyboard(id string) *td.ReplyMarkupInlineKeyboard {
	return &td.ReplyMarkupInlineKeyboard{
		Rows: [][]td.InlineKeyboardButton{{
			{
				Text: "⏹ Stop Broadcast",
				Type: &td.InlineKeyboardButtonTypeCallback{Data: []byte("broadcast_stop_" + id)},
			},
		}},
	}
}

func isBroadcastOwner(userID int64) bool {
	for _, dev := range config.DEVS {
		if dev == userID {
			return true
		}
	}
	return false
}

func cancelBroadcastHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}
	if !broadcastInProgress.Load() {
		_, _ = m.ReplyText(c, "No broadcast in progress.", nil)
		return td.EndGroups
	}
	broadcastCancelFlag.Store(true)
	_, _ = m.ReplyText(c, "⏹ Stopping broadcast...", nil)
	return td.EndGroups
}

func broadcastHandler(c *td.Client, m *td.Message) error {
	if !isDev(c, m) {
		return td.EndGroups
	}
	if broadcastInProgress.Load() {
		_, _ = m.ReplyText(c, "A broadcast is already in progress.", nil)
		return td.EndGroups
	}

	reply, err := getRepliedMessage(c, m)
	if err != nil {
		_, _ = m.ReplyText(c, "Please reply to the message you want to broadcast.", nil)
		return td.EndGroups
	}

	mode := "both"
	copyMode := false
	for _, arg := range strings.Fields(Args(m)) {
		switch arg {
		case "-chat":
			mode = "chat"
		case "-user":
			mode = "user"
		case "-both":
			mode = "both"
		case "-copy":
			copyMode = true
		default:
			_, _ = m.ReplyText(c, "Unknown option: "+arg+"\nUse /broadcast -chat, -user, -both or -copy.", nil)
			return td.EndGroups
		}
	}

	var chats, users []int64
	if mode == "chat" || mode == "both" {
		chats, err = db.Instance.GetAllChats()
		if err != nil {
			c.Logger.Error("Broadcast: failed to load groups", "error", err)
			_, _ = m.ReplyText(c, "Could not load group list from database. Broadcast was not started.", nil)
			return td.EndGroups
		}
	}
	if mode == "user" || mode == "both" {
		users, err = db.Instance.GetAllUsers()
		if err != nil {
			c.Logger.Error("Broadcast: failed to load users", "error", err)
			_, _ = m.ReplyText(c, "Could not load user list from database. Broadcast was not started.", nil)
			return td.EndGroups
		}
	}

	groups := make(map[int64]bool, len(chats))
	for _, id := range chats {
		groups[id] = true
	}

	// De-duplicate IDs before showing the preview or sending.
	targets := make([]int64, 0, len(chats)+len(users))
	seen := make(map[int64]bool, len(chats)+len(users))
	for _, id := range append(chats, users...) {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		targets = append(targets, id)
	}
	if len(targets) == 0 {
		_, _ = m.ReplyText(c, "No broadcast targets found for the selected audience.", nil)
		return td.EndGroups
	}

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	preview := fmt.Sprintf(
		"📢 <b>Broadcast Preview</b>\n\n"+
			"🎯 <b>Audience:</b> %s\n"+
			"👥 <b>Groups:</b> %d\n"+
			"👤 <b>Users:</b> %d\n"+
			"📦 <b>Total recipients:</b> %d\n"+
			"📨 <b>Delivery:</b> %s\n\n"+
			"Confirm to start sending this replied message.",
		strings.ToUpper(mode), len(chats), len(users), len(targets),
		map[bool]string{true: "Copy", false: "Forward"}[copyMode],
	)
	status, err := m.ReplyText(c, preview, &td.SendMessageOpts{
		ParseMode:   td.ParseModeHTML,
		ReplyMarkup: broadcastKeyboard(id),
	})
	if err != nil {
		return err
	}

	broadcastMu.Lock()
	pendingBroadcasts[id] = &pendingBroadcast{
		ownerID: m.SenderID(), reply: reply, status: status,
		targets: targets, groups: groups, copyMode: copyMode, mode: mode,
	}
	broadcastMu.Unlock()
	return td.EndGroups
}

func broadcastControlCallbackHandler(c *td.Client, cb *td.UpdateNewCallbackQuery) error {
	if !isBroadcastOwner(cb.SenderUserId) {
		return cb.Answer(c, "You are not allowed to control broadcasts.", &td.CallbackAnswerOpts{ShowAlert: true})
	}

	data := cb.DataString()
	parts := strings.SplitN(data, "_", 3)
	if len(parts) != 3 || parts[0] != "broadcast" {
		return cb.Answer(c, "Invalid broadcast action.", nil)
	}
	actionAndID := strings.SplitN(parts[2], "_", 2)
	if len(actionAndID) != 2 {
		return cb.Answer(c, "Invalid broadcast action.", nil)
	}
	action, id := actionAndID[0], actionAndID[1]

	broadcastMu.Lock()
	pending := pendingBroadcasts[id]
	currentID := activeBroadcastID
	broadcastMu.Unlock()

	switch action {
	case "cancel":
		if pending == nil || pending.ownerID != cb.SenderUserId {
			return cb.Answer(c, "This preview has expired.", &td.CallbackAnswerOpts{ShowAlert: true})
		}
		broadcastMu.Lock()
		delete(pendingBroadcasts, id)
		broadcastMu.Unlock()
		_, _ = pending.status.EditText(c, "❌ Broadcast cancelled.", nil)
		return cb.Answer(c, "Broadcast cancelled.", nil)

	case "confirm":
		if pending == nil || pending.ownerID != cb.SenderUserId {
			return cb.Answer(c, "This preview has expired.", &td.CallbackAnswerOpts{ShowAlert: true})
		}
		if !broadcastInProgress.CompareAndSwap(false, true) {
			return cb.Answer(c, "Another broadcast is already running.", &td.CallbackAnswerOpts{ShowAlert: true})
		}
		broadcastMu.Lock()
		delete(pendingBroadcasts, id)
		activeBroadcastID = id
		broadcastMu.Unlock()
		broadcastCancelFlag.Store(false)
		_ = cb.Answer(c, "Broadcast started.", nil)
		_, _ = pending.status.EditText(c,
			fmt.Sprintf("🚀 <b>Broadcast started</b>\nAudience: %s\nTotal: %d\nSent: 0 | Failed: 0", strings.ToUpper(pending.mode), len(pending.targets)),
			&td.EditMessageTextOpts{ParseMode: td.ParseModeHTML, ReplyMarkup: broadcastProgressKeyboard(id)})
		go runBroadcast(c, pending, id)
		return nil

	case "stop":
		if currentID != id || !broadcastInProgress.Load() {
			return cb.Answer(c, "This broadcast is no longer active.", &td.CallbackAnswerOpts{ShowAlert: true})
		}
		broadcastCancelFlag.Store(true)
		return cb.Answer(c, "Stopping broadcast...", nil)
	default:
		return cb.Answer(c, "Unknown broadcast action.", nil)
	}
}

func broadcastSleep(duration time.Duration) bool {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if broadcastCancelFlag.Load() {
			return false
		}
		remaining := time.Until(deadline)
		if remaining > 250*time.Millisecond {
			remaining = 250 * time.Millisecond
		}
		time.Sleep(remaining)
	}
	return !broadcastCancelFlag.Load()
}

func runBroadcast(c *td.Client, job *pendingBroadcast, id string) {
	defer func() {
		broadcastMu.Lock()
		if activeBroadcastID == id {
			activeBroadcastID = ""
		}
		broadcastMu.Unlock()
		broadcastInProgress.Store(false)
	}()

	total := len(job.targets)
	sent, failed, groupSent, userSent := 0, 0, 0, 0
	var failedBuilder strings.Builder
	stopped := false
	lastUpdate := time.Now()

	update := func(final bool) {
		percent := 100
		if total > 0 {
			percent = (sent + failed) * 100 / total
		}
		state := "📢 <b>Broadcast in progress</b>"
		if final && stopped {
			state = "⏹ <b>Broadcast stopped</b>"
		} else if final {
			state = "✅ <b>Broadcast completed</b>"
		}
		text := fmt.Sprintf(
			"%s\n\n🎯 Audience: %s\n📊 Progress: %d%% (%d/%d)\n✅ Sent: %d\n❌ Failed: %d\n👥 Groups: %d\n👤 Users: %d",
			state, strings.ToUpper(job.mode), percent, sent+failed, total,
			sent, failed, groupSent, userSent,
		)
		markup := broadcastProgressKeyboard(id)
		if final {
			markup = nil
		}
		_, _ = job.status.EditText(c, text, &td.EditMessageTextOpts{ParseMode: td.ParseModeHTML, ReplyMarkup: markup})
		lastUpdate = time.Now()
	}

	for _, chatID := range job.targets {
		if broadcastCancelFlag.Load() {
			stopped = true
			break
		}

		for {
			var sendErr error
			if job.copyMode {
				_, sendErr = job.reply.Copy(c, chatID, &td.CopyMessageOpts{ReplyMarkup: job.reply.ReplyMarkup})
			} else {
				_, sendErr = job.reply.Forward(c, chatID, &td.ForwardMessageOpts{})
			}

			if sendErr == nil {
				sent++
				if job.groups[chatID] {
					groupSent++
				} else {
					userSent++
				}
				break
			}

			if wait := getFloodWait(sendErr); wait > 0 {
				_, _ = job.status.EditText(c,
					fmt.Sprintf("⏳ <b>Telegram rate limit</b>\nWaiting %d seconds, then retrying the same recipient.\n\n✅ Sent: %d | ❌ Failed: %d", wait, sent, failed),
					&td.EditMessageTextOpts{ParseMode: td.ParseModeHTML, ReplyMarkup: broadcastProgressKeyboard(id)})
				if !broadcastSleep(time.Duration(wait+1) * time.Second) {
					stopped = true
					break
				}
				continue
			}

			failed++
			failedBuilder.WriteString(fmt.Sprintf("%d - %v\n", chatID, sendErr))
			break
		}

		if stopped {
			break
		}
		if time.Since(lastUpdate) >= 5*time.Second || (sent+failed)%25 == 0 {
			update(false)
		}
		if !broadcastSleep(200 * time.Millisecond) {
			stopped = true
			break
		}
	}

	update(true)
	failedText := failedBuilder.String()
	if failedText != "" {
		path := filepath.Join(os.TempDir(), fmt.Sprintf("broadcast_failed_%s.txt", id))
		if err := os.WriteFile(path, []byte(failedText), 0600); err == nil {
			_, sendErr := job.status.ReplyDocument(c, td.InputFileLocal{Path: path}, &td.SendDocumentOpts{
				Caption: fmt.Sprintf("📄 Failed recipients: %d", failed),
			})
			_ = os.Remove(path)
			if sendErr != nil {
				c.Logger.Warn("Broadcast failed-recipient report could not be sent", "error", sendErr)
			}
		} else {
			c.Logger.Warn("Broadcast failed-recipient report could not be written", "error", err)
		}
	}
}
