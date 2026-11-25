-- 006_cleanup_and_indexes.sql
-- Removes deprecated warranty registry table/attributes and guarantees index support.

DROP TABLE IF EXISTS warranty_records;

UPDATE products
SET specs = specs - 'Warranty'
WHERE specs ? 'Warranty';

UPDATE products
SET warranty = ''
WHERE warranty <> '';

CREATE INDEX IF NOT EXISTS idx_products_category_brand_pub
  ON products (published, category, brand);

