package club

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/OpenIPC/website/service/internal/reports"
)

// Telegram is the OpenIPC bot: it signs people in when they tap Start on a
// link the site made, and tells members what happened to what they sent.
// Telegram calls it (a webhook); it calls Telegram's Bot API.
type Telegram struct {
	Token string
	// API is Telegram's Bot API, https://api.telegram.org (a test's server).
	API  string
	HTTP *http.Client
	Log  *slog.Logger

	mu       sync.Mutex
	username string
}

// Username is the bot's @name without the @, once Start has read it.
func (t *Telegram) Username() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.username
}

// Secret is what Telegram sends back in X-Telegram-Bot-Api-Secret-Token:
// derived from the token, so it is one setting fewer and changes with it.
func (t *Telegram) Secret() string {
	m := hmac.New(sha256.New, []byte(t.Token))
	m.Write([]byte("openipc club webhook"))
	return hex.EncodeToString(m.Sum(nil))[:48]
}

func (t *Telegram) call(ctx context.Context, method string, in, out any) error {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(t.API, "/")+"/bot"+t.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.HTTP.Do(req)
	if err != nil {
		// the token is in the URL; never let it into a log line
		return fmt.Errorf("telegram %s: %s", method, strings.ReplaceAll(err.Error(), t.Token, "<token>"))
	}
	defer resp.Body.Close()
	var env struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&env); err != nil {
		return fmt.Errorf("telegram %s: %d", method, resp.StatusCode)
	}
	if !env.OK {
		return fmt.Errorf("telegram %s: %s", method, env.Description)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// Start learns the bot's username and points its webhook at this site.
func (t *Telegram) Start(ctx context.Context, siteURL string) error {
	var me struct {
		Username string `json:"username"`
	}
	if err := t.call(ctx, "getMe", map[string]any{}, &me); err != nil {
		return err
	}
	t.mu.Lock()
	t.username = me.Username
	t.mu.Unlock()
	return t.call(ctx, "setWebhook", map[string]any{
		"url":             strings.TrimSuffix(siteURL, "/") + "/api/v1/club/telegram/webhook",
		"secret_token":    t.Secret(),
		"allowed_updates": []string{"message"},
	}, nil)
}

// Button is an inline button under a message that opens a page.
type Button struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

// Send writes to a private chat. text is Telegram HTML.
func (t *Telegram) Send(ctx context.Context, chat int64, text string, buttons ...Button) error {
	msg := map[string]any{"chat_id": chat, "text": text, "parse_mode": "HTML", "link_preview_options": map[string]bool{"is_disabled": true}}
	if len(buttons) > 0 {
		rows := [][]Button{}
		for _, b := range buttons {
			rows = append(rows, []Button{b})
		}
		msg["reply_markup"] = map[string]any{"inline_keyboard": rows}
	}
	return t.call(ctx, "sendMessage", msg, nil)
}

// telegramStart is POST /api/v1/club/telegram: a sign-in for this browser,
// as a link that opens the bot with the code as Start's parameter.
func (a *API) telegramStart(w http.ResponseWriter, r *http.Request) {
	if a.Telegram == nil || a.Telegram.Username() == "" {
		a.refuse(w, http.StatusServiceUnavailable, "Telegram sign-in is not available on this site")
		return
	}
	if !a.limits.allow("tg:"+clientKey(r), 20, time.Hour, a.now()) {
		a.refuse(w, http.StatusTooManyRequests, "too many sign-ins from this address; try again in an hour")
		return
	}
	code, expires, err := a.newLogin(w, r, "telegram", "")
	if err != nil {
		a.fail(w, "the sign-in", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"link": "https://t.me/" + a.Telegram.Username() + "?start=" + code, "expires_at": expires,
	})
}

type tgUpdate struct {
	Message *struct {
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		From *struct {
			ID           int64  `json:"id"`
			IsBot        bool   `json:"is_bot"`
			Username     string `json:"username"`
			FirstName    string `json:"first_name"`
			LastName     string `json:"last_name"`
			LanguageCode string `json:"language_code"`
		} `json:"from"`
		Text string `json:"text"`
	} `json:"message"`
}

// telegramWebhook is POST /api/v1/club/telegram/webhook: Telegram, with
// what someone wrote to the bot. Anything else is refused by the secret.
func (a *API) telegramWebhook(w http.ResponseWriter, r *http.Request) {
	if a.Telegram == nil {
		http.NotFound(w, r)
		return
	}
	got := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if subtle.ConstantTimeCompare([]byte(got), []byte(a.Telegram.Secret())) != 1 {
		a.refuse(w, http.StatusForbidden, "not Telegram")
		return
	}
	var u tgUpdate
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&u); err != nil {
		a.refuse(w, http.StatusBadRequest, "not an update")
		return
	}
	// Telegram retries anything but a 200; what went wrong is ours to log.
	w.WriteHeader(http.StatusOK)
	m := u.Message
	if m == nil || m.From == nil || m.From.IsBot || m.Chat.Type != "private" {
		return
	}
	ctx := context.WithoutCancel(r.Context())
	if err := a.onMessage(ctx, m.Chat.ID, signIn{
		Provider: "telegram", Subject: fmt.Sprint(m.From.ID),
		Handle: handleOf(m.From.Username, m.From.FirstName), Name: strings.TrimSpace(m.From.FirstName + " " + m.From.LastName),
		ChatID: &m.Chat.ID, Locale: localeOf(m.From.LanguageCode),
	}, strings.TrimSpace(m.Text)); err != nil {
		a.Log.Error("club: telegram message failed", "err", err)
	}
}

func handleOf(username, first string) string {
	if username != "" {
		return "@" + username
	}
	return first
}

func localeOf(code string) string {
	switch {
	case strings.HasPrefix(code, "ru"), strings.HasPrefix(code, "uk"), strings.HasPrefix(code, "be"), strings.HasPrefix(code, "kk"):
		return "ru"
	case strings.HasPrefix(code, "zh"):
		return "zh"
	}
	return "en"
}

func (a *API) onMessage(ctx context.Context, chat int64, who signIn, text string) error {
	l := who.Locale
	cmd, arg, _ := strings.Cut(text, " ")
	cmd = strings.ToLower(strings.SplitN(cmd, "@", 2)[0])
	switch cmd {
	case "/start":
		return a.tgStart(ctx, chat, who, strings.TrimSpace(arg))
	case "/quiet", "/loud":
		quiet := cmd == "/quiet"
		tag, err := a.DB.Exec(ctx, `UPDATE club_members SET quiet = $2 WHERE id =
			(SELECT member_id FROM club_identities WHERE provider = 'telegram' AND subject = $1)`, who.Subject, quiet)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return a.Telegram.Send(ctx, chat, t(l, "not_member"))
		}
		if quiet {
			return a.Telegram.Send(ctx, chat, t(l, "quiet"))
		}
		return a.Telegram.Send(ctx, chat, t(l, "loud"))
	case "/stop":
		if _, err := a.DB.Exec(ctx, `UPDATE club_identities SET chat_id = NULL WHERE provider = 'telegram' AND subject = $1`, who.Subject); err != nil {
			return err
		}
		return a.Telegram.Send(ctx, chat, t(l, "stopped"))
	}
	return a.Telegram.Send(ctx, chat, t(l, "help"), Button{Text: t(l, "open_club"), URL: a.Cfg.SiteURL + "/club/"})
}

// tgStart finishes the sign-in whose code Start carried. Without a code, or
// with one that expired, it still signs the person up and sends a link
// that opens the site signed in -- the browser tab may be long closed.
func (a *API) tgStart(ctx context.Context, chat int64, who signIn, code string) error {
	l := who.Locale
	var member string
	var done bool
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		var forMember *string
		var expires time.Time
		var finished *string
		err := tx.QueryRow(ctx, `
			SELECT for_member, expires_at, member_id FROM club_logins
			WHERE code_sha256 = $1 AND provider = 'telegram' AND used_at IS NULL FOR UPDATE`, sha(code)).
			Scan(&forMember, &expires, &finished)
		valid := code != "" && err == nil && finished == nil && expires.After(a.now())
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		member, err = identify(ctx, tx, who, func() string {
			if valid {
				return deref(forMember)
			}
			return ""
		}())
		if err != nil {
			return err
		}
		if valid {
			_, err = tx.Exec(ctx, `UPDATE club_logins SET member_id = $2 WHERE code_sha256 = $1`, sha(code), member)
			done = true
		}
		return err
	})
	if err != nil {
		return err
	}
	if done {
		return a.Telegram.Send(ctx, chat, fmt.Sprintf(t(l, "signed_in"), html.EscapeString(who.Handle)))
	}
	link := randomString(18)
	if _, err := a.DB.Exec(ctx, `
		INSERT INTO club_logins (code_sha256, provider, member_id, expires_at) VALUES ($1, 'telegram', $2, $3)`,
		sha(link), member, a.now().Add(loginTTL)); err != nil {
		return err
	}
	key := "expired"
	if code == "" {
		key = "welcome"
	}
	return a.Telegram.Send(ctx, chat, t(l, key),
		Button{Text: t(l, "open_signed_in"), URL: a.Cfg.SiteURL + "/api/v1/club/finish?code=" + link})
}

// notify writes to a member through the bot, when they have it and have
// not muted it. A member without Telegram reads the same on their page.
func (a *API) notify(ctx context.Context, member string, text func(locale string) string, buttons func(locale string) []Button) {
	if a.Telegram == nil {
		return
	}
	var chat int64
	var locale string
	err := a.DB.QueryRow(ctx, `
		SELECT i.chat_id, i.locale FROM club_identities i JOIN club_members m ON m.id = i.member_id
		WHERE i.member_id = $1 AND i.provider = 'telegram' AND i.chat_id IS NOT NULL AND NOT m.quiet
		ORDER BY i.seen_at DESC LIMIT 1`, member).Scan(&chat, &locale)
	if errors.Is(err, pgx.ErrNoRows) {
		return
	}
	if err == nil {
		err = a.Telegram.Send(ctx, chat, text(locale), buttons(locale)...)
	}
	if err != nil {
		a.Log.Warn("club: telegram notice failed", "member", member, "err", err)
	}
}

func (a *API) notifyDecision(ctx context.Context, member, report, decision string, d reports.Decided) {
	a.notify(ctx, member, func(l string) string {
		switch {
		case decision == "publish" && d.Points > 0:
			return fmt.Sprintf(t(l, "accepted"), report, d.Points, d.Total)
		case decision == "publish":
			return fmt.Sprintf(t(l, "accepted_known"), report)
		default:
			return fmt.Sprintf(t(l, "rejected"), report)
		}
	}, func(l string) []Button {
		return []Button{{Text: t(l, "open_club"), URL: a.Cfg.SiteURL + "/club/"}}
	})
}

var messages = map[string]map[string]string{
	"en": {
		"signed_in":      "✅ Signed in to openipc.org as <b>%s</b>. You can go back to the browser.",
		"welcome":        "Hi! I sign you in to openipc.org and tell you what happens to the boards, logs and dumps you send. I never see your chats or your phone number.",
		"expired":        "That sign-in code has expired. This button opens openipc.org signed in:",
		"open_signed_in": "Open openipc.org signed in",
		"open_club":      "My submissions",
		"help":           "I sign you in to openipc.org and tell you what happens to what you send.\n/quiet mutes me, /loud unmutes, /stop unlinks Telegram.",
		"quiet":          "Muted. Your submissions are still listed on openipc.org/club. Send /loud to hear from me again, or /stop to unlink Telegram.",
		"loud":           "I'll tell you again when something you sent is reviewed.",
		"stopped":        "Telegram is unlinked from your openipc.org account. Sign in with Telegram again to link it back.",
		"not_member":     "You haven't signed in to openipc.org with Telegram yet.",
		"accepted":       "✅ Your report <b>%s</b> was accepted. <b>+%d ★</b>, %d in total.",
		"accepted_known": "✅ Your report <b>%s</b> was accepted. The catalogue already had what it brings, so it earns no stars this time. Thank you anyway.",
		"rejected":       "⚪ Your report <b>%s</b> was not accepted. Your page on openipc.org says why when the reviewer left a note.",
	},
	"ru": {
		"signed_in":      "✅ Вы вошли на openipc.org как <b>%s</b>. Можно вернуться в браузер.",
		"welcome":        "Привет! Я вхожу за вас на openipc.org и сообщаю, что стало с платами, логами и дампами, которые вы прислали. Ваших чатов и номера телефона я не вижу.",
		"expired":        "Этот код входа устарел. Кнопка ниже откроет openipc.org уже с входом:",
		"open_signed_in": "Открыть openipc.org с входом",
		"open_club":      "Мои материалы",
		"help":           "Я вхожу за вас на openipc.org и сообщаю, что стало с присланным.\n/quiet — не писать, /loud — писать снова, /stop — отвязать Telegram.",
		"quiet":          "Больше не пишу. Ваши материалы по-прежнему видны на openipc.org/club. /loud — писать снова, /stop — отвязать Telegram.",
		"loud":           "Снова сообщу, когда присланное проверят.",
		"stopped":        "Telegram отвязан от вашей учётной записи на openipc.org. Войдите через Telegram снова, чтобы привязать.",
		"not_member":     "Вы ещё не входили на openipc.org через Telegram.",
		"accepted":       "✅ Ваш отчёт <b>%s</b> принят. <b>+%d ★</b>, всего %d.",
		"accepted_known": "✅ Ваш отчёт <b>%s</b> принят. В каталоге это уже было, поэтому звёзд в этот раз нет. Всё равно спасибо.",
		"rejected":       "⚪ Ваш отчёт <b>%s</b> не принят. Если проверяющий оставил пояснение, оно на вашей странице на openipc.org.",
	},
	"zh": {
		"signed_in":      "✅ 已以 <b>%s</b> 身份登录 openipc.org。现在可以回到浏览器。",
		"welcome":        "你好！我帮你登录 openipc.org，并告诉你提交的电路板、日志和固件转储的审核结果。我看不到你的聊天记录或手机号。",
		"expired":        "该登录码已过期。点击下方按钮即可直接登录 openipc.org：",
		"open_signed_in": "登录并打开 openipc.org",
		"open_club":      "我的提交",
		"help":           "我帮你登录 openipc.org，并告诉你提交内容的审核结果。\n/quiet 静音，/loud 取消静音，/stop 解除 Telegram 绑定。",
		"quiet":          "已静音。你的提交仍列在 openipc.org/club。发送 /loud 恢复通知，或 /stop 解除 Telegram 绑定。",
		"loud":           "你的提交被审核后我会再通知你。",
		"stopped":        "已从你的 openipc.org 账户解除 Telegram 绑定。再次用 Telegram 登录即可重新绑定。",
		"not_member":     "你还没有用 Telegram 登录过 openipc.org。",
		"accepted":       "✅ 你的报告 <b>%s</b> 已通过。<b>+%d ★</b>，共 %d。",
		"accepted_known": "✅ 你的报告 <b>%s</b> 已通过。目录中已有相同内容，因此本次没有星星。仍然感谢！",
		"rejected":       "⚪ 你的报告 <b>%s</b> 未通过。审核者如留有说明，可在 openipc.org 的个人页面查看。",
	},
}

func t(locale, key string) string {
	if s, ok := messages[locale][key]; ok {
		return s
	}
	return messages["en"][key]
}
