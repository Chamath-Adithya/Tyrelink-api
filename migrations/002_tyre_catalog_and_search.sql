-- 002_tyre_catalog_and_search.sql
-- Upgrades schema for high-scalability automotive tyre e-commerce and full-text search

-- 1. Upgrade products table
ALTER TABLE products ADD COLUMN IF NOT EXISTS slug TEXT UNIQUE;
ALTER TABLE products ADD COLUMN IF NOT EXISTS sinhala_title TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS featured_image TEXT DEFAULT '/hero-tyre.png';
ALTER TABLE products ADD COLUMN IF NOT EXISTS images JSONB DEFAULT '["/hero-tyre.png"]'::jsonb;
ALTER TABLE products ADD COLUMN IF NOT EXISTS specs JSONB DEFAULT '{}'::jsonb;
ALTER TABLE products ADD COLUMN IF NOT EXISTS short_description TEXT;
ALTER TABLE products ADD COLUMN IF NOT EXISTS original_price_minor BIGINT DEFAULT 0;
ALTER TABLE products ADD COLUMN IF NOT EXISTS stock_count INT DEFAULT 10;
ALTER TABLE products ADD COLUMN IF NOT EXISTS in_stock BOOLEAN DEFAULT TRUE;
ALTER TABLE products ADD COLUMN IF NOT EXISTS is_featured BOOLEAN DEFAULT FALSE;
ALTER TABLE products ADD COLUMN IF NOT EXISTS is_best_seller BOOLEAN DEFAULT FALSE;
ALTER TABLE products ADD COLUMN IF NOT EXISTS is_flash_deal BOOLEAN DEFAULT FALSE;
ALTER TABLE products ADD COLUMN IF NOT EXISTS flash_discount INT DEFAULT 0;
ALTER TABLE products ADD COLUMN IF NOT EXISTS warranty TEXT DEFAULT 'Manufacturer Warranty';
ALTER TABLE products ADD COLUMN IF NOT EXISTS rating NUMERIC(3,2) DEFAULT 4.8;
ALTER TABLE products ADD COLUMN IF NOT EXISTS review_count INT DEFAULT 12;
ALTER TABLE products ADD COLUMN IF NOT EXISTS max_order_quantity INT DEFAULT 4;

-- 2. Scalable B-Tree and GIN Indexes
CREATE INDEX IF NOT EXISTS idx_products_category ON products(category);
CREATE INDEX IF NOT EXISTS idx_products_brand ON products(brand);
CREATE INDEX IF NOT EXISTS idx_products_published ON products(published);
CREATE INDEX IF NOT EXISTS idx_products_flash_deal ON products(is_flash_deal) WHERE is_flash_deal = TRUE;
CREATE INDEX IF NOT EXISTS idx_products_best_seller ON products(is_best_seller) WHERE is_best_seller = TRUE;
CREATE INDEX IF NOT EXISTS idx_products_price_minor ON products(price_minor);
CREATE INDEX IF NOT EXISTS idx_products_created_at ON products(created_at DESC);

-- Full-Text Search index for tyre sizes, brands, and vehicle search
CREATE INDEX IF NOT EXISTS idx_products_fts ON products USING gin(
  to_tsvector('english', coalesce(name,'') || ' ' || coalesce(brand,'') || ' ' || coalesce(size,'') || ' ' || coalesce(category,'') || ' ' || coalesce(short_description,''))
);

-- 3. Upgrade order_leads table
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS order_ref TEXT UNIQUE;
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS customer_name TEXT;
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS phone1 TEXT;
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS phone2 TEXT;
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS address TEXT;
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS payment_method TEXT DEFAULT 'Cash on Delivery';
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS subtotal_minor BIGINT DEFAULT 0;
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS courier_minor BIGINT DEFAULT 45000;
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS grand_total_minor BIGINT DEFAULT 0;
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS items JSONB DEFAULT '[]'::jsonb;
ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT now();

CREATE INDEX IF NOT EXISTS idx_order_leads_ref ON order_leads(order_ref);
CREATE INDEX IF NOT EXISTS idx_order_leads_phone ON order_leads(phone1);
CREATE INDEX IF NOT EXISTS idx_order_leads_status ON order_leads(status);
CREATE INDEX IF NOT EXISTS idx_order_leads_created_at ON order_leads(created_at DESC);

-- 4. Seed initial tyre catalog
INSERT INTO products (id, sku, slug, brand, name, sinhala_title, category, size, price_minor, original_price_minor, featured_image, images, specs, short_description, stock_count, in_stock, is_featured, is_best_seller, is_flash_deal, flash_discount, warranty, rating, review_count, max_order_quantity, published)
VALUES
('tl-ceat-milaze-155', 'TL-CAR-155-01', 'ceat-milaze-155-70-r13', 'CEAT', 'CEAT Milaze 155/70 R13', 'CEAT Milaze කාර් ටයරය', 'car-tyres', '155/70 R13', 1850000, 2050000, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Size": "155/70 R13", "Construction": "Radial tubeless", "Vehicle": "Compact cars", "Warranty": "5 years"}'::jsonb, 'Reliable everyday passenger tyre with balanced comfort and tread life.', 12, true, true, true, true, 10, '5 Year Manufacturer Warranty', 4.7, 28, 4, true),
('tl-dsi-sdrive-185', 'TL-CAR-185-02', 'dsi-s-drive-185-65-r15', 'DSI', 'DSI S-Drive 185/65 R15', 'DSI S-Drive කාර් ටයරය', 'car-tyres', '185/65 R15', 2990000, 3250000, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Size": "185/65 R15", "Load index": "91H", "Construction": "Radial tubeless", "Warranty": "4 years"}'::jsonb, 'Comfortable, value-focused radial tyre for everyday Sri Lankan roads.', 8, true, true, true, false, 0, '4 Year Manufacturer Warranty', 4.8, 42, 4, true),
('tl-michelin-xm2-195', 'TL-CAR-195-03', 'michelin-energy-xm2-195-65-r15', 'Michelin', 'Michelin Energy XM2+ 195/65 R15', 'මිචෙලින් කාර් ටයරය', 'car-tyres', '195/65 R15', 5450000, 5800000, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Size": "195/65 R15", "Load index": "91H", "Construction": "Radial tubeless", "Warranty": "5 years"}'::jsonb, 'Premium long-life tyre engineered for comfort, braking confidence and efficiency.', 5, true, true, true, false, 0, '5 Year Manufacturer Warranty', 4.9, 19, 4, true),
('tl-dunlop-at5-265', 'TL-SUV-265-01', 'dunlop-grandtrek-at5-265-65-r17', 'Dunlop', 'Dunlop Grandtrek AT5 265/65 R17', 'Dunlop Grandtrek SUV ටයරය', 'suv-4x4', '265/65 R17', 7800000, 8350000, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Size": "265/65 R17", "Load index": "112T", "Terrain": "All-terrain", "Warranty": "5 years"}'::jsonb, 'All-terrain SUV tyre for confident touring, wet roads and light off-road use.', 4, true, true, false, true, 7, '5 Year Manufacturer Warranty', 4.8, 16, 4, true),
('tl-bridgestone-dueler-265', 'TL-SUV-265-02', 'bridgestone-dueler-ht-265-65-r17', 'Bridgestone', 'Bridgestone Dueler H/T 265/65 R17', 'බ්‍රිජ්ස්ටෝන් SUV ටයරය', 'suv-4x4', '265/65 R17', 8600000, 0, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Size": "265/65 R17", "Load index": "112S", "Terrain": "Highway terrain", "Warranty": "5 years"}'::jsonb, 'Quiet highway handling and dependable durability for SUVs and pickups.', 3, true, true, false, false, 0, '5 Year Manufacturer Warranty', 4.8, 11, 4, true),
('tl-ceat-zoom-90', 'TL-MOTO-90-01', 'ceat-zoom-plus-90-90-17', 'CEAT', 'CEAT Zoom Plus 90/90-17', 'CEAT Zoom යතුරුපැදි ටයරය', 'motorcycle', '90/90-17', 1290000, 1400000, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Size": "90/90-17", "Fitment": "Front or rear — confirm with us", "Type": "Tubeless", "Warranty": "Manufacturer warranty"}'::jsonb, 'Dependable tubeless motorcycle tyre for urban grip and daily mileage.', 18, true, false, true, false, 0, 'Manufacturer Warranty', 4.7, 37, 2, true),
('tl-michelin-pilot-100', 'TL-MOTO-100-02', 'michelin-pilot-street-2-100-80-17', 'Michelin', 'Michelin Pilot Street 2 100/80-17', 'මිචෙලින් මෝටර්සයිකල් ටයරය', 'motorcycle', '100/80-17', 2450000, 0, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Size": "100/80-17", "Fitment": "Rear — confirm with us", "Type": "Tubeless", "Warranty": "5 years"}'::jsonb, 'Premium motorcycle control and wet-weather confidence.', 6, true, true, false, false, 0, '5 Year Manufacturer Warranty', 4.9, 14, 2, true),
('tl-hankook-vantra-195', 'TL-COMM-195-01', 'hankook-vantra-195-70-r15c', 'Hankook', 'Hankook Vantra LT 195/70 R15C', 'Hankook වාණිජ වෑන් ටයරය', 'commercial', '195/70 R15C', 4790000, 0, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Size": "195/70 R15C", "Rating": "104/102R", "Construction": "Commercial radial", "Warranty": "4 years"}'::jsonb, 'Reinforced commercial tyre for dependable load carrying and fleet use.', 7, true, false, true, false, 0, '4 Year Manufacturer Warranty', 4.6, 9, 6, true),
('tl-valve-kit', 'TL-ACC-VALVE-01', 'tubeless-valve-service-kit', 'Tyrelink', 'Tubeless Valve Service Kit', 'ටියුබ්ලස් වෑල්ව් කට්ටලය', 'wheels', 'Standard', 185000, 0, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Contents": "4 valves and caps", "Fitment": "Tubeless passenger wheels", "Use": "Tyre service accessory"}'::jsonb, 'Essential tubeless valve and cap kit for a reliable tyre service.', 40, true, false, true, false, 0, 'Fitment checked on collection', 4.8, 22, 5, true),
('tl-air-compressor', 'TL-EQ-COMP-01', 'garage-air-compressor-100l', 'Tyrelink Pro', '100L Garage Air Compressor', 'ලීටර් 100 එයාර් කොම්ප්‍රෙෂර්', 'garage-tools', '100L', 18500000, 0, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Tank": "100 litres", "Use": "Workshop air supply", "Support": "Installation guidance included", "Warranty": "1 year"}'::jsonb, 'Workshop-ready air compressor for tyre service, inflation and pneumatic tools.', 2, true, true, false, false, 0, '1 Year Equipment Warranty', 4.8, 6, 1, true),
('tl-wheel-balancer', 'TL-EQ-BAL-01', 'digital-wheel-balancer', 'Tyrelink Pro', 'Digital Wheel Balancer', 'ඩිජිටල් වීල් බැලන්සර් යන්ත්‍රය', 'garage-equipment', 'Digital Universal', 48500000, 0, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Application": "Passenger and light-commercial wheels", "Service": "Installation and training", "Warranty": "1 year"}'::jsonb, 'Professional digital wheel balancing equipment with installation and operator guidance.', 1, true, true, false, false, 0, '1 Year Equipment Warranty', 4.9, 4, 1, true),
('tl-tyre-changer', 'TL-EQ-CHG-01', 'semi-automatic-tyre-changer', 'Tyrelink Pro', 'Semi-Automatic Tyre Changer', 'ටයර් ගලවන සහ සවිකරන යන්ත්‍රය', 'garage-equipment', 'Semi-Automatic', 62500000, 0, '/hero-tyre.png', '["/hero-tyre.png"]'::jsonb, '{"Application": "Passenger and light-commercial tyres", "Service": "Installation and training", "Warranty": "1 year"}'::jsonb, 'Tyre-centre equipment supplied with setup, training and after-sales support.', 1, true, false, true, false, 0, '1 Year Equipment Warranty', 4.9, 3, 1, true)
ON CONFLICT (id) DO UPDATE SET
  slug = EXCLUDED.slug,
  name = EXCLUDED.name,
  brand = EXCLUDED.brand,
  price_minor = EXCLUDED.price_minor,
  stock_count = EXCLUDED.stock_count,
  specs = EXCLUDED.specs,
  updated_at = now();

