package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (a *app) recentSandboxes() ([]sandbox, error) {
	rows, err := a.db.Query(`select id, slug, brand_name, palette, discount_template, applied_discount, created_at from sandboxes order by id desc limit 8`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sandboxes []sandbox
	for rows.Next() {
		var s sandbox
		if err := rows.Scan(&s.ID, &s.Slug, &s.BrandName, &s.Palette, &s.DiscountTemplate, &s.AppliedDiscount, &s.CreatedAt); err != nil {
			return nil, err
		}
		sandboxes = append(sandboxes, s)
	}
	return sandboxes, rows.Err()
}

func (a *app) uniqueSlug(base string) (string, error) {
	if base == "" {
		base = "acme"
	}
	slug := base
	for i := 2; ; i++ {
		var id int64
		err := a.db.QueryRow(`select id from sandboxes where slug = ?`, slug).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return slug, nil
		}
		if err != nil {
			return "", err
		}
		slug = fmt.Sprintf("%s-%d", base, i)
	}
}

func (a *app) insertCustomer(c customer) (int64, error) {
	res, err := a.db.Exec(`insert into customers (sandbox_id, name, email, points, tier, token, created_at) values (?, ?, ?, ?, ?, ?, ?)`, c.SandboxID, c.Name, normalizeEmail(c.Email), c.Points, c.Tier, c.Token, c.CreatedAt)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (a *app) sandboxBySlug(slug string) (sandbox, error) {
	var s sandbox
	err := a.db.QueryRow(`select id, slug, brand_name, palette, discount_template, applied_discount, created_at from sandboxes where slug = ?`, slug).
		Scan(&s.ID, &s.Slug, &s.BrandName, &s.Palette, &s.DiscountTemplate, &s.AppliedDiscount, &s.CreatedAt)
	return s, err
}

func (a *app) customerByToken(sandboxID int64, tok string) (customer, error) {
	var c customer
	err := a.db.QueryRow(`select id, sandbox_id, name, email, points, tier, token, created_at from customers where sandbox_id = ? and token = ?`, sandboxID, tok).
		Scan(&c.ID, &c.SandboxID, &c.Name, &c.Email, &c.Points, &c.Tier, &c.Token, &c.CreatedAt)
	return c, err
}

func (a *app) customerByEmail(sandboxID int64, email string) (customer, error) {
	var c customer
	err := a.db.QueryRow(`select id, sandbox_id, name, email, points, tier, token, created_at from customers where sandbox_id = ? and email = ?`, sandboxID, normalizeEmail(email)).
		Scan(&c.ID, &c.SandboxID, &c.Name, &c.Email, &c.Points, &c.Tier, &c.Token, &c.CreatedAt)
	return c, err
}

func (a *app) customerByID(sandboxID, id int64) (customer, error) {
	var c customer
	err := a.db.QueryRow(`select id, sandbox_id, name, email, points, tier, token, created_at from customers where sandbox_id = ? and id = ?`, sandboxID, id).
		Scan(&c.ID, &c.SandboxID, &c.Name, &c.Email, &c.Points, &c.Tier, &c.Token, &c.CreatedAt)
	return c, err
}

func (a *app) customerByDiscountCode(sandboxID int64, code string) (customer, error) {
	var c customer
	err := a.db.QueryRow(`
		select customers.id, customers.sandbox_id, customers.name, customers.email, customers.points, customers.tier, customers.token, customers.created_at
		from discounts
		join customers on customers.id = discounts.customer_id
		where discounts.sandbox_id = ? and discounts.code = ?
	`, sandboxID, code).Scan(&c.ID, &c.SandboxID, &c.Name, &c.Email, &c.Points, &c.Tier, &c.Token, &c.CreatedAt)
	return c, err
}

func (a *app) customers(sandboxID int64) ([]customer, error) {
	rows, err := a.db.Query(`select id, sandbox_id, name, email, points, tier, token, created_at from customers where sandbox_id = ? order by id`, sandboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []customer
	for rows.Next() {
		var c customer
		if err := rows.Scan(&c.ID, &c.SandboxID, &c.Name, &c.Email, &c.Points, &c.Tier, &c.Token, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (a *app) discounts(sandboxID int64) ([]discount, error) {
	rows, err := a.db.Query(`select id, sandbox_id, customer_id, code, value_cents, status, created_at from discounts where sandbox_id = ? order by id desc`, sandboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []discount
	for rows.Next() {
		var d discount
		if err := rows.Scan(&d.ID, &d.SandboxID, &d.CustomerID, &d.Code, &d.ValueCents, &d.Status, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (a *app) discountValue(sandboxID int64, code string) (int, error) {
	var valueCents int
	err := a.db.QueryRow(`select value_cents from discounts where sandbox_id = ? and code = ?`, sandboxID, code).Scan(&valueCents)
	return valueCents, err
}

func (a *app) pendingDiscountCode(sandboxID int64) (string, error) {
	var code string
	err := a.db.QueryRow(`select code from discounts where sandbox_id = ? and status = 'created' order by id desc limit 1`, sandboxID).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return code, err
}

func (a *app) redeemedDiscountCode(sandboxID, customerID int64) (string, error) {
	var code string
	err := a.db.QueryRow(`select code from discounts where sandbox_id = ? and customer_id = ? order by id desc limit 1`, sandboxID, customerID).Scan(&code)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return code, err
}

func (a *app) orders(sandboxID int64) ([]demoOrder, error) {
	rows, err := a.db.Query(`select id, sandbox_id, product_name, subtotal_cents, discount_code, discount_cents, total_cents, created_at from orders where sandbox_id = ? order by id desc`, sandboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []demoOrder
	for rows.Next() {
		var o demoOrder
		if err := rows.Scan(&o.ID, &o.SandboxID, &o.ProductName, &o.SubtotalCents, &o.DiscountCode, &o.DiscountCents, &o.TotalCents, &o.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (a *app) events(sandboxID int64, area string) ([]event, error) {
	rows, err := a.db.Query(`select id, sandbox_id, customer_id, kind, area, title, payload, created_at from events where sandbox_id = ? and area = ? order by id desc limit 40`, sandboxID, area)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []event
	for rows.Next() {
		var e event
		if err := rows.Scan(&e.ID, &e.SandboxID, &e.CustomerID, &e.Kind, &e.Area, &e.Title, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (a *app) customerEvents(sandboxID, customerID int64, area string) ([]event, error) {
	rows, err := a.db.Query(`select id, sandbox_id, customer_id, kind, area, title, payload, created_at from events where sandbox_id = ? and customer_id = ? and area = ? order by id desc limit 40`, sandboxID, customerID, area)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []event
	for rows.Next() {
		var e event
		if err := rows.Scan(&e.ID, &e.SandboxID, &e.CustomerID, &e.Kind, &e.Area, &e.Title, &e.Payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type eventWriter interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func (a *app) event(sandboxID int64, customerID sql.NullInt64, kind, area, title string, payload map[string]any) error {
	return insertEvent(a.db, sandboxID, customerID, kind, area, title, payload)
}

func insertEvent(writer eventWriter, sandboxID int64, customerID sql.NullInt64, kind, area, title string, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = writer.Exec(`insert into events (sandbox_id, customer_id, kind, area, title, payload, created_at) values (?, ?, ?, ?, ?, ?, ?)`, sandboxID, nullable(customerID), kind, area, title, string(b), time.Now().UTC())
	return err
}

func nullable(n sql.NullInt64) any {
	if n.Valid {
		return n.Int64
	}
	return nil
}

func migrate(db *sql.DB) error {
	stmts := []string{
		`create table if not exists sandboxes (
			id integer primary key autoincrement,
			slug text not null unique,
			brand_name text not null,
			palette text not null,
			discount_template text not null,
			applied_discount text not null default '',
			created_at datetime not null
		)`,
		`create table if not exists customers (
			id integer primary key autoincrement,
			sandbox_id integer not null,
			name text not null,
			email text not null,
			points integer not null,
			tier text not null,
			token text not null unique,
			created_at datetime not null,
			unique(sandbox_id, email)
		)`,
		`create table if not exists discounts (
			id integer primary key autoincrement,
			sandbox_id integer not null,
			customer_id integer not null,
			code text not null,
			value_cents integer not null default 0,
			status text not null,
			created_at datetime not null,
			unique(sandbox_id, customer_id)
		)`,
		`create table if not exists orders (
			id integer primary key autoincrement,
			sandbox_id integer not null,
			product_name text not null,
			subtotal_cents integer not null,
			discount_code text not null,
			discount_cents integer not null,
			total_cents integer not null,
			created_at datetime not null
		)`,
		`create table if not exists events (
			id integer primary key autoincrement,
			sandbox_id integer not null,
			customer_id integer,
			kind text not null,
			area text not null,
			title text not null,
			payload text not null,
			created_at datetime not null
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	if err := ensureColumn(db, "discounts", "value_cents", "integer not null default 0"); err != nil {
		return err
	}
	if _, err := db.Exec(`update discounts set value_cents = ? where value_cents = 0`, currentReward().ValueCents); err != nil {
		return err
	}
	return nil
}

func ensureColumn(db *sql.DB, table, column, definition string) error {
	rows, err := db.Query(`pragma table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = db.Exec(`alter table ` + table + ` add column ` + column + ` ` + definition)
	return err
}
