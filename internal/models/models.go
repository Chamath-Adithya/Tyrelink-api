package models

import "time"

type Product struct {
	ID                 string         `json:"id"`
	SKU                string         `json:"sku"`
	Slug               string         `json:"slug"`
	Brand              string         `json:"brand"`
	Name               string         `json:"name"`
	SinhalaTitle       string         `json:"sinhalaTitle,omitempty"`
	Category           string         `json:"category"`
	Size               string         `json:"size,omitempty"`
	PriceMinor         int64          `json:"priceMinor"`
	Price              int64          `json:"price"` // In LKR (Rupees)
	OriginalPriceMinor int64          `json:"originalPriceMinor,omitempty"`
	OriginalPrice      int64          `json:"originalPrice,omitempty"`
	Currency           string         `json:"currency"`
	PriceIndicative    bool           `json:"priceIndicative"`
	FeaturedImage      string         `json:"featuredImage"`
	Images             []string       `json:"images"`
	Specs              map[string]any `json:"specs"`
	ShortDescription   string         `json:"shortDescription,omitempty"`
	SinhalaDescription string         `json:"sinhalaDescription,omitempty"`
	StockCount         int            `json:"stockCount"`
	InStock            bool           `json:"inStock"`
	IsFeatured         bool           `json:"isFeatured"`
	IsBestSeller       bool           `json:"isBestSeller"`
	IsFlashDeal        bool           `json:"isFlashDeal"`
	FlashDealDiscount  int            `json:"flashDealDiscount,omitempty"`
	Warranty           string         `json:"warranty,omitempty"`
	Rating             float64        `json:"rating,omitempty"`
	ReviewCount        int            `json:"reviewCount,omitempty"`
	MaxOrderQuantity   int            `json:"maxOrderQuantity,omitempty"`
	Published          bool           `json:"published"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

type OrderLeadItem struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Price            int64    `json:"price"`
	Quantity         int      `json:"quantity"`
	Image            string   `json:"image,omitempty"`
	SelectedUpgrades []string `json:"selectedUpgrades,omitempty"`
}

type OrderLead struct {
	ID            string          `json:"id"`
	OrderRef      string          `json:"orderRef"`
	CustomerName  string          `json:"customerName"`
	Address       string          `json:"address"`
	Phone1        string          `json:"phone1"`
	Phone2        string          `json:"phone2,omitempty"`
	PaymentMethod string          `json:"paymentMethod"`
	Items         []OrderLeadItem `json:"items"`
	Subtotal      int64           `json:"subtotal"`
	CourierCharge int64           `json:"courierCharge"`
	GrandTotal    int64           `json:"grandTotal"`
	Status        string          `json:"status"` // 'new', 'contacted', 'dispatched', 'delivered', 'cancelled'
	CreatedAt     time.Time       `json:"createdAt"`
	Payload       map[string]any  `json:"payload,omitempty"`
}

type CampaignConfig struct {
	FlashSale struct {
		IsActive   bool     `json:"isActive"`
		StartTime  string   `json:"startTime"`
		EndTime    string   `json:"endTime"`
		Title      string   `json:"title"`
		ProductIDs []string `json:"productIds"`
	} `json:"flashSale"`
	Trending struct {
		Title      string   `json:"title"`
		ProductIDs []string `json:"productIds"`
	} `json:"trending"`
}
