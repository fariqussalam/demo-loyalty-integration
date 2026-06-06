package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type sandboxSetup struct {
	brandName        string
	palette          string
	discountTemplate string
}

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	sandboxes, err := a.recentSandboxes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "home.html", homeData{Sandboxes: sandboxes, DefaultBrand: defaultBrand})
}

func (a *app) createSandbox(w http.ResponseWriter, r *http.Request) {
	s, err := a.createSandboxFromSetup(sandboxSetupFromRequest(r))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/integrations/"+s.Slug, http.StatusSeeOther)
}

func sandboxSetupFromRequest(r *http.Request) sandboxSetup {
	brand := strings.TrimSpace(r.FormValue("brand_name"))
	if brand == "" {
		brand = defaultBrand
	}
	palette := r.FormValue("palette")
	if palette == "" {
		palette = defaultPalette
	}
	discountTemplate := strings.TrimSpace(r.FormValue("discount_template"))
	if discountTemplate == "" {
		discountTemplate = defaultDiscountTemplate
	}
	return sandboxSetup{
		brandName:        brand,
		palette:          palette,
		discountTemplate: discountTemplate,
	}
}

func (a *app) createSandboxFromSetup(setup sandboxSetup) (sandbox, error) {
	slug, err := a.uniqueSlug(slugify(setup.brandName))
	if err != nil {
		return sandbox{}, err
	}

	now := time.Now().UTC()
	res, err := a.db.Exec(`insert into sandboxes (slug, brand_name, palette, discount_template, created_at) values (?, ?, ?, ?, ?)`, slug, setup.brandName, setup.palette, setup.discountTemplate, now)
	if err != nil {
		return sandbox{}, err
	}
	sandboxID, err := res.LastInsertId()
	if err != nil {
		return sandbox{}, err
	}

	if err := a.seedDefaultCustomer(sandboxID, slug, now); err != nil {
		return sandbox{}, err
	}
	if err := a.event(sandboxID, sql.NullInt64{}, "sandbox_generated", eventAreaActivity, "Integration sandbox generated", map[string]any{"slug": slug, "brand": setup.brandName}); err != nil {
		return sandbox{}, err
	}

	return sandbox{
		ID:               sandboxID,
		Slug:             slug,
		BrandName:        setup.brandName,
		Palette:          setup.palette,
		DiscountTemplate: setup.discountTemplate,
		CreatedAt:        now,
	}, nil
}

func (a *app) seedDefaultCustomer(sandboxID int64, slug string, now time.Time) error {
	olivia := customer{
		SandboxID: sandboxID,
		Name:      demoCustomerName,
		Email:     demoCustomerEmail,
		Points:    demoCustomerPoints,
		Tier:      demoCustomerTier,
		Token:     token(slug, demoCustomerEmail),
		CreatedAt: now,
	}
	customerID, err := a.insertCustomer(olivia)
	if err != nil {
		return err
	}
	return a.event(sandboxID, sql.NullInt64{Int64: customerID, Valid: true}, "starting_points", eventAreaActivity, "Olivia starts with 1,500 points", map[string]any{"points": demoCustomerPoints})
}

func (a *app) result(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	data := a.links(r, s, "")
	a.render(w, "result.html", resultData{Sandbox: s, linkData: data})
}

func (a *app) deleteSandbox(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	if err := a.deleteSandboxData(s.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) deleteAllSandboxes(w http.ResponseWriter, r *http.Request) {
	tx, err := a.db.Begin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	for _, stmt := range []string{
		`delete from events`,
		`delete from orders`,
		`delete from discounts`,
		`delete from customers`,
		`delete from sandboxes`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) deleteSandboxData(sandboxID int64) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, stmt := range []string{
		`delete from events where sandbox_id = ?`,
		`delete from orders where sandbox_id = ?`,
		`delete from discounts where sandbox_id = ?`,
		`delete from customers where sandbox_id = ?`,
		`delete from sandboxes where id = ?`,
	} {
		if _, err := tx.Exec(stmt, sandboxID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (a *app) storefront(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	orders, err := a.orders(s.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	cart, err := a.cart(s)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "storefront.html", storefrontData{Sandbox: s, Cart: cart, Orders: orders, linkData: a.links(r, s, "")})
}

func (a *app) buy(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	quantity := parseQuantity(r.FormValue("quantity"))
	cart, err := a.cartForQuantity(s, quantity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	orderID, err := a.saveDemoOrder(s, cart, quantity, r.FormValue("auth"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, purchaseResponse{OrderID: orderID, ProductName: cart.Product.Name, DiscountCode: cart.DiscountCode, FormattedTotal: money(cart.TotalCents)})
		return
	}
	http.Redirect(w, r, "/storefront/"+s.Slug, http.StatusSeeOther)
}

func (a *app) saveDemoOrder(s sandbox, cart cartState, quantity int, auth string) (int64, error) {
	res, err := a.db.Exec(`insert into orders (sandbox_id, product_name, subtotal_cents, discount_code, discount_cents, total_cents, created_at) values (?, ?, ?, ?, ?, ?, ?)`,
		s.ID, cart.Product.Name, cart.SubtotalCents, cart.DiscountCode, cart.DiscountCents, cart.TotalCents, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	orderID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	customerID, customerName, err := a.customerForPurchase(s, cart.DiscountCode, auth)
	if err != nil {
		return 0, err
	}

	title := "Demo order saved"
	if customerName != "" {
		title = fmt.Sprintf("%s placed order #%d", customerName, orderID)
	}
	payload := map[string]any{
		"order_id":       orderID,
		"quantity":       quantity,
		"product":        cart.Product.Name,
		"discount_code":  cart.DiscountCode,
		"subtotal_cents": cart.SubtotalCents,
		"discount_cents": cart.DiscountCents,
		"total_cents":    cart.TotalCents,
	}
	return orderID, a.event(s.ID, customerID, "demo_order_saved", eventAreaActivity, title, payload)
}

func (a *app) customerForPurchase(s sandbox, discountCode, auth string) (sql.NullInt64, string, error) {
	if auth != "" {
		c, err := a.customerByToken(s.ID, auth)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return sql.NullInt64{}, "", err
		}
		if err == nil {
			return sql.NullInt64{Int64: c.ID, Valid: true}, c.Name, nil
		}
	}
	if discountCode == "" {
		return sql.NullInt64{}, "", nil
	}
	c, err := a.customerByDiscountCode(s.ID, discountCode)
	if errors.Is(err, sql.ErrNoRows) {
		return sql.NullInt64{}, "", nil
	}
	if err != nil {
		return sql.NullInt64{}, "", err
	}
	return sql.NullInt64{Int64: c.ID, Valid: true}, c.Name, nil
}

func (a *app) applyDiscount(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}
	if err := a.applyDiscountCode(s.ID, code); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "unknown discount code", http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if wantsJSON(r) {
		s.AppliedDiscount = code
		cart, err := a.cartForQuantity(s, parseQuantity(r.FormValue("quantity")))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, cart.response())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) resetDiscount(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	if err := a.resetAppliedDiscount(s.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pending, err := a.pendingDiscountCode(s.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if wantsJSON(r) {
		writeJSON(w, resetCartResponse{PendingCode: pending})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *app) applyDiscountCode(sandboxID int64, code string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var customerID int64
	if err := tx.QueryRow(`select customer_id from discounts where sandbox_id = ? and code = ?`, sandboxID, code).Scan(&customerID); err != nil {
		return err
	}
	if _, err := tx.Exec(`update sandboxes set applied_discount = ? where id = ?`, code, sandboxID); err != nil {
		return err
	}
	if _, err := tx.Exec(`update discounts set status = 'applied' where sandbox_id = ? and code = ?`, sandboxID, code); err != nil {
		return err
	}
	if err := insertEvent(tx, sandboxID, sql.NullInt64{Int64: customerID, Valid: true}, "apply_discount_code", eventAreaTrace, callbackApplyDiscountCode+" applied "+code, map[string]any{"blockClientCall": callbackApplyDiscountCode, "code": code}); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *app) resetAppliedDiscount(sandboxID int64) error {
	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`update discounts set status = 'created' where sandbox_id = ? and code = (select applied_discount from sandboxes where id = ?)`, sandboxID, sandboxID); err != nil {
		return err
	}
	if _, err := tx.Exec(`update sandboxes set applied_discount = '' where id = ?`, sandboxID); err != nil {
		return err
	}
	return tx.Commit()
}

func (a *app) rewardsHost(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	auth := r.URL.Query().Get("auth")
	links := a.links(r, s, auth)
	a.render(w, "rewards_host.html", rewardsHostData{Sandbox: s, IframeURL: links.IframeURL, linkData: links})
}

func (a *app) rewardsBlock(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	data, err := a.rewardsBlockView(s, r.URL.Query().Get("auth"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "rewards_block.html", data)
}

func (a *app) rewardsBlockView(s sandbox, auth string) (rewardsBlockData, error) {
	c, err := a.customerByToken(s.ID, auth)
	valid := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return rewardsBlockData{}, err
	}
	if auth != "" && !valid {
		if err := a.event(s.ID, sql.NullInt64{}, "auth_rejected", eventAreaTrace, "Auth token rejected", map[string]any{"auth_present": true}); err != nil {
			return rewardsBlockData{}, err
		}
	}
	if err := a.event(s.ID, nullCustomer(c), "rewards_loaded", eventAreaTrace, "Rewards7 loaded", map[string]any{"signed_in": valid}); err != nil {
		return rewardsBlockData{}, err
	}

	data := rewardsBlockData{Sandbox: s, Reward: currentReward()}
	if !valid {
		return data, nil
	}

	events, err := a.customerEvents(s.ID, c.ID, eventAreaActivity)
	if err != nil {
		return rewardsBlockData{}, err
	}
	redeemedCode, err := a.redeemedDiscountCode(s.ID, c.ID)
	if err != nil {
		return rewardsBlockData{}, err
	}
	data.Customer = &c
	data.PointsHistory = pointsHistory(events)
	data.RedeemedCode = redeemedCode
	return data, nil
}

func (a *app) popup(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	kind := r.PathValue("kind")
	data, found, err := a.popupView(s, kind, r.URL.Query().Get("return_to"), r.URL.Query().Get("auth"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	if err := a.event(s.ID, nullCustomerPtr(data.Customer), "open_popup", eventAreaTrace, callbackOpenPopup+" opened "+kind+" popup", map[string]any{"blockClientCall": callbackOpenPopup, "name": kind}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "popup.html", data)
}

func (a *app) popupView(s sandbox, kind, returnTo, auth string) (popupData, bool, error) {
	data := popupData{
		Sandbox:       s,
		PopupKind:     kind,
		ReturnTo:      returnTo,
		SelectedEmail: demoCustomerEmail,
		Reward:        currentReward(),
	}

	switch kind {
	case "signup":
		data.RandomName, data.RandomEmail = randomIdentity()
	case "login":
		customers, err := a.customers(s.ID)
		if err != nil {
			return popupData{}, false, err
		}
		data.Customers = customers
	case "redeem":
		c, err := a.customerByToken(s.ID, auth)
		if errors.Is(err, sql.ErrNoRows) {
			data.Error = "Please log in before redeeming."
		} else if err != nil {
			return popupData{}, false, err
		} else {
			data.Customer = &c
		}
	default:
		return popupData{}, false, nil
	}
	return data, true, nil
}

func (a *app) signup(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	email := normalizeEmail(r.FormValue("email"))
	if name == "" || email == "" {
		http.Error(w, "name and email required", http.StatusBadRequest)
		return
	}
	c, err := a.signupCustomer(s, name, email)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.resetAppliedDiscount(s.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.closePopup(w, s, popupResult{Auth: c.Token, CustomerID: c.ID, ReturnTo: r.FormValue("return_to")})
}

func (a *app) signupCustomer(s sandbox, name, email string) (customer, error) {
	c, err := a.customerByEmail(s.ID, email)
	if errors.Is(err, sql.ErrNoRows) {
		c = customer{SandboxID: s.ID, Name: name, Email: email, Points: signupBonusPoints, Tier: demoCustomerTier, Token: token(s.Slug, email), CreatedAt: time.Now().UTC()}
		id, err := a.insertCustomer(c)
		if err != nil {
			return customer{}, err
		}
		c.ID = id
		err = a.event(s.ID, sql.NullInt64{Int64: c.ID, Valid: true}, "signup_bonus", eventAreaActivity, c.Name+" signed up and received 1,000 points", map[string]any{"points": signupBonusPoints, "email": email})
		return c, err
	}
	if err != nil {
		return customer{}, err
	}
	err = a.event(s.ID, sql.NullInt64{Int64: c.ID, Valid: true}, "signup_linked", eventAreaActivity, c.Name+" returned through signup", map[string]any{"email": email, "welcome_bonus_awarded": false})
	return c, err
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	id, _ := strconv.ParseInt(r.FormValue("customer_id"), 10, 64)
	c, err := a.loginCustomer(s, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "unknown customer", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := a.resetAppliedDiscount(s.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.closePopup(w, s, popupResult{Auth: c.Token, CustomerID: c.ID, ReturnTo: r.FormValue("return_to")})
}

func (a *app) loginCustomer(s sandbox, id int64) (customer, error) {
	c, err := a.customerByID(s.ID, id)
	if err != nil {
		return customer{}, err
	}
	err = a.event(s.ID, sql.NullInt64{Int64: c.ID, Valid: true}, "signin", eventAreaActivity, c.Name+" signed in", map[string]any{"customer_id": c.ID})
	return c, err
}

func (a *app) redeem(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	auth := r.FormValue("auth")
	c, err := a.customerByToken(s.ID, auth)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "invalid auth", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	reward := currentReward()
	if c.Points < reward.CostPoints {
		http.Error(w, "not enough points", http.StatusBadRequest)
		return
	}
	result, err := a.redeemReward(s, c)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result.ReturnTo = r.FormValue("return_to")
	a.closePopup(w, s, result)
}

func (a *app) closePopup(w http.ResponseWriter, s sandbox, result popupResult) {
	title := callbackClosePopup + " returned auth token"
	payload := map[string]any{"blockClientCall": callbackClosePopup, "auth_present": result.Auth != ""}
	if result.CustomerID != 0 {
		payload["customer_id"] = result.CustomerID
	}
	if result.DiscountCode != "" {
		title = callbackClosePopup + " returned redemption"
		payload["code"] = result.DiscountCode
	}
	if err := a.event(s.ID, sql.NullInt64{}, "close_popup", eventAreaTrace, title, payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "popup_done.html", popupDoneData{Sandbox: s, Auth: result.Auth, DiscountCode: result.DiscountCode, Callback: callbackClosePopup, ReturnTo: result.ReturnTo})
}

func (a *app) redeemReward(s sandbox, c customer) (popupResult, error) {
	tx, err := a.db.Begin()
	if err != nil {
		return popupResult{}, err
	}
	defer tx.Rollback()

	var existing discount
	err = tx.QueryRow(`select id, sandbox_id, customer_id, code, value_cents, status, created_at from discounts where sandbox_id = ? and customer_id = ?`, s.ID, c.ID).
		Scan(&existing.ID, &existing.SandboxID, &existing.CustomerID, &existing.Code, &existing.ValueCents, &existing.Status, &existing.CreatedAt)
	if err == nil {
		if err := insertEvent(tx, s.ID, sql.NullInt64{Int64: c.ID, Valid: true}, "duplicate_redemption_blocked", eventAreaActivity, "Duplicate redemption blocked", map[string]any{"code": existing.Code}); err != nil {
			return popupResult{}, err
		}
		if err := tx.Commit(); err != nil {
			return popupResult{}, err
		}
		return popupResult{Auth: c.Token, CustomerID: c.ID, DiscountCode: existing.Code}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return popupResult{}, err
	}

	code := discountCode(s, c)
	reward := currentReward()
	res, err := tx.Exec(`update customers set points = points - ? where id = ? and points >= ?`, reward.CostPoints, c.ID, reward.CostPoints)
	if err != nil {
		return popupResult{}, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return popupResult{}, err
	}
	if affected == 0 {
		return popupResult{}, errors.New("not enough points")
	}
	if _, err := tx.Exec(`insert into discounts (sandbox_id, customer_id, code, value_cents, status, created_at) values (?, ?, ?, ?, 'created', ?)`, s.ID, c.ID, code, reward.ValueCents, time.Now().UTC()); err != nil {
		return popupResult{}, err
	}
	if err := insertEvent(tx, s.ID, sql.NullInt64{Int64: c.ID, Valid: true}, "reward_redeemed", eventAreaActivity, c.Name+" redeemed "+reward.Title, map[string]any{"code": code, "points": -reward.CostPoints}); err != nil {
		return popupResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return popupResult{}, err
	}
	return popupResult{Auth: c.Token, CustomerID: c.ID, DiscountCode: code}, nil
}

func (a *app) admin(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	customers, err := a.customers(s.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, "admin.html", adminData{Sandbox: s, Customers: customers, linkData: a.links(r, s, "")})
}

func (a *app) adminCustomer(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sandboxFromRequest(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	c, err := a.customerByID(s.ID, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	customers, err := a.customers(s.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	activities, err := a.customerEvents(s.ID, c.ID, eventAreaActivity)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	traces, err := a.customerEvents(s.ID, c.ID, eventAreaTrace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	data := adminData{
		Sandbox:    s,
		Customer:   &c,
		Customers:  customers,
		Activities: activities,
		Traces:     traces,
		linkData:   a.links(r, s, ""),
	}
	a.render(w, "admin.html", data)
}

func nullCustomer(c customer) sql.NullInt64 {
	if c.ID == 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: c.ID, Valid: true}
}

func nullCustomerPtr(c *customer) sql.NullInt64 {
	if c == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: c.ID, Valid: true}
}

func pointsHistory(events []event) []pointsHistoryEntry {
	var history []pointsHistoryEntry
	for _, e := range events {
		var payload struct {
			Code   string `json:"code"`
			Points int    `json:"points"`
		}
		if err := json.Unmarshal([]byte(e.Payload), &payload); err != nil || payload.Points == 0 {
			continue
		}
		history = append(history, pointsHistoryEntry{
			Title:     e.Title,
			Code:      payload.Code,
			Points:    payload.Points,
			CreatedAt: e.CreatedAt,
		})
	}
	return history
}

func slugify(s string) string {
	s = strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = strings.Trim(re.ReplaceAllString(s, "-"), "-")
	s = strings.TrimSuffix(s, "-cosmetics")
	return s
}

func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func token(slug, email string) string {
	raw := slug + ":" + normalizeEmail(email)
	return "demo_" + strings.TrimRight(base64.RawURLEncoding.EncodeToString([]byte(raw)), "=")
}

func currentProduct() demoProduct {
	return demoProduct{
		Name:         "Radiance Serum",
		CategoryPath: "Home / Skincare / Serum",
		Copy:         "Brighten. Hydrate. Glow.",
		ImageURL:     "https://images.pexels.com/photos/4735943/pexels-photo-4735943.jpeg?auto=compress&cs=tinysrgb&w=900",
		ThumbnailURL: "https://images.pexels.com/photos/4735943/pexels-photo-4735943.jpeg?auto=compress&cs=tinysrgb&w=180",
		PriceCents:   4800,
		Rating:       "★★★★★",
		ReviewCount:  128,
		Quantity:     1,
	}
}

func currentReward() rewardOffer {
	valueCents := 1000
	return rewardOffer{
		Title:       fmt.Sprintf("$%d Off Your Order", dollars(valueCents)),
		CostPoints:  1000,
		ValueCents:  valueCents,
		RedeemLabel: fmt.Sprintf("Redeem $%d Off", dollars(valueCents)),
	}
}

func (a *app) cart(s sandbox) (cartState, error) {
	return a.cartForQuantity(s, currentProduct().Quantity)
}

func (a *app) cartForQuantity(s sandbox, quantity int) (cartState, error) {
	product := currentProduct()
	product.Quantity = quantity
	subtotal := product.PriceCents * product.Quantity

	discountCents := 0
	pendingCode := ""
	if s.AppliedDiscount != "" {
		value, err := a.discountValue(s.ID, s.AppliedDiscount)
		if err != nil {
			return cartState{}, err
		}
		discountCents = value
	} else {
		var err error
		pendingCode, err = a.pendingDiscountCode(s.ID)
		if err != nil {
			return cartState{}, err
		}
	}

	total := subtotal - discountCents
	if total < 0 {
		total = 0
	}
	return cartState{
		Product:             product,
		DiscountCode:        s.AppliedDiscount,
		PendingDiscountCode: pendingCode,
		SubtotalCents:       subtotal,
		DiscountCents:       discountCents,
		TotalCents:          total,
		HasDiscount:         s.AppliedDiscount != "",
		HasPendingDiscount:  pendingCode != "",
	}, nil
}

func (c cartState) response() cartResponse {
	return cartResponse{
		Code:              c.DiscountCode,
		DiscountLabel:     money(c.DiscountCents),
		SubtotalCents:     c.SubtotalCents,
		DiscountCents:     c.DiscountCents,
		TotalCents:        c.TotalCents,
		FormattedSubtotal: money(c.SubtotalCents),
		FormattedDiscount: money(c.DiscountCents),
		FormattedTotal:    money(c.TotalCents),
	}
}

func parseQuantity(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 1
	}
	if n > 99 {
		return 99
	}
	return n
}

func discountCode(s sandbox, c customer) string {
	code := s.DiscountTemplate
	if code == "" {
		code = defaultDiscountTemplate
	}
	brand := strings.ToUpper(strings.ReplaceAll(s.Slug, "-", ""))
	handle := strings.ToUpper(strings.Split(c.Name, " ")[0])
	if handle == "" {
		handle = fmt.Sprintf("CUS%d", c.ID)
	}
	code = strings.ReplaceAll(code, "{BRAND}", brand)
	code = strings.ReplaceAll(code, "{VALUE}", strconv.Itoa(dollars(currentReward().ValueCents)))
	code = strings.ReplaceAll(code, "{CUSTOMER}", handle)
	if !strings.Contains(code, handle) {
		code += "-" + handle
	}
	return code
}

func randomIdentity() (string, string) {
	names := []string{"Maya Stone", "Nadia Park", "Clara Reyes", "Iris Lane", "Sofia Vale"}
	name := names[rand.Intn(len(names))]
	email := strings.ToLower(strings.ReplaceAll(name, " ", ".")) + fmt.Sprintf("%d@example.test", rand.Intn(90)+10)
	return name, email
}
