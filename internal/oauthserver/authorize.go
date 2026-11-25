package oauthserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/maksimkurb/cublyverse/internal/auth"
	"github.com/maksimkurb/cublyverse/internal/store/db"
	"github.com/maksimkurb/cublyverse/internal/workspace"
)

const csrfCookie = "cv_oauth_csrf"

var challengeRE = regexp.MustCompile(`^[A-Za-z0-9_-]{43,128}$`)

// authRequest is a parsed authorization request.
type authRequest struct {
	Client      Client
	RedirectURI string
	State       string
	Scope       string
	Challenge   string
	Resource    string
	// ResourceWorkspace is the workspace the resource URL names, if any.
	ResourceWorkspace string
}

// requestError is a request problem that cannot be reported by redirecting to
// the client (the client or its redirect URI is not trusted yet).
type requestError struct{ message string }

func (e requestError) Error() string { return e.message }

// redirectError is an error reported to the client through its redirect URI.
type redirectError struct{ code, description string }

func (e redirectError) Error() string { return e.code + ": " + e.description }

// parseAuthRequest validates the authorization parameters. The first error
// kind is shown on a page, the second goes back to the client.
func (s *Service) parseAuthRequest(r *http.Request, v url.Values) (authRequest, error) {
	client, err := s.loadClient(r.Context(), v.Get("client_id"))
	if err != nil {
		if errors.Is(err, errUnknownClient) {
			return authRequest{}, requestError{"Неизвестное приложение (client_id). Зарегистрируйте клиента заново."}
		}
		return authRequest{}, err
	}
	redirectURI := v.Get("redirect_uri")
	if redirectURI == "" && len(client.RedirectURIs) == 1 {
		redirectURI = client.RedirectURIs[0]
	}
	if redirectURI == "" || !redirectMatches(client.RedirectURIs, redirectURI) {
		return authRequest{}, requestError{"Адрес возврата (redirect_uri) не совпадает с зарегистрированным."}
	}
	req := authRequest{Client: client, RedirectURI: redirectURI, State: v.Get("state"), Scope: v.Get("scope"), Challenge: v.Get("code_challenge")}
	if v.Get("response_type") != "code" {
		return req, redirectError{"unsupported_response_type", "Поддерживается только response_type=code."}
	}
	if !challengeRE.MatchString(req.Challenge) || v.Get("code_challenge_method") != "S256" {
		return req, redirectError{"invalid_request", "Нужен PKCE: code_challenge и code_challenge_method=S256."}
	}
	if resource := v.Get("resource"); resource != "" {
		u, err := url.Parse(resource)
		if err != nil || !strings.HasPrefix(resource, s.cfg.Issuer) || u.RawQuery != "" || u.Fragment != "" {
			return req, redirectError{"invalid_target", "Неизвестный ресурс."}
		}
		ws, ok := parseResourcePath(u.Path)
		if !ok || u.Scheme+"://"+u.Host != s.cfg.Issuer {
			return req, redirectError{"invalid_target", "Неизвестный ресурс."}
		}
		req.Resource = s.cfg.Issuer + u.Path
		req.ResourceWorkspace = ws
	}
	return req, nil
}

func (s *Service) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req, err := s.parseAuthRequest(r, q)
	if !s.respondRequestError(w, r, req, err) {
		return
	}

	session, ok := s.identity.ResolveSession(r.Context(), r)
	if !ok || session.IsAPIToken() {
		// Sign in through the regular SSO flow and come back here.
		target := PathAuthorize + "?" + r.URL.RawQuery
		http.Redirect(w, r, "/auth/login?return_to="+url.QueryEscape(target), http.StatusFound)
		return
	}
	s.renderConsent(w, r, req, session.PrincipalID, "")
}

// respondRequestError reports a parse error and returns false, or true when the
// request is fine.
func (s *Service) respondRequestError(w http.ResponseWriter, r *http.Request, req authRequest, err error) bool {
	var page requestError
	var redir redirectError
	switch {
	case err == nil:
		return true
	case errors.As(err, &page):
		s.renderPage(w, http.StatusBadRequest, errorPage, consentView{Title: "Запрос отклонён", Message: page.message})
	case errors.As(err, &redir):
		redirectToClient(w, r, req.RedirectURI, url.Values{"error": {redir.code}, "error_description": {redir.description}, "state": {req.State}}, s.cfg.Issuer)
	default:
		s.cfg.Logger.ErrorContext(r.Context(), "oauth authorize failed", "error", err)
		s.renderPage(w, http.StatusInternalServerError, errorPage, consentView{Title: "Ошибка", Message: "Внутренняя ошибка сервера."})
	}
	return false
}

type consentView struct {
	Title, Message string
	Client         string
	RedirectHost   string
	Scopes         []auth.Scope
	Workspaces     []workspace.Workspace
	FixedWorkspace string
	CSRF           string
	// Hidden are the original request parameters, replayed by the form.
	Hidden     map[string]string
	CIMD       bool
	ClientID   string
	WantsCraft bool
}

func (s *Service) renderConsent(w http.ResponseWriter, r *http.Request, req authRequest, userID, message string) {
	workspaces, err := s.workspaces.List(r.Context(), userID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	csrf, err := randomToken("")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: csrfCookie, Value: csrf, Path: PathAuthorize, HttpOnly: true,
		Secure: strings.HasPrefix(s.cfg.Issuer, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	granted := grantedScopes(req.Scope)
	var scopes []auth.Scope
	wantsCraft := false
	for _, name := range granted {
		for _, sc := range auth.Scopes {
			if sc.Name == name {
				scopes = append(scopes, sc)
			}
		}
		if strings.HasPrefix(name, "craft:") {
			wantsCraft = true
		}
	}
	view := consentView{
		Title: "Доступ приложению", Message: message, Client: req.Client.Name, ClientID: req.Client.ID,
		CIMD: req.Client.CIMD, RedirectHost: redirectHost(req.RedirectURI), Scopes: scopes,
		Workspaces: workspaces, CSRF: csrf, WantsCraft: wantsCraft,
		Hidden: map[string]string{
			"client_id": req.Client.ID, "redirect_uri": req.RedirectURI, "state": req.State,
			"scope": req.Scope, "code_challenge": req.Challenge, "code_challenge_method": "S256",
			"response_type": "code", "resource": req.Resource,
		},
	}
	if req.ResourceWorkspace != "" {
		for _, ws := range workspaces {
			if ws.ID == req.ResourceWorkspace {
				view.FixedWorkspace = ws.Name
				view.Workspaces = []workspace.Workspace{ws}
			}
		}
		if view.FixedWorkspace == "" {
			s.renderPage(w, http.StatusForbidden, errorPage, consentView{Title: "Нет доступа", Message: "Вы не состоите в workspace, к которому запрошен доступ."})
			return
		}
	}
	s.renderPage(w, http.StatusOK, consentPage, view)
}

func (s *Service) handleAuthorizeDecision(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderPage(w, http.StatusBadRequest, errorPage, consentView{Title: "Ошибка", Message: "Некорректная форма."})
		return
	}
	session, ok := s.identity.ResolveSession(r.Context(), r)
	if !ok || session.IsAPIToken() {
		s.renderPage(w, http.StatusUnauthorized, errorPage, consentView{Title: "Сессия истекла", Message: "Войдите заново и повторите подключение."})
		return
	}
	cookie, err := r.Cookie(csrfCookie)
	form := r.PostForm.Get("csrf")
	if err != nil || form == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(form)) != 1 {
		s.renderPage(w, http.StatusForbidden, errorPage, consentView{Title: "Запрос отклонён", Message: "Не пройдена защита формы (CSRF). Откройте подключение заново."})
		return
	}
	req, err := s.parseAuthRequest(r, r.PostForm)
	if !s.respondRequestError(w, r, req, err) {
		return
	}
	if r.PostForm.Get("decision") != "allow" {
		redirectToClient(w, r, req.RedirectURI, url.Values{"error": {"access_denied"}, "error_description": {"Пользователь отказал в доступе."}, "state": {req.State}}, s.cfg.Issuer)
		return
	}

	workspaceID := r.PostForm.Get("workspace_id")
	if req.ResourceWorkspace != "" {
		workspaceID = req.ResourceWorkspace
	}
	var wsID pgtype.UUID
	if workspaceID != "" {
		ws, err := s.workspaces.Require(r.Context(), session.PrincipalID, workspaceID)
		if err != nil {
			s.renderPage(w, http.StatusForbidden, errorPage, consentView{Title: "Нет доступа", Message: "Вы не состоите в выбранном workspace."})
			return
		}
		wsID, _ = workspace.ParseID(ws.ID)
	}
	scopes := grantedScopes(req.Scope)
	if !wsID.Valid {
		// Craft data lives in a workspace; without one there is nothing to grant.
		kept := scopes[:0]
		for _, sc := range scopes {
			if !strings.HasPrefix(sc, "craft:") {
				kept = append(kept, sc)
			}
		}
		scopes = kept
		if len(scopes) == 0 {
			scopes = []string{auth.ScopeProfileRead}
		}
	}

	code, err := randomToken("")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	family, err := newUUID()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	familyID, _ := workspace.ParseID(family)
	userID, _ := workspace.ParseID(session.PrincipalID)
	if err := s.st.Queries().CreateOAuthCode(r.Context(), db.CreateOAuthCodeParams{
		CodeHash: hashSecret(code), FamilyID: familyID, ClientID: req.Client.ID, UserID: userID, WorkspaceID: wsID,
		Scopes: scopes, Resource: req.Resource, RedirectUri: req.RedirectURI, CodeChallenge: req.Challenge,
		ExpiresAt: s.now().Add(codeTTL),
	}); err != nil {
		s.fail(w, r, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: csrfCookie, Path: PathAuthorize, MaxAge: -1})
	redirectToClient(w, r, req.RedirectURI, url.Values{"code": {code}, "state": {req.State}}, s.cfg.Issuer)
}

// redirectToClient sends the user back to the client. iss (RFC 9207) lets the
// client detect a mix-up between authorization servers.
func redirectToClient(w http.ResponseWriter, r *http.Request, redirectURI string, params url.Values, issuer string) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "bad redirect", http.StatusBadRequest)
		return
	}
	q := u.Query()
	for k, vs := range params {
		if len(vs) > 0 && vs[0] != "" {
			q.Set(k, vs[0])
		}
	}
	q.Set("iss", issuer)
	u.RawQuery = q.Encode()
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, u.String(), http.StatusFound)
}

func redirectHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.Host != "" {
		return u.Host
	}
	return u.Scheme + "://"
}

// pkceMatches verifies an S256 code challenge.
func pkceMatches(challenge, verifier string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(computed), []byte(challenge)) == 1
}

// --- pages ---------------------------------------------------------------------

const pageStyle = `
:root{color-scheme:light dark}
body{font:16px/1.5 system-ui,sans-serif;margin:0;min-height:100vh;display:grid;place-items:center;background:#f4f4f6;color:#15171c}
main{background:#fff;max-width:30rem;width:calc(100% - 2rem);padding:1.5rem;border-radius:14px;box-shadow:0 1px 8px #0002}
h1{font-size:1.25rem;margin:.2rem 0 .8rem}
ul{padding-left:1.1rem}
.muted{color:#666;font-size:.9rem}
label{display:block;margin:.8rem 0 .25rem;font-weight:600}
select{width:100%;padding:.5rem;font:inherit}
.row{display:flex;gap:.6rem;margin-top:1.2rem}
button{font:inherit;padding:.6rem 1rem;border-radius:8px;border:1px solid #5546e8;cursor:pointer;background:#5546e8;color:#fff}
button.secondary{background:transparent;color:#5546e8}
code{background:#eee;padding:.05rem .35rem;border-radius:5px}
.warn{background:#fff3cd;padding:.6rem .8rem;border-radius:8px;font-size:.9rem}
@media (prefers-color-scheme:dark){body{background:#15171c;color:#eee}main{background:#1f222a}.muted{color:#aaa}code{background:#2c3039}.warn{background:#4a3f12}button.secondary{color:#a9a0ff}}
`

var errorPage = template.Must(template.New("e").Parse(`<!doctype html><html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} — Cublyverse</title><style>` + pageStyle + `</style></head>
<body><main><h1>{{.Title}}</h1><p>{{.Message}}</p></main></body></html>`))

var consentPage = template.Must(template.New("c").Parse(`<!doctype html><html lang="ru"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} — Cublyverse</title><style>` + pageStyle + `</style></head>
<body><main>
<h1>Приложение «{{.Client}}» просит доступ</h1>
<p class="muted">После подтверждения оно вернёт вас на <code>{{.RedirectHost}}</code>.
{{if .CIMD}}Адрес приложения: <code>{{.ClientID}}</code>.{{end}}</p>
{{if .Message}}<p class="warn">{{.Message}}</p>{{end}}
<p>Что оно сможет делать от вашего имени:</p>
<ul>{{range .Scopes}}<li>{{.Description}}</li>{{end}}</ul>
<form method="post" action="/oauth/authorize">
{{range $k, $v := .Hidden}}<input type="hidden" name="{{$k}}" value="{{$v}}">{{end}}
<input type="hidden" name="csrf" value="{{.CSRF}}">
{{if .WantsCraft}}
<label for="ws">Workspace</label>
{{if .FixedWorkspace}}<p><strong>{{.FixedWorkspace}}</strong></p><input type="hidden" name="workspace_id" value="{{(index .Workspaces 0).ID}}">
{{else}}<select id="ws" name="workspace_id"><option value="">— без доступа к «Бусинкам» —</option>
{{range .Workspaces}}<option value="{{.ID}}">{{.Name}}</option>{{end}}</select>
<p class="muted">Приложение получит доступ только к выбранному workspace.</p>{{end}}
{{end}}
<div class="row"><button type="submit" name="decision" value="allow">Разрешить</button>
<button type="submit" name="decision" value="deny" class="secondary">Отказать</button></div>
</form>
<p class="muted">Доступ можно отозвать в настройках Cublyverse.</p>
</main></body></html>`))

func (s *Service) renderPage(w http.ResponseWriter, status int, tmpl *template.Template, view consentView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
	w.WriteHeader(status)
	if err := tmpl.Execute(w, view); err != nil {
		s.cfg.Logger.Error("render oauth page", "error", err)
	}
}
