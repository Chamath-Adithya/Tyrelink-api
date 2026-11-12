package database

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/solveo/tyrelink/backend/internal/models"
)

type DB struct {
	Pool        *pgxpool.Pool
	mu          sync.RWMutex
	memoryItems map[string]models.Product
	memoryLeads []models.OrderLead
}

func Connect(ctx context.Context, dsn string) *DB {
	db := &DB{
		memoryItems: make(map[string]models.Product),
		memoryLeads: make([]models.OrderLead, 0),
	}

	// Pre-populate in-memory data for offline/standalone mode
	seedMemoryData(db)

	if dsn == "" {
		log.Println("PostgreSQL DSN not provided: Running in embedded memory storage mode")
		return db
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Printf("Failed to parse DATABASE_URL: %v. Running in memory mode.", err)
		return db
	}

	cfg.MaxConns = 25
	cfg.MinConns = 5
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		log.Printf("Failed to connect to PostgreSQL: %v. Running in memory mode.", err)
		return db
	}

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		log.Printf("PostgreSQL ping failed: %v. Running in memory mode.", err)
		return db
	}

	log.Println("Connected to PostgreSQL successfully")
	db.Pool = pool
	return db
}

func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}

func (db *DB) IsPostgres() bool {
	return db.Pool != nil
}

// Memory getters/setters for standalone execution
func (db *DB) GetMemoryProducts() []models.Product {
	db.mu.RLock()
	defer db.mu.RUnlock()
	res := make([]models.Product, 0, len(db.memoryItems))
	for _, p := range db.memoryItems {
		if p.Published {
			res = append(res, p)
		}
	}
	return res
}

func (db *DB) GetMemoryProductByID(idOrSlug string) (*models.Product, bool) {
	db.mu.RLock()
	defer db.mu.RUnlock()
	for _, p := range db.memoryItems {
		if p.ID == idOrSlug || p.Slug == idOrSlug {
			return &p, true
		}
	}
	return nil, false
}

func (db *DB) SaveMemoryProduct(p models.Product) {
	db.mu.Lock()
	defer db.mu.Unlock()
	p.UpdatedAt = time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = p.UpdatedAt
	}
	db.memoryItems[p.ID] = p
}

func (db *DB) DeleteMemoryProduct(id string) bool {
	db.mu.Lock()
	defer db.mu.Unlock()
	p, ok := db.memoryItems[id]
	if !ok {
		return false
	}
	p.Published = false
	p.UpdatedAt = time.Now().UTC()
	db.memoryItems[id] = p
	return true
}

func (db *DB) AddMemoryLead(lead models.OrderLead) {
	db.mu.Lock()
	defer db.mu.Unlock()
	db.memoryLeads = append([]models.OrderLead{lead}, db.memoryLeads...)
}

func (db *DB) GetMemoryLeads() []models.OrderLead {
	db.mu.RLock()
	defer db.mu.RUnlock()
	res := make([]models.OrderLead, len(db.memoryLeads))
	copy(res, db.memoryLeads)
	return res
}

func seedMemoryData(db *DB) {
	seeds := []models.Product{
		{
			ID: "tl-ceat-milaze-155", SKU: "TL-CAR-155-01", Slug: "ceat-milaze-155-70-r13",
			Brand: "CEAT", Name: "CEAT Milaze 155/70 R13", SinhalaTitle: "CEAT Milaze කාර් ටයරය",
			Category: "car-tyres", Size: "155/70 R13", PriceMinor: 1850000, Price: 18500,
			OriginalPriceMinor: 2050000, OriginalPrice: 20500, Currency: "LKR",
			FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Size": "155/70 R13", "Construction": "Radial tubeless", "Vehicle": "Compact cars", "Warranty": "5 years"},
			ShortDescription: "Reliable everyday passenger tyre with balanced comfort and tread life.",
			StockCount:       12, InStock: true, IsFeatured: true, IsBestSeller: true, IsFlashDeal: true, FlashDealDiscount: 10,
			Warranty: "5 Year Manufacturer Warranty", Rating: 4.7, ReviewCount: 28, MaxOrderQuantity: 4, Published: true,
		},
		{
			ID: "tl-dsi-sdrive-185", SKU: "TL-CAR-185-02", Slug: "dsi-s-drive-185-65-r15",
			Brand: "DSI", Name: "DSI S-Drive 185/65 R15", SinhalaTitle: "DSI S-Drive කාර් ටයරය",
			Category: "car-tyres", Size: "185/65 R15", PriceMinor: 2990000, Price: 29900,
			OriginalPriceMinor: 3250000, OriginalPrice: 32500, Currency: "LKR",
			FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Size": "185/65 R15", "Load index": "91H", "Construction": "Radial tubeless", "Warranty": "4 years"},
			ShortDescription: "Comfortable, value-focused radial tyre for everyday Sri Lankan roads.",
			StockCount:       8, InStock: true, IsFeatured: true, IsBestSeller: true,
			Warranty: "4 Year Manufacturer Warranty", Rating: 4.8, ReviewCount: 42, MaxOrderQuantity: 4, Published: true,
		},
		{
			ID: "tl-michelin-xm2-195", SKU: "TL-CAR-195-03", Slug: "michelin-energy-xm2-195-65-r15",
			Brand: "Michelin", Name: "Michelin Energy XM2+ 195/65 R15", SinhalaTitle: "මිචෙලින් කාර් ටයරය",
			Category: "car-tyres", Size: "195/65 R15", PriceMinor: 5450000, Price: 54500,
			OriginalPriceMinor: 5800000, OriginalPrice: 58000, Currency: "LKR",
			FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Size": "195/65 R15", "Load index": "91H", "Construction": "Radial tubeless", "Warranty": "5 years"},
			ShortDescription: "Premium long-life tyre engineered for comfort, braking confidence and efficiency.",
			StockCount:       5, InStock: true, IsFeatured: true, IsBestSeller: true,
			Warranty: "5 Year Manufacturer Warranty", Rating: 4.9, ReviewCount: 19, MaxOrderQuantity: 4, Published: true,
		},
		{
			ID: "tl-dunlop-at5-265", SKU: "TL-SUV-265-01", Slug: "dunlop-grandtrek-at5-265-65-r17",
			Brand: "Dunlop", Name: "Dunlop Grandtrek AT5 265/65 R17",
			Category: "suv-4x4", Size: "265/65 R17", PriceMinor: 7800000, Price: 78000,
			OriginalPriceMinor: 8350000, OriginalPrice: 83500, Currency: "LKR",
			FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Size": "265/65 R17", "Load index": "112T", "Terrain": "All-terrain", "Warranty": "5 years"},
			ShortDescription: "All-terrain SUV tyre for confident touring, wet roads and light off-road use.",
			StockCount:       4, InStock: true, IsFeatured: true, IsFlashDeal: true, FlashDealDiscount: 7,
			Warranty: "5 Year Manufacturer Warranty", Rating: 4.8, ReviewCount: 16, MaxOrderQuantity: 4, Published: true,
		},
		{
			ID: "tl-bridgestone-dueler-265", SKU: "TL-SUV-265-02", Slug: "bridgestone-dueler-ht-265-65-r17",
			Brand: "Bridgestone", Name: "Bridgestone Dueler H/T 265/65 R17",
			Category: "suv-4x4", Size: "265/65 R17", PriceMinor: 8600000, Price: 86000,
			Currency: "LKR", FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Size": "265/65 R17", "Load index": "112S", "Terrain": "Highway terrain", "Warranty": "5 years"},
			ShortDescription: "Quiet highway handling and dependable durability for SUVs and pickups.",
			StockCount:       3, InStock: true, IsFeatured: true,
			Warranty: "5 Year Manufacturer Warranty", Rating: 4.8, ReviewCount: 11, MaxOrderQuantity: 4, Published: true,
		},
		{
			ID: "tl-ceat-zoom-90", SKU: "TL-MOTO-90-01", Slug: "ceat-zoom-plus-90-90-17",
			Brand: "CEAT", Name: "CEAT Zoom Plus 90/90-17",
			Category: "motorcycle", Size: "90/90-17", PriceMinor: 1290000, Price: 12900,
			OriginalPriceMinor: 1400000, OriginalPrice: 14000, Currency: "LKR",
			FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Size": "90/90-17", "Fitment": "Front or rear — confirm with us", "Type": "Tubeless", "Warranty": "Manufacturer warranty"},
			ShortDescription: "Dependable tubeless motorcycle tyre for urban grip and daily mileage.",
			StockCount:       18, InStock: true, IsBestSeller: true,
			Warranty: "Manufacturer Warranty", Rating: 4.7, ReviewCount: 37, MaxOrderQuantity: 2, Published: true,
		},
		{
			ID: "tl-michelin-pilot-100", SKU: "TL-MOTO-100-02", Slug: "michelin-pilot-street-2-100-80-17",
			Brand: "Michelin", Name: "Michelin Pilot Street 2 100/80-17",
			Category: "motorcycle", Size: "100/80-17", PriceMinor: 2450000, Price: 24500,
			Currency: "LKR", FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Size": "100/80-17", "Fitment": "Rear — confirm with us", "Type": "Tubeless", "Warranty": "5 years"},
			ShortDescription: "Premium motorcycle control and wet-weather confidence.",
			StockCount:       6, InStock: true, IsFeatured: true,
			Warranty: "5 Year Manufacturer Warranty", Rating: 4.9, ReviewCount: 14, MaxOrderQuantity: 2, Published: true,
		},
		{
			ID: "tl-hankook-vantra-195", SKU: "TL-COMM-195-01", Slug: "hankook-vantra-195-70-r15c",
			Brand: "Hankook", Name: "Hankook Vantra LT 195/70 R15C",
			Category: "commercial", Size: "195/70 R15C", PriceMinor: 4790000, Price: 47900,
			Currency: "LKR", FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Size": "195/70 R15C", "Rating": "104/102R", "Construction": "Commercial radial", "Warranty": "4 years"},
			ShortDescription: "Reinforced commercial tyre for dependable load carrying and fleet use.",
			StockCount:       7, InStock: true, IsBestSeller: true,
			Warranty: "4 Year Manufacturer Warranty", Rating: 4.6, ReviewCount: 9, MaxOrderQuantity: 6, Published: true,
		},
		{
			ID: "tl-valve-kit", SKU: "TL-ACC-VALVE-01", Slug: "tubeless-valve-service-kit",
			Brand: "Tyrelink", Name: "Tubeless Valve Service Kit",
			Category: "wheels", Size: "Standard", PriceMinor: 185000, Price: 1850,
			Currency: "LKR", FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Contents": "4 valves and caps", "Fitment": "Tubeless passenger wheels", "Use": "Tyre service accessory"},
			ShortDescription: "Essential tubeless valve and cap kit for a reliable tyre service.",
			StockCount:       40, InStock: true, IsBestSeller: true,
			Warranty: "Fitment checked on collection", Rating: 4.8, ReviewCount: 22, MaxOrderQuantity: 5, Published: true,
		},
		{
			ID: "tl-air-compressor", SKU: "TL-EQ-COMP-01", Slug: "garage-air-compressor-100l",
			Brand: "Tyrelink Pro", Name: "100L Garage Air Compressor",
			Category: "garage-tools", Size: "100L", PriceMinor: 18500000, Price: 185000,
			Currency: "LKR", FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Tank": "100 litres", "Use": "Workshop air supply", "Support": "Installation guidance included", "Warranty": "1 year"},
			ShortDescription: "Workshop-ready air compressor for tyre service, inflation and pneumatic tools.",
			StockCount:       2, InStock: true, IsFeatured: true,
			Warranty: "1 Year Equipment Warranty", Rating: 4.8, ReviewCount: 6, MaxOrderQuantity: 1, Published: true,
		},
		{
			ID: "tl-wheel-balancer", SKU: "TL-EQ-BAL-01", Slug: "digital-wheel-balancer",
			Brand: "Tyrelink Pro", Name: "Digital Wheel Balancer",
			Category: "garage-equipment", Size: "Digital Universal", PriceMinor: 48500000, Price: 485000,
			Currency: "LKR", FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Application": "Passenger and light-commercial wheels", "Service": "Installation and training", "Warranty": "1 year"},
			ShortDescription: "Professional digital wheel balancing equipment with installation and operator guidance.",
			StockCount:       1, InStock: true, IsFeatured: true,
			Warranty: "1 Year Equipment Warranty", Rating: 4.9, ReviewCount: 4, MaxOrderQuantity: 1, Published: true,
		},
		{
			ID: "tl-tyre-changer", SKU: "TL-EQ-CHG-01", Slug: "semi-automatic-tyre-changer",
			Brand: "Tyrelink Pro", Name: "Semi-Automatic Tyre Changer",
			Category: "garage-equipment", Size: "Semi-Automatic", PriceMinor: 62500000, Price: 625000,
			Currency: "LKR", FeaturedImage: "/hero-tyre.png", Images: []string{"/hero-tyre.png"},
			Specs:            map[string]any{"Application": "Passenger and light-commercial tyres", "Service": "Installation and training", "Warranty": "1 year"},
			ShortDescription: "Tyre-centre equipment supplied with setup, training and after-sales support.",
			StockCount:       1, InStock: true, IsBestSeller: true,
			Warranty: "1 Year Equipment Warranty", Rating: 4.9, ReviewCount: 3, MaxOrderQuantity: 1, Published: true,
		},
	}

	for _, s := range seeds {
		db.memoryItems[s.ID] = s
	}
}

