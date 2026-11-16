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
    :root { color-scheme: dark; }
    * { box-sizing: border-box; }
    body {
      font: 16px/1.5 Onest, system-ui, -apple-system, "Segoe UI", sans-serif;
      margin: 0; min-height: 100vh; min-height: 100dvh; padding: 28px 20px 24px;
      display: flex; flex-direction: column; gap: 24px;
      background: #15171c; color: #fff;
    }
    .brand { display: flex; align-items: center; gap: 10px; }
    .brand span {
      font: 700 16px Unbounded, Onest, system-ui, sans-serif; letter-spacing: -0.01em;
    }
    main {
      flex: 1; display: flex; flex-direction: column; justify-content: center;
      width: 100%; max-width: 27.5rem; margin: 0 auto;
    }
    h1 {
      font: 700 1.75rem/1.12 Unbounded, Onest, system-ui, sans-serif;
      letter-spacing: -0.03em; margin: 0 0 1.25rem;
    }
    p { margin: 0 0 1rem; color: #c5c8d0; }
    p strong { color: #fff; }
    a { color: #c9f23d; }
    .muted { font-size: .9rem; }
    form { margin: .5rem 0 0; }
    button, .button {
      display: flex; align-items: center; justify-content: center;
      width: 100%; min-height: 52px; padding: 0 1rem;
      border: 1.5px solid #fff; border-radius: 10px; background: #c9f23d; color: #15171c;
      box-shadow: 3px 3px 0 #fff;
      font: inherit; font-weight: 600; text-align: center; text-decoration: none;
      cursor: pointer;
    }
    button:active, .button:active { transform: translate(3px, 3px); box-shadow: none; }
    .secondary {
      background: transparent; color: #fff; border-color: #5a5f6b; box-shadow: none;
      margin-top: .75rem;
    }
    :focus-visible { outline: 3px solid #5546e8; outline-offset: 2px; }
    code {
      font: 500 .9em "JetBrains Mono", ui-monospace, monospace;
      background: #262a33; color: #fff; border-radius: 6px; padding: .1rem .4rem;
    }
`

// pageBrand is the cube mark and the wordmark, the same as in the WebApp.
const pageBrand = `
  <div class="brand">
    <svg aria-hidden="true" width="30" height="30" viewBox="0 0 28 28" stroke="#fff" stroke-width="1.5" stroke-linejoin="round">
      <path d="M14 2 25 8 14 14 3 8Z" fill="#c9f23d"/>
      <path d="M3 8 14 14V26L3 20Z" fill="#15171c"/>
      <path d="M25 8 14 14V26L25 20Z" fill="#5546e8"/>
    </svg>
    <span>cublyverse</span>
  </div>`

// pageTemplate wraps body in the shared document shell. Body is trusted
// template source written in this package, not user input.
func pageTemplate(body string) *template.Template {
	return template.Must(template.New("page").Parse(`<!doctype html>
<html lang="ru">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}} — Cublyverse</title>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Unbounded:wght@700&family=Onest:wght@400;600&family=JetBrains+Mono:wght@500&display=swap">
  <style>` + pageStyle + `</style>
</head>
<body>` + pageBrand + `
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
    <h1>Аккаунт привязан</h1>
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
