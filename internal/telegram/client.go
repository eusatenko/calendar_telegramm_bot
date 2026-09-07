package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func NewClient(token string) *Client {
	return &Client{base: "https://api.telegram.org/bot" + token + "/", http: &http.Client{Timeout: 65 * time.Second}}
}

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}
type Chat struct {
	ID int64 `json:"id"`
}
type Message struct {
	MessageID int    `json:"message_id"`
	Chat      Chat   `json:"chat"`
	From      User   `json:"from"`
	Text      string `json:"text"`
}
type CallbackQuery struct {
	ID      string  `json:"id"`
	From    User    `json:"from"`
	Message Message `json:"message"`
	Data    string  `json:"data"`
}
type Update struct {
	UpdateID int            `json:"update_id"`
	Message  *Message       `json:"message"`
	Callback *CallbackQuery `json:"callback_query"`
}
type Button struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
	URL          string `json:"url,omitempty"`
}
type Markup struct {
	InlineKeyboard [][]Button `json:"inline_keyboard"`
}
type apiResponse[T any] struct {
	OK          bool   `json:"ok"`
	Description string `json:"description"`
	Result      T      `json:"result"`
}

func (c *Client) call(ctx context.Context, method string, payload any, out any) error {
	b, _ := json.Marshal(payload)
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, c.base+method, bytes.NewReader(b))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.http.Do(req)
	if e != nil {
		return e
	}
	defer resp.Body.Close()
	var envelope apiResponse[json.RawMessage]
	if e = json.NewDecoder(resp.Body).Decode(&envelope); e != nil {
		return e
	}
	if !envelope.OK {
		return fmt.Errorf("Telegram API %s: %s", method, envelope.Description)
	}
	if out != nil {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}
func (c *Client) GetUpdates(ctx context.Context, offset int) ([]Update, error) {
	var out []Update
	e := c.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 50, "allowed_updates": []string{"message", "callback_query"}}, &out)
	return out, e
}
func (c *Client) Send(ctx context.Context, chat int64, text string, m Markup) error {
	return c.call(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": text, "reply_markup": m}, nil)
}
func (c *Client) Edit(ctx context.Context, chat int64, msg int, text string, m Markup) error {
	return c.call(ctx, "editMessageText", map[string]any{"chat_id": chat, "message_id": msg, "text": text, "reply_markup": m}, nil)
}
func (c *Client) Answer(ctx context.Context, id, text string) error {
	return c.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}
func (c *Client) Username(ctx context.Context) (string, error) {
	var u User
	e := c.call(ctx, "getMe", map[string]any{}, &u)
	return u.Username, e
}
