package main

import (
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testApp(t *testing.T) *app {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := migrate(db); err != nil {
		t.Fatal(err)
	}
	return newApp(db)
}

func TestRouterSurfacesSmoke(t *testing.T) {
	a := testApp(t)
	router := a.routes()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}

	body := strings.NewReader("brand_name=Acme+Cosmetics&palette=black&discount_template=ACME-{VALUE}-OFF")
	req = httptest.NewRequest(http.MethodPost, "/integrations", body)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST /integrations status = %d, want %d: %s", w.Code, http.StatusSeeOther, w.Body.String())
	}
	if got := w.Result().Header.Get("Location"); got != "/integrations/acme" {
		t.Fatalf("redirect location = %q, want /integrations/acme", got)
	}

	paths := []string{
		"/integrations/acme",
		"/storefront/acme",
		"/storefront/acme/apps/rewards",
		"/blocks/acme/Rewards7",
		"/popups/acme/login",
		"/admin/acme",
		"/admin/acme/customers/1",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d: %s", path, w.Code, http.StatusOK, w.Body.String())
		}
	}
}

func TestSlugifyPrefersShortAcmeSlug(t *testing.T) {
	got := slugify("Acme Cosmetics")
	if got != "acme" {
		t.Fatalf("slugify() = %q, want acme", got)
	}
}

func TestUniqueSlugDoesNotOverwrite(t *testing.T) {
	a := testApp(t)
	_, err := a.db.Exec(`insert into sandboxes (slug, brand_name, palette, discount_template, created_at) values ('acme', 'Acme Cosmetics', 'black', 'ACME-{VALUE}-OFF', ?)`, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.uniqueSlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	if got != "acme-2" {
		t.Fatalf("uniqueSlug() = %q, want acme-2", got)
	}
}

func TestDeleteSandboxRemovesMerchantDataAndRedirectsHome(t *testing.T) {
	a := testApp(t)
	s, err := a.createSandboxFromSetup(sandboxSetup{
		brandName:        "Acme Cosmetics",
		palette:          "black",
		discountTemplate: "ACME-{VALUE}-OFF",
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.customerByEmail(s.ID, demoCustomerEmail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`insert into discounts (sandbox_id, customer_id, code, value_cents, status, created_at) values (?, ?, 'ACME-10-OFF-OLIVIA', ?, 'created', ?)`, s.ID, c.ID, currentReward().ValueCents, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`insert into orders (sandbox_id, product_name, subtotal_cents, discount_code, discount_cents, total_cents, created_at) values (?, 'Hydrating Serum', 2400, '', 0, 2400, ?)`, s.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/integrations/acme/delete", nil)
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusSeeOther, w.Body.String())
	}
	if got := w.Result().Header.Get("Location"); got != "/" {
		t.Fatalf("redirect location = %q, want /", got)
	}
	if _, err := a.sandboxBySlug("acme"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("sandboxBySlug() error = %v, want sql.ErrNoRows", err)
	}

	for _, table := range []string{"customers", "discounts", "orders", "events"} {
		var count int
		if err := a.db.QueryRow(`select count(*) from `+table+` where sandbox_id = ?`, s.ID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}
}

func TestDeleteAllSandboxesRemovesAllMerchantDataAndRedirectsHome(t *testing.T) {
	a := testApp(t)
	var first sandbox
	for _, brand := range []string{"Acme Cosmetics", "Glow Labs"} {
		s, err := a.createSandboxFromSetup(sandboxSetup{
			brandName:        brand,
			palette:          "black",
			discountTemplate: "{BRAND}-{VALUE}-{CUSTOMER}",
		})
		if err != nil {
			t.Fatal(err)
		}
		if first.ID == 0 {
			first = s
		}
	}
	c, err := a.customerByEmail(first.ID, demoCustomerEmail)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`insert into discounts (sandbox_id, customer_id, code, value_cents, status, created_at) values (?, ?, 'ACME-10-OFF-OLIVIA', ?, 'created', ?)`, first.ID, c.ID, currentReward().ValueCents, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`insert into orders (sandbox_id, product_name, subtotal_cents, discount_code, discount_cents, total_cents, created_at) values (?, 'Hydrating Serum', 2400, '', 0, 2400, ?)`, first.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/integrations/delete-all", nil)
	w := httptest.NewRecorder()
	a.routes().ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusSeeOther, w.Body.String())
	}
	if got := w.Result().Header.Get("Location"); got != "/" {
		t.Fatalf("redirect location = %q, want /", got)
	}

	for _, table := range []string{"sandboxes", "customers", "discounts", "orders", "events"} {
		var count int
		if err := a.db.QueryRow(`select count(*) from ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s count = %d, want 0", table, count)
		}
	}
}

func TestCustomerTokenResolvesWithinSandbox(t *testing.T) {
	a := testApp(t)
	sandboxID := insertSandbox(t, a, "acme")
	c := customer{SandboxID: sandboxID, Name: "Olivia Chen", Email: "olivia@example.test", Points: 1500, Tier: "Glow Insider", Token: token("acme", "olivia@example.test"), CreatedAt: time.Now().UTC()}
	id, err := a.insertCustomer(c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := a.customerByToken(sandboxID, c.Token)
	if err != nil || got.ID != id {
		t.Fatalf("customerByToken() = (%+v, %v), want id %d", got, err, id)
	}
}

func TestDiscountCodeUsesCustomerHandle(t *testing.T) {
	s := sandbox{Slug: "acme", DiscountTemplate: "ACME-{VALUE}-OFF"}
	c := customer{ID: 42, Name: "Olivia Chen"}
	got := discountCode(s, c)
	if got != "ACME-10-OFF-OLIVIA" {
		t.Fatalf("discountCode() = %q, want ACME-10-OFF-OLIVIA", got)
	}
}

func TestSignupEmailLinksExistingCustomer(t *testing.T) {
	a := testApp(t)
	sandboxID := insertSandbox(t, a, "acme")
	c := customer{SandboxID: sandboxID, Name: "Maya Stone", Email: "maya@example.test", Points: 1000, Tier: "Glow Insider", Token: token("acme", "maya@example.test"), CreatedAt: time.Now().UTC()}
	if _, err := a.insertCustomer(c); err != nil {
		t.Fatal(err)
	}
	got, err := a.customerByEmail(sandboxID, "MAYA@example.test")
	if err != nil {
		t.Fatal("expected existing customer to be found by normalized email")
	}
	if got.Points != 1000 {
		t.Fatalf("points = %d, want 1000", got.Points)
	}
}

func TestLoginResetsAppliedDiscount(t *testing.T) {
	a := testApp(t)
	sandboxID := insertSandbox(t, a, "acme")
	c := customer{SandboxID: sandboxID, Name: "Olivia Chen", Email: "olivia@example.test", Points: 500, Tier: "Glow Insider", Token: token("acme", "olivia@example.test"), CreatedAt: time.Now().UTC()}
	id, err := a.insertCustomer(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`insert into discounts (sandbox_id, customer_id, code, value_cents, status, created_at) values (?, ?, 'ACME-10-OFF-OLIVIA', ?, 'created', ?)`, sandboxID, id, currentReward().ValueCents, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := a.applyDiscountCode(sandboxID, "ACME-10-OFF-OLIVIA"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/popups/acme/login", strings.NewReader("customer_id=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("slug", "acme")
	w := httptest.NewRecorder()
	a.login(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusOK, w.Body.String())
	}
	s, err := a.sandboxBySlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	if s.AppliedDiscount != "" {
		t.Fatalf("applied discount = %q, want reset", s.AppliedDiscount)
	}
	pending, err := a.pendingDiscountCode(sandboxID)
	if err != nil {
		t.Fatal(err)
	}
	if pending != "ACME-10-OFF-OLIVIA" {
		t.Fatalf("pending discount = %q, want ACME-10-OFF-OLIVIA", pending)
	}
}

func TestRedeemRewardDeductsPointsAndCreatesDiscount(t *testing.T) {
	a := testApp(t)
	sandboxID := insertSandbox(t, a, "acme")
	s, err := a.sandboxBySlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	c := customer{SandboxID: sandboxID, Name: "Olivia Chen", Email: "olivia@example.test", Points: 1500, Tier: "Glow Insider", Token: token("acme", "olivia@example.test"), CreatedAt: time.Now().UTC()}
	id, err := a.insertCustomer(c)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = id

	result, err := a.redeemReward(s, c)
	if err != nil {
		t.Fatal(err)
	}
	if result.DiscountCode != "ACME-10-OFF-OLIVIA" {
		t.Fatalf("discount code = %q, want ACME-10-OFF-OLIVIA", result.DiscountCode)
	}
	got, err := a.customerByID(sandboxID, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Points != 500 {
		t.Fatalf("points = %d, want 500", got.Points)
	}
	discounts, err := a.discounts(sandboxID)
	if err != nil {
		t.Fatal(err)
	}
	if len(discounts) != 1 || discounts[0].Status != "created" || discounts[0].ValueCents != currentReward().ValueCents {
		t.Fatalf("discounts = %+v, want one created discount with reward value", discounts)
	}
}

func TestApplyDiscountCodeUpdatesCartStateAndDiscountStatus(t *testing.T) {
	a := testApp(t)
	sandboxID := insertSandbox(t, a, "acme")
	c := customer{SandboxID: sandboxID, Name: "Olivia Chen", Email: "olivia@example.test", Points: 500, Tier: "Glow Insider", Token: token("acme", "olivia@example.test"), CreatedAt: time.Now().UTC()}
	id, err := a.insertCustomer(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`insert into discounts (sandbox_id, customer_id, code, value_cents, status, created_at) values (?, ?, 'ACME-10-OFF-OLIVIA', ?, 'created', ?)`, sandboxID, id, currentReward().ValueCents, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	if err := a.applyDiscountCode(sandboxID, "ACME-10-OFF-OLIVIA"); err != nil {
		t.Fatal(err)
	}
	s, err := a.sandboxBySlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	if s.AppliedDiscount != "ACME-10-OFF-OLIVIA" {
		t.Fatalf("applied discount = %q, want ACME-10-OFF-OLIVIA", s.AppliedDiscount)
	}
	discounts, err := a.discounts(sandboxID)
	if err != nil {
		t.Fatal(err)
	}
	if len(discounts) != 1 || discounts[0].Status != "applied" {
		t.Fatalf("discounts = %+v, want one applied discount", discounts)
	}
}

func TestCartUsesAppliedDiscountValue(t *testing.T) {
	a := testApp(t)
	sandboxID := insertSandbox(t, a, "acme")
	c := customer{SandboxID: sandboxID, Name: "Olivia Chen", Email: "olivia@example.test", Points: 500, Tier: "Glow Insider", Token: token("acme", "olivia@example.test"), CreatedAt: time.Now().UTC()}
	id, err := a.insertCustomer(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`insert into discounts (sandbox_id, customer_id, code, value_cents, status, created_at) values (?, ?, 'CUSTOM-25', 2500, 'applied', ?)`, sandboxID, id, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := a.applyDiscountCode(sandboxID, "CUSTOM-25"); err != nil {
		t.Fatal(err)
	}
	s, err := a.sandboxBySlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	cart, err := a.cart(s)
	if err != nil {
		t.Fatal(err)
	}
	if cart.DiscountCents != 2500 {
		t.Fatalf("discount cents = %d, want 2500", cart.DiscountCents)
	}
	if cart.TotalCents != cart.SubtotalCents-2500 {
		t.Fatalf("total cents = %d, want subtotal minus discount", cart.TotalCents)
	}
}

func TestCartPrefillsPendingDiscountWithoutReducingTotal(t *testing.T) {
	a := testApp(t)
	sandboxID := insertSandbox(t, a, "acme")
	c := customer{SandboxID: sandboxID, Name: "Olivia Chen", Email: "olivia@example.test", Points: 500, Tier: "Glow Insider", Token: token("acme", "olivia@example.test"), CreatedAt: time.Now().UTC()}
	id, err := a.insertCustomer(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`insert into discounts (sandbox_id, customer_id, code, value_cents, status, created_at) values (?, ?, 'ACME-10-OFF-OLIVIA', ?, 'created', ?)`, sandboxID, id, currentReward().ValueCents, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	s, err := a.sandboxBySlug("acme")
	if err != nil {
		t.Fatal(err)
	}

	cart, err := a.cart(s)
	if err != nil {
		t.Fatal(err)
	}
	if cart.PendingDiscountCode != "ACME-10-OFF-OLIVIA" {
		t.Fatalf("pending discount = %q, want ACME-10-OFF-OLIVIA", cart.PendingDiscountCode)
	}
	if cart.HasDiscount || cart.DiscountCents != 0 || cart.TotalCents != cart.SubtotalCents {
		t.Fatalf("cart = %+v, want pending code without price reduction", cart)
	}
}

func TestResetAppliedDiscountKeepsCodePending(t *testing.T) {
	a := testApp(t)
	sandboxID := insertSandbox(t, a, "acme")
	c := customer{SandboxID: sandboxID, Name: "Olivia Chen", Email: "olivia@example.test", Points: 500, Tier: "Glow Insider", Token: token("acme", "olivia@example.test"), CreatedAt: time.Now().UTC()}
	id, err := a.insertCustomer(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`insert into discounts (sandbox_id, customer_id, code, value_cents, status, created_at) values (?, ?, 'ACME-10-OFF-OLIVIA', ?, 'created', ?)`, sandboxID, id, currentReward().ValueCents, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := a.applyDiscountCode(sandboxID, "ACME-10-OFF-OLIVIA"); err != nil {
		t.Fatal(err)
	}
	if err := a.resetAppliedDiscount(sandboxID); err != nil {
		t.Fatal(err)
	}
	s, err := a.sandboxBySlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	cart, err := a.cart(s)
	if err != nil {
		t.Fatal(err)
	}
	if cart.HasDiscount || cart.PendingDiscountCode != "ACME-10-OFF-OLIVIA" {
		t.Fatalf("cart = %+v, want reset applied code as pending", cart)
	}
}

func TestCartUsesRequestedQuantity(t *testing.T) {
	a := testApp(t)
	insertSandbox(t, a, "acme")
	s, err := a.sandboxBySlug("acme")
	if err != nil {
		t.Fatal(err)
	}

	cart, err := a.cartForQuantity(s, 3)
	if err != nil {
		t.Fatal(err)
	}
	if cart.Product.Quantity != 3 {
		t.Fatalf("quantity = %d, want 3", cart.Product.Quantity)
	}
	if cart.SubtotalCents != currentProduct().PriceCents*3 {
		t.Fatalf("subtotal cents = %d, want unit price times 3", cart.SubtotalCents)
	}
}

func TestBuyAddsCustomerActivityFromAuth(t *testing.T) {
	a := testApp(t)
	sandboxID := insertSandbox(t, a, "acme")
	c := customer{SandboxID: sandboxID, Name: "Olivia Chen", Email: "olivia@example.test", Points: 500, Tier: "Glow Insider", Token: token("acme", "olivia@example.test"), CreatedAt: time.Now().UTC()}
	id, err := a.insertCustomer(c)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/storefront/acme/buy", strings.NewReader("quantity=2&auth="+c.Token))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("slug", "acme")
	w := httptest.NewRecorder()
	a.buy(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusSeeOther, w.Body.String())
	}

	events, err := a.customerEvents(sandboxID, id, eventAreaActivity)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "demo_order_saved" {
		t.Fatalf("events = %+v, want customer order activity", events)
	}
}

func TestBuyAddsCustomerActivityFromAppliedDiscount(t *testing.T) {
	a := testApp(t)
	sandboxID := insertSandbox(t, a, "acme")
	c := customer{SandboxID: sandboxID, Name: "Olivia Chen", Email: "olivia@example.test", Points: 500, Tier: "Glow Insider", Token: token("acme", "olivia@example.test"), CreatedAt: time.Now().UTC()}
	id, err := a.insertCustomer(c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`insert into discounts (sandbox_id, customer_id, code, value_cents, status, created_at) values (?, ?, 'ACME-10-OFF-OLIVIA', ?, 'applied', ?)`, sandboxID, id, currentReward().ValueCents, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := a.applyDiscountCode(sandboxID, "ACME-10-OFF-OLIVIA"); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/storefront/acme/buy", strings.NewReader("quantity=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("slug", "acme")
	w := httptest.NewRecorder()
	a.buy(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusSeeOther, w.Body.String())
	}

	events, err := a.customerEvents(sandboxID, id, eventAreaActivity)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "demo_order_saved" {
		t.Fatalf("events = %+v, want customer order activity", events)
	}
}

func TestPointsHistoryUsesOnlyRealPointMovement(t *testing.T) {
	now := time.Now().UTC()
	events := []event{
		{Title: "Olivia signed in", Payload: `{"customer_id":1}`, CreatedAt: now},
		{Title: "Olivia redeemed $10 off", Payload: `{"code":"ACME-10-OFF-OLIVIA","points":-1000}`, CreatedAt: now.Add(-time.Minute)},
		{Title: "Olivia starts with 1,500 points", Payload: `{"points":1500}`, CreatedAt: now.Add(-2 * time.Minute)},
	}

	got := pointsHistory(events)
	if len(got) != 2 {
		t.Fatalf("pointsHistory() length = %d, want 2", len(got))
	}
	if got[0].Points != -1000 || got[0].Code != "ACME-10-OFF-OLIVIA" {
		t.Fatalf("redeem entry = %+v, want -1000 with discount code", got[0])
	}
	if got[1].Points != 1500 {
		t.Fatalf("starting entry points = %d, want 1500", got[1].Points)
	}
}

func insertSandbox(t *testing.T, a *app, slug string) int64 {
	t.Helper()
	res, err := a.db.Exec(`insert into sandboxes (slug, brand_name, palette, discount_template, created_at) values (?, 'Acme Cosmetics', 'black', 'ACME-{VALUE}-OFF', ?)`, slug, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
