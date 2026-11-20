package bot

import (
	"ashokshau/tgmusic/internal/config"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	td "github.com/AshokShau/gotdbot"
)

const telegramBotAPI = "https://api.telegram.org"

type botAPIResponse[T any] struct {
	OK          bool   `json:"ok"`
	Result      T      `json:"result"`
	Description string `json:"description"`
	ErrorCode   int    `json:"error_code"`
}

type botAPIUser struct {
	Username string `json:"username"`
}

type joinRequestUser struct {
	FirstName string `json:"first_name"`
}

type joinRequestChat struct {
	Title string `json:"title"`
}

type botAPIJoinRequest struct {
	Chat       joinRequestChat `json:"chat"`
	From       joinRequestUser `json:"from"`
	UserChatID int64           `json:"user_chat_id"`
}

type botAPIUpdate struct {
	UpdateID       int64                `json:"update_id"`
	ChatJoinRequest *botAPIJoinRequest `json:"chat_join_request"`
}

type inlineKeyboardButton struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type inlineKeyboardMarkup struct {
	InlineKeyboard [][]inlineKeyboardButton `json:"inline_keyboard"`
}

type sendMessagePayload struct {
	ChatID      int64                `json:"chat_id"`
	Text        string               `json:"text"`
	ReplyMarkup inlineKeyboardMarkup `json:"reply_markup"`
}

var joinRequestHTTPClient = &http.Client{Timeout: 40 * time.Second}

// StartJoinRequestDM starts the Bot API listener that sends a first-contact DM
// to users who submit join requests. It does not approve or decline requests.
func StartJoinRequestDM(client *td.Client) {
	if config.Token == "" {
		return
	}
	go runJoinRequestDM(client)
}

func runJoinRequestDM(client *td.Client) {
	username, err := getBotAPIUsername()
	if err != nil {
		client.Logger.Warnf("Join-request DM listener could not initialize: %v", err)
		return
	}
	if username == "" {
		client.Logger.Warnf("Join-request DM listener stopped: bot username is empty")
		return
	}

	client.Logger.Infof("Join-request DM listener started for @%s", username)
	var offset int64

	for {
		updates, err := getJoinRequestUpdates(offset)
		if err != nil {
			var apiErr *telegramAPIError
			if errors.As(err, &apiErr) && apiErr.Code == http.StatusConflict {
				client.Logger.Warnf("Join-request DM listener stopped: Bot API getUpdates conflict (another poller may be active or a webhook is configured): %v", err)
				return
			}
			client.Logger.Warnf("Join-request DM polling failed: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if update.ChatJoinRequest == nil {
				continue
			}
			if err := sendJoinRequestDM(update.ChatJoinRequest, username); err != nil {
				client.Logger.Warnf("Failed to DM join requester %d: %v", update.ChatJoinRequest.UserChatID, err)
			}
		}
	}
}

type telegramAPIError struct {
	Code int
	Text string
}

func (e *telegramAPIError) Error() string {
	return fmt.Sprintf("Telegram Bot API error %d: %s", e.Code, e.Text)
}

func callTelegramBotAPI(method string, payload any, result any, timeout time.Duration) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	url := fmt.Sprintf("%s/bot%s/%s", telegramBotAPI, config.Token, method)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := joinRequestHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var envelope botAPIResponse[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		code := envelope.ErrorCode
		if code == 0 {
			code = resp.StatusCode
		}
		return &telegramAPIError{Code: code, Text: envelope.Description}
	}
	if result != nil && len(envelope.Result) > 0 {
		return json.Unmarshal(envelope.Result, result)
	}
	return nil
}

func getBotAPIUsername() (string, error) {
	var user botAPIUser
	if err := callTelegramBotAPI("getMe", map[string]any{}, &user, 15*time.Second); err != nil {
		return "", err
	}
	return strings.TrimSpace(user.Username), nil
}

func getJoinRequestUpdates(offset int64) ([]botAPIUpdate, error) {
	payload := map[string]any{
		"timeout":        30,
		"allowed_updates": []string{"chat_join_request"},
	}
	if offset > 0 {
		payload["offset"] = offset
	}
	var updates []botAPIUpdate
	if err := callTelegramBotAPI("getUpdates", payload, &updates, 35*time.Second); err != nil {
		return nil, err
	}
	return updates, nil
}

func sendJoinRequestDM(request *botAPIJoinRequest, botUsername string) error {
	if request == nil || request.UserChatID == 0 {
		return fmt.Errorf("join request has no usable user_chat_id")
	}

	name := strings.TrimSpace(request.From.FirstName)
	if name == "" {
		name = "there"
	}
	groupName := strings.TrimSpace(request.Chat.Title)
	if groupName == "" {
		groupName = "the group"
	}

	startURL := fmt.Sprintf("https://t.me/%s?start=join_request", botUsername)
	payload := sendMessagePayload{
		ChatID: request.UserChatID,
		Text: fmt.Sprintf(
			"👋 Hi %s!\n\nYour request to join %s has been received.\n\nPlease start the bot using the button below to receive updates and get support.",
			name,
			groupName,
		),
		ReplyMarkup: inlineKeyboardMarkup{
			InlineKeyboard: [][]inlineKeyboardButton{
				{{Text: "🚀 START BOT", URL: startURL}},
				{
					{Text: "📢 UPDATES", URL: config.SupportChannel},
					{Text: "💬 SUPPORT", URL: config.SupportGroup},
				},
			},
		},
	}
	return callTelegramBotAPI("sendMessage", payload, nil, 15*time.Second)
}
