-- 005_admin_enhancements.sql
-- Aligns order lead workflow statuses, adds admin notes, audit logging, campaigns seed, and missing catalog products.

-- 1. Expand order_leads.status CHECK constraint to cover full fulfillment lifecycle
DO $$
DECLARE
  con_name text;
BEGIN
  SELECT c.conname INTO con_name
  FROM pg_constraint c
  JOIN pg_class t ON c.conrelid = t.oid
  WHERE t.relname = 'order_leads'
    AND c.contype = 'c'
    AND pg_get_constraintdef(c.oid) LIKE '%status%';

  IF con_name IS NOT NULL THEN
    EXECUTE format('ALTER TABLE order_leads DROP CONSTRAINT %I', con_name);
  END IF;
END $$;

ALTER TABLE order_leads
  ADD CONSTRAINT order_leads_status_check
  CHECK (status IN ('new','contacted','qualified','dispatched','delivered','cancelled','closed','spam'));

ALTER TABLE order_leads ADD COLUMN IF NOT EXISTS admin_notes TEXT NOT NULL DEFAULT '';

-- 2. Audit log table for traceable admin operations
CREATE TABLE IF NOT EXISTS audit_logs (
  id TEXT PRIMARY KEY,
  actor TEXT NOT NULL DEFAULT 'admin',
  action TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL DEFAULT '',
  details JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_entity ON audit_logs(entity_type, entity_id);

-- 3. Seed missing garage tool / valve products from storefront catalog
INSERT INTO products (
  id, sku, slug, brand, name, sinhala_title, category, size,
  price_minor, original_price_minor, featured_image, images, specs,
  short_description, stock_count, in_stock, is_featured, is_best_seller,
  is_flash_deal, flash_discount, warranty, rating, review_count, max_order_quantity, published
)
VALUES
(
  'tl-impact-wrench-half', 'TL-TOOL-WRENCH-01', 'twin-hammer-air-impact-wrench',
  'Tyrelink Pro', '1/2" Twin Hammer Air Impact Wrench', '1/2" එයාර් ඉම්පැක්ට් රෙන්ච්',
  'garage-tools', '1/2 inch Drive', 1750000, 1980000,
  '/images/legacy/wrenches-sri-lanka.webp', '["/images/legacy/wrenches-sri-lanka.webp"]'::jsonb,
  '{"Drive":"1/2 inch Square","Mechanism":"Twin Hammer Pneumatic","Vehicle":"Cars, SUVs & Light Vans","Warranty":"1 year"}'::jsonb,
  'High-torque twin hammer pneumatic wrench built for fast lug nut removal in tyre bays.',
  14, true, false, true, false, 12, '1 Year Workshop Warranty', 4.8, 15, 3, true
),
(
  'tl-nitrogen-inflator', 'TL-EQ-N2-01', 'digital-nitrogen-tyre-inflator',
  'Tyrelink Pro', 'Digital Nitrogen Tyre Inflator', 'ඩිජිටල් නයිට්‍රජන් ඉන්ෆ්ලේටර් යන්ත්‍රය',
  'garage-equipment', 'Digital Dual LED', 14500000, 15800000,
  '/images/legacy/nitrogen-machine.webp', '["/images/legacy/nitrogen-machine.webp"]'::jsonb,
  '{"Display":"Dual Digital LED","Application":"Passenger & Commercial Bays","Warranty":"1 year"}'::jsonb,
  'Precision digital nitrogen tyre inflator for consistent pressure accuracy across workshop bays.',
  3, true, true, false, false, 8, '1 Year Equipment Warranty', 4.9, 7, 1, true
),
(
  'tl-tr13-valve-kit', 'TL-ACC-TR13-01', 'heavy-duty-tr13-valve-set',
  'Tyrelink', 'Heavy-Duty TR13 Tubeless Valve Set', 'TR13 හෙවි-ඩියුටි ටියුබ්ලස් වෑල්ව්',
  'wheels', 'TR13 Standard', 240000, 0,
  '/images/legacy/valves-sri-lanka.webp', '["/images/legacy/valves-sri-lanka.webp"]'::jsonb,
  '{"Type":"TR13 Snap-in Brass Core","Vehicle":"All Passenger & Van Rims","Warranty":"6 months"}'::jsonb,
  'Heavy-duty brass-core snap-in TR13 valves engineered for long-lasting rim airtightness.',
  50, true, false, false, false, 0, '6 Month Replacement Warranty', 4.8, 19, 10, true
)
ON CONFLICT (id) DO NOTHING;

-- 4. Seed initial homepage campaigns configuration
INSERT INTO campaigns (title, slug, content, active, starts_at, ends_at)
VALUES (
  'Homepage Campaigns',
  'home-campaigns',
  '{
    "flashSale": {
      "isActive": true,
      "startTime": "2026-11-01T00:00:00.000Z",
      "endTime": "2026-11-30T23:59:59.000Z",
      "title": "Today''s Flash Deals",
      "productIds": ["tl-dunlop-at5-265","tl-ceat-zoom-90","tl-air-compressor","tl-michelin-pilot-100"]
    },
    "trending": {
      "isActive": true,
      "title": "Trending & Best Sellers",
      "productIds": ["tl-michelin-xm2-195","tl-bridgestone-dueler-265","tl-michelin-pilot-100","tl-hankook-vantra-195","tl-valve-kit"]
    }
  }'::jsonb,
  true,
  '2026-11-01T00:00:00.000Z',
  '2026-11-30T23:59:59.000Z'
)
ON CONFLICT (slug) DO NOTHING;

