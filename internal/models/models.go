package models

import "time"

type ProductFAQ struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type Product struct {
	YouTubeReviewID    string         `json:"youtubeReviewId"`
	ID                 string         `json:"id"`
	SKU                string         `json:"sku"`
	Slug               string         `json:"slug"`
	Brand              string         `json:"brand"`
	Name               string         `json:"name"`
	Title              string         `json:"title"`
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
	FAQs               []ProductFAQ   `json:"faqs,omitempty"`
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
	Status        string          `json:"status"` // 'new', 'contacted', 'qualified', 'dispatched', 'delivered', 'cancelled', 'closed', 'spam'
	AdminNotes    string          `json:"adminNotes,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt,omitempty"`
	Payload       map[string]any  `json:"payload,omitempty"`
}

type FlashSaleConfig struct {
	IsActive   bool     `json:"isActive"`
	StartTime  string   `json:"startTime"`
	EndTime    string   `json:"endTime"`
	Title      string   `json:"title,omitempty"`
	ProductIDs []string `json:"productIds"`
}

type TrendingConfig struct {
	IsActive   bool     `json:"isActive"`
	Title      string   `json:"title,omitempty"`
	ProductIDs []string `json:"productIds"`
}

type CampaignConfig struct {
	FlashSale FlashSaleConfig `json:"flashSale"`
	Trending  TrendingConfig  `json:"trending"`
}

type AuditLog struct {
	ID         string         `json:"id"`
	Actor      string         `json:"actor"`
	Action     string         `json:"action"`
	EntityType string         `json:"entityType"`
	EntityID   string         `json:"entityId"`
	Details    map[string]any `json:"details,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

type AdminStats struct {
	TotalProducts      int   `json:"totalProducts"`
	PublishedProducts  int   `json:"publishedProducts"`
	OutOfStockProducts int   `json:"outOfStockProducts"`
	FlashDealCount     int   `json:"flashDealCount"`
	BestSellerCount    int   `json:"bestSellerCount"`
	TotalLeads         int   `json:"totalLeads"`
	NewLeads           int   `json:"newLeads"`
	ContactedLeads     int   `json:"contactedLeads"`
	DispatchedLeads    int   `json:"dispatchedLeads"`
	DeliveredLeads     int   `json:"deliveredLeads"`
	LeadsPipelineLKR   int64 `json:"leadsPipelineLkr"`
	PendingReviews     int   `json:"pendingReviews"`
	ApprovedReviews    int   `json:"approvedReviews"`
	FlatDeliveryFee    int   `json:"flatDeliveryFee"`
	PostgresConnected  bool  `json:"postgresConnected"`
	RedisConnected     bool  `json:"redisConnected"`
}
