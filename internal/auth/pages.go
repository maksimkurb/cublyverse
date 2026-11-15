package auth

import (
	"html/template"
	"log/slog"
	"net/http"
)

// appPath is where the WebApp lives; every page links back to it.
const appPath = "/app/"

// pageView is the data the sign-in pages render.
type pageView struct {
	Title        string
	Message      string
	State        string
	Token        string
	Platform     string
	RemoteUserID string
	SSOName      string
}

// pageStyle is shared by every page so the sign-in surface stays one design.
const pageStyle = `
    :root { color-scheme: light dark; }
    body {
      font: 16px/1.5 system-ui, -apple-system, "Segoe UI", sans-serif;
      margin: 0; padding: 2rem 1rem; background: #f5f5f7; color: #111827;
    }
    main {
      max-width: 30rem; margin: 0 auto; background: #fff; border-radius: 1rem;
      padding: 1.5rem; box-shadow: 0 1px 3px rgb(0 0 0 / 12%);
    }
    h1 { font-size: 1.35rem; margin: 0 0 1rem; }
    p { margin: 0 0 1rem; }
    .muted { color: #6b7280; font-size: .9rem; }
    button, .button {
      display: inline-block; width: 100%; box-sizing: border-box; padding: .85rem 1rem;
      border: 0; border-radius: .75rem; background: #2563eb; color: #fff;
      font: inherit; font-weight: 600; text-align: center; text-decoration: none;
      cursor: pointer;
    }
    .secondary { background: #e5e7eb; color: #111827; margin-top: .5rem; }
    code { background: #f3f4f6; border-radius: .35rem; padding: .1rem .35rem; }
    @media (prefers-color-scheme: dark) {
      body { background: #0b0f19; color: #f9fafb; }
      main { background: #111827; box-shadow: none; }
      .muted { color: #9ca3af; }
      code { background: #1f2937; }
      .secondary { background: #374151; color: #f9fafb; }
    }
`

// pageTemplate wraps body in the shared document shell. Body is trusted
// template source written in this package, not user input.
func pageTemplate(body string) *template.Template {
	return template.Must(template.New("page").Parse(`<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} — Cublyverse</title>
  <style>` + pageStyle + `</style>
</head>
<body>
  <main>
` + body + `
  </main>
</body>
</html>
`))
}

var (
	errorPage = pageTemplate(`
    <h1>{{.Title}}</h1>
    <p>{{.Message}}</p>
    <p class="muted">Вернитесь в чат с ботом и нажмите «Войти» ещё раз.</p>
    <a class="button secondary" href="` + appPath + `">Открыть приложение</a>`)

	telegramVerifyPage = pageTemplate(`
    <h1>Вход в Cublyverse</h1>
    <p>Проверяем, что страница открыта из Telegram от вашего имени.</p>
    <p id="status" role="status">Проверка…</p>
    <p id="fallback" class="muted" hidden>
      Если страница не перенаправилась, <a id="continue" href="#">продолжите вручную</a>.
    </p>
    <script src="https://telegram.org/js/telegram-web-app.js"></script>
    <script>
      (function () {
        var state = "{{.State}}";
        var status = document.getElementById("status");
        var fallback = document.getElementById("fallback");
        var cont = document.getElementById("continue");
        var webApp = window.Telegram && window.Telegram.WebApp;
        if (!webApp || !webApp.initData) {
          status.textContent = "Откройте эту страницу из чата с ботом в Telegram.";
          return;
        }
        if (webApp.ready) { webApp.ready(); }
        if (webApp.expand) { webApp.expand(); }
        fetch("/auth/telegram/verify", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ state: state, init_data: webApp.initData })
        })
          .then(function (response) {
            return response.json().then(function (body) { return { ok: response.ok, body: body }; });
          })
          .then(function (result) {
            if (result.ok && result.body.redirect_url) {
              window.location.assign(result.body.redirect_url);
              return;
            }
            status.textContent = (result.body && result.body.message) || "Не удалось подтвердить вход.";
          })
          .catch(function () {
            status.textContent = "Ошибка сети. Попробуйте ещё раз.";
          });
      })();
    </script>`)

	confirmPage = pageTemplate(`
    <h1>Привязка аккаунта</h1>
    <p>Привязать аккаунт <strong>{{.Platform}}</strong>
       <code>{{.RemoteUserID}}</code> к SSO-аккаунту <strong>{{.SSOName}}</strong>?</p>
    <p class="muted">После подтверждения этот мессенджер будет работать от имени
       этого SSO-аккаунта. Ссылка одноразовая и действует 10 минут.</p>
    <form method="post" action="/auth/confirm">
      <input type="hidden" name="state" value="{{.State}}">
      <input type="hidden" name="token" value="{{.Token}}">
      <button type="submit">Привязать</button>
    </form>
    <a class="button secondary" href="` + appPath + `">Отмена</a>`)

	linkedPage = pageTemplate(`
    <h1>✅ Аккаунт привязан</h1>
    <p>Аккаунт <strong>{{.Platform}}</strong> <code>{{.RemoteUserID}}</code>
       привязан к SSO-аккаунту <strong>{{.SSOName}}</strong>.</p>
    <p class="muted">Мы отправили подтверждение в чат. Дальше можно пользоваться
       приложением.</p>
    <a class="button" href="` + appPath + `">Открыть приложение</a>`)
)

// renderPage writes an HTML page. Sign-in pages must never be cached and must
// not leak the state token through the Referer header when the browser follows
// the redirect to the identity provider.
func (s *Service) renderPage(w http.ResponseWriter, status int, tmpl *template.Template, view pageView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if err := tmpl.Execute(w, view); err != nil {
		s.logger.Error("render auth page failed", slog.String("error", err.Error()))
	}
}

// renderError shows a user facing message with the app link.
func (s *Service) renderError(w http.ResponseWriter, status int, title, message string) {
	s.renderPage(w, status, errorPage, pageView{Title: title, Message: message})
}

// platformLabel is the Russian name of a messenger platform.
func platformLabel(platform string) string {
	switch platform {
	case "telegram":
		return "Telegram"
	case "matrix":
		return "Matrix"
	default:
		return platform
	}
}
