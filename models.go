package main

import (
	"database/sql"
	"time"
)

type sandbox struct {
	ID               int64
	Slug             string
	BrandName        string
	Palette          string
	DiscountTemplate string
	AppliedDiscount  string
	CreatedAt        time.Time
}

type customer struct {
	ID        int64
	SandboxID int64
	Name      string
	Email     string
	Points    int
	Tier      string
	Token     string
	CreatedAt time.Time
}

type discount struct {
	ID         int64
	SandboxID  int64
	CustomerID int64
	Code       string
	ValueCents int
	Status     string
	CreatedAt  time.Time
}

type demoOrder struct {
	ID            int64
	SandboxID     int64
	ProductName   string
	SubtotalCents int
	DiscountCode  string
	DiscountCents int
	TotalCents    int
	CreatedAt     time.Time
}

type demoProduct struct {
	Name         string
	CategoryPath string
	Copy         string
	ImageURL     string
	ThumbnailURL string
	PriceCents   int
	Rating       string
	ReviewCount  int
	Quantity     int
}

type rewardOffer struct {
	Title       string
	CostPoints  int
	ValueCents  int
	RedeemLabel string
}

type cartState struct {
	Product             demoProduct
	DiscountCode        string
	PendingDiscountCode string
	SubtotalCents       int
	DiscountCents       int
	TotalCents          int
	HasDiscount         bool
	HasPendingDiscount  bool
}

type cartResponse struct {
	Code              string `json:"code"`
	DiscountLabel     string `json:"discountLabel"`
	SubtotalCents     int    `json:"subtotalCents"`
	DiscountCents     int    `json:"discountCents"`
	TotalCents        int    `json:"totalCents"`
	FormattedSubtotal string `json:"formattedSubtotal"`
	FormattedDiscount string `json:"formattedDiscount"`
	FormattedTotal    string `json:"formattedTotal"`
}

type resetCartResponse struct {
	PendingCode string `json:"pendingCode"`
}

type purchaseResponse struct {
	OrderID        int64  `json:"orderId"`
	ProductName    string `json:"productName"`
	DiscountCode   string `json:"discountCode"`
	FormattedTotal string `json:"formattedTotal"`
}

type event struct {
	ID         int64
	SandboxID  int64
	CustomerID sql.NullInt64
	Kind       string
	Area       string
	Title      string
	Payload    string
	CreatedAt  time.Time
}

type pointsHistoryEntry struct {
	Title     string
	Code      string
	Points    int
	CreatedAt time.Time
}

type linkData struct {
	SiteURL        string
	IframeURL      string
	RewardsPageURL string
	StorefrontURL  string
	AdminURL       string
	IntegrationURL string
	InstallSnippet string
}

type homeData struct {
	Sandboxes    []sandbox
	DefaultBrand string
}

type resultData struct {
	Sandbox sandbox
	linkData
}

type storefrontData struct {
	Sandbox sandbox
	Cart    cartState
	Orders  []demoOrder
	linkData
}

type rewardsHostData struct {
	Sandbox   sandbox
	IframeURL string
	linkData
}

type rewardsBlockData struct {
	Sandbox       sandbox
	Customer      *customer
	PointsHistory []pointsHistoryEntry
	RedeemedCode  string
	Reward        rewardOffer
}

type popupData struct {
	Sandbox       sandbox
	Customer      *customer
	Customers     []customer
	PopupKind     string
	ReturnTo      string
	RandomName    string
	RandomEmail   string
	Error         string
	SelectedEmail string
	Reward        rewardOffer
}

type popupDoneData struct {
	Sandbox      sandbox
	Auth         string
	DiscountCode string
	Callback     string
	ReturnTo     string
}

type adminData struct {
	Sandbox    sandbox
	Customer   *customer
	Customers  []customer
	Activities []event
	Traces     []event
	linkData
}

type popupResult struct {
	Auth         string
	CustomerID   int64
	DiscountCode string
	ReturnTo     string
}

const (
	defaultBrand            = "Acme Cosmetics"
	defaultPalette          = "black"
	defaultDiscountTemplate = "{BRAND}-{VALUE}-OFF"
	demoCustomerName        = "Olivia Chen"
	demoCustomerEmail       = "olivia@example.test"
	demoCustomerTier        = "Glow Insider"
	demoCustomerPoints      = 1500
	signupBonusPoints       = 1000
)

const (
	eventAreaActivity = "activity"
	eventAreaTrace    = "trace"
)

const (
	callbackResize            = "Resize1"
	callbackOpenPopup         = "OpenPopup1"
	callbackClosePopup        = "ClosePopup1"
	callbackApplyDiscountCode = "ApplyDiscountCode1"
)
