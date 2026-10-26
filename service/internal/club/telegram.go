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
		"allowed_updates": []string{"message", "callback_query"},
	}, nil)
}

// Button is an inline button under a message: one that opens a page (URL)
// or one that answers the bot (Data).
type Button struct {
	Text string `json:"text"`
	URL  string `json:"url,omitempty"`
	Data string `json:"callback_data,omitempty"`
}

// Send writes to a private chat. text is Telegram HTML.
func (t *Telegram) Send(ctx context.Context, chat int64, text string, buttons ...Button) error {
	msg := map[string]any{"chat_id": chat, "text": text, "parse_mode": "HTML", "link_preview_options": map[string]bool{"is_disabled": true}}
	if len(buttons) > 0 {
		msg["reply_markup"] = keyboard(buttons)
	}
	return t.call(ctx, "sendMessage", msg, nil)
}

func keyboard(buttons []Button) map[string]any {
	rows := [][]Button{}
	for _, b := range buttons {
		rows = append(rows, []Button{b})
	}
	return map[string]any{"inline_keyboard": rows}
}

// Edit replaces a message the bot sent, and its buttons with none.
func (t *Telegram) Edit(ctx context.Context, chat int64, message int64, text string) error {
	return t.call(ctx, "editMessageText", map[string]any{"chat_id": chat, "message_id": message, "text": text,
		"parse_mode": "HTML", "reply_markup": map[string]any{"inline_keyboard": [][]Button{}}}, nil)
}

// Answer stops the spinner on a tapped button.
func (t *Telegram) Answer(ctx context.Context, id, text string) error {
	return t.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
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
	Callback *struct {
		ID   string `json:"id"`
		From struct {
			ID           int64  `json:"id"`
			LanguageCode string `json:"language_code"`
		} `json:"from"`
		Data    string `json:"data"`
		Message *struct {
			ID   int64 `json:"message_id"`
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
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
	if cb := u.Callback; cb != nil {
		if err := a.onAnswer(context.WithoutCancel(r.Context()), cb.ID, cb.From.ID, localeOf(cb.From.LanguageCode), cb.Data, cb.Message); err != nil {
			a.Log.Error("club: telegram answer failed", "err", err)
		}
		return
	}
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

// tgStart takes the code Start carried. It does not sign anyone in yet: it
// asks the person who tapped Start whether they asked for this sign-in,
// where and when it was asked for -- a start link sent to them by someone
// else would otherwise sign that someone's browser in as them. Their Yes
// (onAnswer) finishes it.
//
// Without a code, or with one that expired, it still signs the person up
// and sends a link that opens the site; the site asks before it signs in.
func (a *API) tgStart(ctx context.Context, chat int64, who signIn, code string) error {
	l := who.Locale
	var member, from string
	var asked time.Time
	var valid bool
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		var forMember *string
		var expires time.Time
		var finished *string
		err := tx.QueryRow(ctx, `
			SELECT for_member, expires_at, member_id, requested_from, created_at FROM club_logins
			WHERE code_sha256 = $1 AND provider = 'telegram' AND used_at IS NULL FOR UPDATE`, sha(code)).
			Scan(&forMember, &expires, &finished, &from, &asked)
		valid = code != "" && err == nil && finished == nil && expires.After(a.now())
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		link := ""
		if valid {
			link = deref(forMember)
		}
		member, err = identify(ctx, tx, who, link, a.now())
		if err != nil {
			return err
		}
		if valid {
			_, err = tx.Exec(ctx, `UPDATE club_logins SET claimed_by = $2 WHERE code_sha256 = $1`, sha(code), member)
		}
		return err
	})
	if err != nil {
		return err
	}
	if valid {
		return a.Telegram.Send(ctx, chat,
			fmt.Sprintf(t(l, "confirm"), html.EscapeString(who.Handle), html.EscapeString(orElse(from, "?")), asked.UTC().Format("15:04")),
			Button{Text: t(l, "confirm_yes"), Data: "y:" + code},
			Button{Text: t(l, "confirm_no"), Data: "n:" + code})
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

// onAnswer is a tap on Yes or No under the sign-in question. Only the
// Telegram account that tapped Start may answer it.
func (a *API) onAnswer(ctx context.Context, id string, from int64, l, data string, msg *struct {
	ID   int64 `json:"message_id"`
	Chat struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}) error {
	verb, code, ok := strings.Cut(data, ":")
	if !ok || (verb != "y" && verb != "n") {
		return a.Telegram.Answer(ctx, id, "")
	}
	var handle string
	var done bool
	err := pgx.BeginFunc(ctx, a.DB, func(tx pgx.Tx) error {
		var claimed *string
		var expires time.Time
		var member *string
		err := tx.QueryRow(ctx, `
			SELECT claimed_by, expires_at, member_id FROM club_logins
			WHERE code_sha256 = $1 AND provider = 'telegram' AND used_at IS NULL FOR UPDATE`, sha(code)).
			Scan(&claimed, &expires, &member)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// The answer must come from whoever tapped Start.
		if claimed == nil || member != nil || !expires.After(a.now()) {
			return nil
		}
		if err := tx.QueryRow(ctx, `SELECT handle FROM club_identities WHERE provider = 'telegram' AND subject = $1 AND member_id = $2`,
			fmt.Sprint(from), *claimed).Scan(&handle); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			return err
		}
		if verb == "y" {
			_, err = tx.Exec(ctx, `UPDATE club_logins SET member_id = claimed_by WHERE code_sha256 = $1`, sha(code))
		} else {
			_, err = tx.Exec(ctx, `UPDATE club_logins SET expires_at = $2 WHERE code_sha256 = $1`, sha(code), a.now())
		}
		done = err == nil
		return err
	})
	if err != nil {
		return err
	}
	text := t(l, "confirm_gone")
	switch {
	case done && verb == "y":
		text = fmt.Sprintf(t(l, "signed_in"), html.EscapeString(handle))
	case done:
		text = t(l, "confirm_refused")
	}
	if err := a.Telegram.Answer(ctx, id, ""); err != nil {
		return err
	}
	if msg != nil {
		return a.Telegram.Edit(ctx, msg.Chat.ID, msg.ID, text)
	}
	return nil
}

// notify writes to a member through the bot, when they have it and have
// not muted it. A member without Telegram reads the same on their page. The
// error is the bot's failing to deliver; a member it may not write to is not
// one.
func (a *API) notify(ctx context.Context, member string, text func(locale string) string, buttons func(locale string) []Button) error {
	if a.Telegram == nil {
		return nil
	}
	var chat int64
	var locale string
	err := a.DB.QueryRow(ctx, `
		SELECT i.chat_id, i.locale FROM club_identities i JOIN club_members m ON m.id = i.member_id
		WHERE i.member_id = $1 AND i.provider = 'telegram' AND i.chat_id IS NOT NULL AND NOT m.quiet
		ORDER BY i.seen_at DESC LIMIT 1`, member).Scan(&chat, &locale)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err == nil {
		err = a.Telegram.Send(ctx, chat, text(locale), buttons(locale)...)
	}
	if err != nil {
		a.Log.Warn("club: telegram notice failed", "member", member, "err", err)
	}
	return err
}

func (a *API) notifyDecision(ctx context.Context, member, report, decision string, d reports.Decided) {
	// d.Total is the reports' ledger; the message says the member's whole.
	total := d.Total
	if a.Wall != nil {
		if wall, err := a.Wall.StarsOf(ctx, member); err == nil {
			total += wall
		}
	}
	// Why a part earned less than alone (reports.Reason*), in the member's
	// words: a stars-less acceptance says why, not a guess at it.
	why := func(l string) string {
		var out []string
		for _, r := range d.Reasons {
			out = append(out, t(l, "reason_"+r))
		}
		return strings.Join(out, t(l, "reason_sep"))
	}
	_ = a.notify(ctx, member, func(l string) string {
		switch {
		case decision == "publish" && d.Points > 0 && len(d.Reasons) > 0:
			return fmt.Sprintf(t(l, "accepted"), report, d.Points, total) + " " + fmt.Sprintf(t(l, "accepted_partly"), why(l))
		case decision == "publish" && d.Points > 0:
			return fmt.Sprintf(t(l, "accepted"), report, d.Points, total)
		case decision == "publish" && len(d.Reasons) > 0:
			return fmt.Sprintf(t(l, "accepted_none"), report, why(l))
		case decision == "publish":
			return fmt.Sprintf(t(l, "accepted_known"), report)
		default:
			return fmt.Sprintf(t(l, "rejected"), report)
		}
	}, func(l string) []Button {
		return []Button{{Text: t(l, "open_club"), URL: a.Cfg.SiteURL + "/club/"}}
	})
}

// Asked is what asking a report's sender did.
type Asked struct {
	// Member: who sent it, "" for a report sent signed out (its receipt
	// shows the question).
	Member string
	// Told: how the question was sent to them -- "telegram" or "email",
	// each only once it was handed over -- or "" when nothing was sent:
	// their page on /club shows it either way.
	Told string
	// Quiet: they muted notices, which is why nothing was sent.
	Quiet bool
}

// Ask puts a reviewer's question to a report's sender (reports.Store.Ask)
// and tells a member sender, linking to the report on /club, where editing
// it is the answer. The review page and `openipc reports ask` both come here.
func (a *API) Ask(ctx context.Context, id, by, question string, revision *int) (Asked, error) {
	member, err := a.Reports.Store().Ask(ctx, id, by, question, revision)
	if err != nil {
		return Asked{}, err
	}
	res := Asked{Member: member}
	if member != "" {
		res.Told, res.Quiet = a.notifyAsk(ctx, member, id, strings.TrimSpace(question))
	}
	a.Log.Info("club: sender asked", "report", id, "by", by, "member", member, "told", res.Told, "quiet", res.Quiet)
	return res, nil
}

// notifyAsk writes the question through the bot when the member has a
// chat with it, and otherwise -- or when the bot fails -- by mail to the
// address they sign in with: a question left unseen keeps the report
// waiting for good. It answers the channel that took the message, "" when
// none did, and whether the member muted notices: then nothing is sent,
// as with decisions.
func (a *API) notifyAsk(ctx context.Context, member, report, question string) (told string, quiet bool) {
	if err := a.DB.QueryRow(ctx, `SELECT quiet FROM club_members WHERE id = $1`, member).Scan(&quiet); err != nil {
		a.Log.Warn("club: the asked member unreadable", "member", member, "err", err)
		return "", false
	}
	if quiet {
		return "", true
	}
	if a.Telegram != nil {
		var chat int64
		var l string
		err := a.DB.QueryRow(ctx, `
			SELECT chat_id, locale FROM club_identities
			WHERE member_id = $1 AND provider = 'telegram' AND chat_id IS NOT NULL
			ORDER BY seen_at DESC LIMIT 1`, member).Scan(&chat, &l)
		switch {
		case err == nil:
			err = a.Telegram.Send(ctx, chat, fmt.Sprintf(t(l, "asked"), report, html.EscapeString(question)),
				Button{Text: t(l, "open_report"), URL: a.reportLink(l, report)})
			if err == nil {
				return "telegram", false
			}
			a.Log.Warn("club: telegram question failed", "member", member, "err", err)
		case !errors.Is(err, pgx.ErrNoRows):
			a.Log.Warn("club: the asked member's chat unreadable", "member", member, "err", err)
		}
	}
	if a.Mail == nil {
		return "", false
	}
	var to, l string
	err := a.DB.QueryRow(ctx, `
		SELECT subject, locale FROM club_identities WHERE member_id = $1 AND provider = 'email'
		ORDER BY seen_at DESC LIMIT 1`, member).Scan(&to, &l)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			a.Log.Warn("club: the asked member's address unreadable", "member", member, "err", err)
		}
		return "", false
	}
	mail, ok := askMails[l]
	if !ok {
		mail = askMails["en"]
	}
	if err := a.Mail.Send(to, fmt.Sprintf(mail[0], report), fmt.Sprintf(mail[1], report, question, a.reportLink(l, report))); err != nil {
		a.Log.Warn("club: question mail failed", "member", member, "err", err)
		return "", false
	}
	return "email", false
}

// reportLink is a member's report on their page, in their language.
func (a *API) reportLink(locale, report string) string {
	prefix := ""
	if locale == "ru" || locale == "zh" {
		prefix = "/" + locale
	}
	return a.Cfg.SiteURL + prefix + "/club/#" + report
}

var askMails = map[string][2]string{
	"en": {"A question about your report %s", "A maintainer reviewing your report %s on openipc.org asks:\n\n%s\n\nTo answer, edit the report on your page -- add the photo, correct the marking, or answer in its note:\n\n%s\n\nYour edit puts it back in front of the reviewer.\n\n-- OpenIPC\n"},
	"ru": {"Вопрос по вашему отчёту %s", "Сопровождающий, проверяющий ваш отчёт %s на openipc.org, спрашивает:\n\n%s\n\nЧтобы ответить, измените отчёт на своей странице -- добавьте фото, исправьте маркировку или ответьте в примечании:\n\n%s\n\nПосле правки отчёт снова попадёт к проверяющему.\n\n-- OpenIPC\n"},
	"zh": {"关于你的报告 %s 的问题", "审核你在 openipc.org 提交的报告 %s 的维护者问：\n\n%s\n\n回答方式：在你的个人页面编辑该报告——补充照片、更正标记，或在备注中回答：\n\n%s\n\n编辑后报告会重新回到审核者面前。\n\n-- OpenIPC\n"},
}

var messages = map[string]map[string]string{
	"en": {
		"signed_in":          "✅ Signed in to openipc.org as <b>%s</b>. You can go back to the browser.",
		"welcome":            "Hi! I sign you in to openipc.org and tell you what happens to the boards, logs and dumps you send. I never see your chats or your phone number.",
		"expired":            "That sign-in code has expired. This button opens openipc.org signed in:",
		"open_signed_in":     "Open openipc.org signed in",
		"open_club":          "My submissions",
		"help":               "I sign you in to openipc.org and tell you what happens to what you send.\n/quiet mutes me, /loud unmutes, /stop unlinks Telegram.",
		"quiet":              "Muted. Your submissions are still listed on openipc.org/club. Send /loud to hear from me again, or /stop to unlink Telegram.",
		"loud":               "I'll tell you again when something you sent is reviewed.",
		"stopped":            "Telegram is unlinked from your openipc.org account. Sign in with Telegram again to link it back.",
		"not_member":         "You haven't signed in to openipc.org with Telegram yet.",
		"accepted":           "✅ Your report <b>%s</b> was accepted. <b>+%d ★</b>, %d in total.",
		"accepted_known":     "✅ Your report <b>%s</b> was accepted. It brings no new stars this time. Thank you anyway.",
		"accepted_none":      "✅ Your report <b>%s</b> was accepted, but earns no stars this time: %s. Thank you anyway.",
		"accepted_partly":    "Some of it earned nothing: %s.",
		"reason_known":       "the same file was already published",
		"reason_camera":      "you were already paid for this camera",
		"reason_sep":         "; ",
		"rejected":           "⚪ Your report <b>%s</b> was not accepted. Your page on openipc.org says why when the reviewer left a note.",
		"asked":              "❓ A question about your report <b>%s</b>:\n\n%s\n\nEdit the report on openipc.org to answer: add a photo, correct it, or reply in its note. It goes back to the reviewer then.",
		"open_report":        "Open the report",
		"confirm":            "Sign in to openipc.org as <b>%s</b>?\nAsked for from %s at %s UTC. Tap Yes only if you asked for it yourself, just now.",
		"confirm_yes":        "✅ Yes, sign me in",
		"confirm_no":         "✖ No, it was not me",
		"confirm_refused":    "Nobody was signed in. If someone sent you that link, they wanted your account: ignore them.",
		"confirm_gone":       "That sign-in has expired or was already answered. Start again on openipc.org.",
		"camera_linked":      "📷 %s is linked to your openipc.org account. It starts earning stars after %d days with pictures on the Open Wall.",
		"camera_joined":      "★ +%d. %s has met the bar of the openipc.org home page: a month on the Open Wall with real pictures. %d stars in total.",
		"camera_joined_rare": "★ +%d. %s has met the bar of the openipc.org home page, and no other camera on the wall has its chip or its sensor. %d stars in total.",
		"camera_month":       "★ +%d. %s kept sending pictures to the Open Wall for another 30 days. %d stars in total.",
		"camera_milestone":   "🎉 %s: %d days on the Open Wall. Thank you for keeping it running.",
		"camera_silent":      "⏸ %s has sent no picture to the Open Wall for %d days. Check its power, its network and its OpenWall settings.",
		"open_cameras":       "My cameras",
		"a_camera":           "Your camera",
		"merged":             "🔗 Your openipc.org accounts are one now: what you sent, your cameras and your stars are together, %d ★ in total. Sign in with any of: %s.",
		"provider_telegram":  "Telegram",
		"provider_github":    "GitHub",
		"provider_email":     "email",
	},
	"ru": {
		"signed_in":          "✅ Вы вошли на openipc.org как <b>%s</b>. Можно вернуться в браузер.",
		"welcome":            "Привет! Я вхожу за вас на openipc.org и сообщаю, что стало с платами, логами и дампами, которые вы прислали. Ваших чатов и номера телефона я не вижу.",
		"expired":            "Этот код входа устарел. Кнопка ниже откроет openipc.org уже с входом:",
		"open_signed_in":     "Открыть openipc.org с входом",
		"open_club":          "Мои материалы",
		"help":               "Я вхожу за вас на openipc.org и сообщаю, что стало с присланным.\n/quiet — не писать, /loud — писать снова, /stop — отвязать Telegram.",
		"quiet":              "Больше не пишу. Ваши материалы по-прежнему видны на openipc.org/club. /loud — писать снова, /stop — отвязать Telegram.",
		"loud":               "Снова сообщу, когда присланное проверят.",
		"stopped":            "Telegram отвязан от вашей учётной записи на openipc.org. Войдите через Telegram снова, чтобы привязать.",
		"not_member":         "Вы ещё не входили на openipc.org через Telegram.",
		"accepted":           "✅ Ваш отчёт <b>%s</b> принят. <b>+%d ★</b>, всего %d.",
		"accepted_known":     "✅ Ваш отчёт <b>%s</b> принят. Новых звёзд он в этот раз не приносит. Всё равно спасибо.",
		"accepted_none":      "✅ Ваш отчёт <b>%s</b> принят, но звёзд в этот раз нет: %s. Всё равно спасибо.",
		"accepted_partly":    "Часть отчёта звёзд не принесла: %s.",
		"reason_known":       "такой же файл уже опубликован",
		"reason_camera":      "за эту камеру звёзды уже начислены",
		"reason_sep":         "; ",
		"rejected":           "⚪ Ваш отчёт <b>%s</b> не принят. Если проверяющий оставил пояснение, оно на вашей странице на openipc.org.",
		"asked":              "❓ Вопрос по вашему отчёту <b>%s</b>:\n\n%s\n\nЧтобы ответить, измените отчёт на openipc.org: добавьте фото, исправьте его или ответьте в примечании. После этого он вернётся к проверяющему.",
		"open_report":        "Открыть отчёт",
		"confirm":            "Войти на openipc.org как <b>%s</b>?\nЗапрос с адреса %s в %s UTC. Нажмите «Да», только если вы сами запросили вход только что.",
		"confirm_yes":        "✅ Да, войти",
		"confirm_no":         "✖ Нет, это не я",
		"confirm_refused":    "Вход не выполнен. Если ссылку вам прислал кто-то другой, он хотел получить доступ к вашей учётной записи: не отвечайте ему.",
		"confirm_gone":       "Этот вход устарел или на него уже ответили. Начните заново на openipc.org.",
		"camera_linked":      "📷 %s привязана к вашей учётной записи на openipc.org. Звёзды она начнёт приносить после %d дней со снимками на Open Wall.",
		"camera_joined":      "★ +%d. %s дотянула до главной страницы openipc.org: месяц на Open Wall с настоящими снимками. Всего звёзд: %d.",
		"camera_joined_rare": "★ +%d. %s дотянула до главной страницы openipc.org, и ни у одной другой камеры на стене нет её чипа или сенсора. Всего звёзд: %d.",
		"camera_month":       "★ +%d. %s присылала снимки на Open Wall ещё 30 дней. Всего звёзд: %d.",
		"camera_milestone":   "🎉 %s: %d дней на Open Wall. Спасибо, что она работает.",
		"camera_silent":      "⏸ %s не присылала снимков на Open Wall уже %d дн. Проверьте питание, сеть и настройки OpenWall.",
		"open_cameras":       "Мои камеры",
		"a_camera":           "Ваша камера",
		"merged":             "🔗 Ваши учётные записи на openipc.org объединены: присланное, камеры и звёзды теперь в одной, всего %d ★. Входить можно любым способом: %s.",
		"provider_telegram":  "Telegram",
		"provider_github":    "GitHub",
		"provider_email":     "почта",
	},
	"zh": {
		"signed_in":          "✅ 已以 <b>%s</b> 身份登录 openipc.org。现在可以回到浏览器。",
		"welcome":            "你好！我帮你登录 openipc.org，并告诉你提交的电路板、日志和固件转储的审核结果。我看不到你的聊天记录或手机号。",
		"expired":            "该登录码已过期。点击下方按钮即可直接登录 openipc.org：",
		"open_signed_in":     "登录并打开 openipc.org",
		"open_club":          "我的提交",
		"help":               "我帮你登录 openipc.org，并告诉你提交内容的审核结果。\n/quiet 静音，/loud 取消静音，/stop 解除 Telegram 绑定。",
		"quiet":              "已静音。你的提交仍列在 openipc.org/club。发送 /loud 恢复通知，或 /stop 解除 Telegram 绑定。",
		"loud":               "你的提交被审核后我会再通知你。",
		"stopped":            "已从你的 openipc.org 账户解除 Telegram 绑定。再次用 Telegram 登录即可重新绑定。",
		"not_member":         "你还没有用 Telegram 登录过 openipc.org。",
		"accepted":           "✅ 你的报告 <b>%s</b> 已通过。<b>+%d ★</b>，共 %d。",
		"accepted_known":     "✅ 你的报告 <b>%s</b> 已通过。本次没有新的星星。仍然感谢！",
		"accepted_none":      "✅ 你的报告 <b>%s</b> 已通过，但本次没有星星：%s。仍然感谢！",
		"accepted_partly":    "部分内容未获得星星：%s。",
		"reason_known":       "相同的文件已经发布过",
		"reason_camera":      "这台摄像头已经为你计过星星",
		"reason_sep":         "；",
		"rejected":           "⚪ 你的报告 <b>%s</b> 未通过。审核者如留有说明，可在 openipc.org 的个人页面查看。",
		"asked":              "❓ 关于你的报告 <b>%s</b> 的问题：\n\n%s\n\n请在 openipc.org 编辑该报告来回答：补充照片、更正内容，或在备注中回复。之后报告会回到审核者那里。",
		"open_report":        "打开报告",
		"confirm":            "以 <b>%s</b> 身份登录 openipc.org？\n请求来自 %s，时间 %s UTC。仅当这是你刚刚亲自发起的登录时才点“是”。",
		"confirm_yes":        "✅ 是，登录",
		"confirm_no":         "✖ 不是我",
		"confirm_refused":    "未登录任何账户。如果是别人发给你的链接，对方想获取你的账户：请不要理会。",
		"confirm_gone":       "该登录已过期或已处理。请在 openipc.org 重新开始。",
		"camera_linked":      "📷 %s 已绑定到你的 openipc.org 账户。在 Open Wall 上有 %d 天的画面后开始获得星星。",
		"camera_joined":      "★ +%d。%s 已达到 openipc.org 首页的标准：在 Open Wall 上一个月，画面真实。共 %d 颗星。",
		"camera_joined_rare": "★ +%d。%s 已达到 openipc.org 首页的标准，而且墙上没有其他摄像头使用相同的芯片或传感器。共 %d 颗星。",
		"camera_month":       "★ +%d。%s 又向 Open Wall 持续发送了 30 天画面。共 %d 颗星。",
		"camera_milestone":   "🎉 %s：在 Open Wall 上已 %d 天。感谢你让它一直运行。",
		"camera_silent":      "⏸ %s 已有 %d 天没有向 Open Wall 发送画面。请检查电源、网络和 OpenWall 设置。",
		"open_cameras":       "我的摄像头",
		"a_camera":           "你的摄像头",
		"merged":             "🔗 你在 openipc.org 的账户已合并：提交内容、摄像头和星星都在同一个账户里，共 %d ★。可用以下任一方式登录：%s。",
		"provider_telegram":  "Telegram",
		"provider_github":    "GitHub",
		"provider_email":     "邮箱",
	},
}

func t(locale, key string) string {
	if s, ok := messages[locale][key]; ok {
		return s
	}
	return messages["en"][key]
}
