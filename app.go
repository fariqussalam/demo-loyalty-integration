package main

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
)

//go:embed templates/*.html static/*
var files embed.FS

type app struct {
	db        *sql.DB
	templates *template.Template
}

func newApp(db *sql.DB) *app {
	return &app{
		db:        db,
		templates: parseTemplates(),
	}
}

func (a *app) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(files))

	mux.HandleFunc("GET /", a.home)
	mux.HandleFunc("POST /integrations", a.createSandbox)
	mux.HandleFunc("POST /integrations/delete-all", a.deleteAllSandboxes)
	mux.HandleFunc("GET /integrations/{slug}", a.result)
	mux.HandleFunc("POST /integrations/{slug}/delete", a.deleteSandbox)

	mux.HandleFunc("GET /storefront/{slug}", a.storefront)
	mux.HandleFunc("POST /storefront/{slug}/buy", a.buy)
	mux.HandleFunc("POST /storefront/{slug}/apply-discount", a.applyDiscount)
	mux.HandleFunc("POST /storefront/{slug}/reset-discount", a.resetDiscount)
	mux.HandleFunc("GET /storefront/{slug}/apps/rewards", a.rewardsHost)

	mux.HandleFunc("GET /blocks/{slug}/Rewards7", a.rewardsBlock)
	mux.HandleFunc("GET /popups/{slug}/{kind}", a.popup)
	mux.HandleFunc("POST /popups/{slug}/signup", a.signup)
	mux.HandleFunc("POST /popups/{slug}/login", a.login)
	mux.HandleFunc("POST /popups/{slug}/redeem", a.redeem)

	mux.HandleFunc("GET /admin/{slug}", a.admin)
	mux.HandleFunc("GET /admin/{slug}/customers/{id}", a.adminCustomer)

	return mux
}

func (a *app) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.templates.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *app) sandboxFromRequest(w http.ResponseWriter, r *http.Request) (sandbox, bool) {
	s, err := a.sandboxBySlug(r.PathValue("slug"))
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return sandbox{}, false
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return sandbox{}, false
	}
	return s, true
}

func (a *app) links(r *http.Request, s sandbox, auth string) linkData {
	siteURL := requestOrigin(r)
	iframe := siteURL + blockURL(s, auth)
	rewards := "/storefront/" + s.Slug + "/apps/rewards"
	snippet := fmt.Sprintf(`<iframe id="rewards-frame" title="%s Rewards" src="%s" sandbox="allow-top-navigation allow-scripts allow-forms allow-modals allow-popups allow-popups-to-escape-sandbox allow-same-origin" allow="clipboard-write" style="border:0;width:100%%;height:1500px"></iframe>`, s.BrandName, iframe)
	return linkData{
		SiteURL:        siteURL,
		StorefrontURL:  "/storefront/" + s.Slug,
		RewardsPageURL: rewards,
		IframeURL:      iframe,
		AdminURL:       "/admin/" + s.Slug,
		IntegrationURL: "/integrations/" + s.Slug,
		InstallSnippet: snippet,
	}
}

func requestOrigin(r *http.Request) string {
	scheme := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	if scheme == "" {
		scheme = "http"
		if r.TLS != nil {
			scheme = "https"
		}
	}
	host := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0])
	if host == "" {
		host = r.Host
	}
	return (&url.URL{Scheme: scheme, Host: host}).String()
}

func blockURL(s sandbox, auth string) string {
	v := url.Values{}
	if auth != "" {
		v.Set("auth", auth)
	}
	raw := "/blocks/" + s.Slug + "/Rewards7"
	if encoded := v.Encode(); encoded != "" {
		return raw + "?" + encoded
	}
	return raw
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

func money(cents int) string {
	return fmt.Sprintf("$%.2f", float64(cents)/100)
}

func dollars(cents int) int {
	return cents / 100
}

func parseTemplates() *template.Template {
	funcs := template.FuncMap{
		"money":      money,
		"jsonPretty": jsonPretty,
	}
	return template.Must(template.New("").Funcs(funcs).ParseFS(files, "templates/*.html"))
}

func jsonPretty(s string) template.HTML {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return template.HTML(template.HTMLEscapeString(s))
	}
	b, _ := json.MarshalIndent(v, "", "  ")
	return template.HTML(template.HTMLEscapeString(string(b)))
}
