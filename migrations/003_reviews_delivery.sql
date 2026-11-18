CREATE TABLE IF NOT EXISTS product_reviews (
 id TEXT PRIMARY KEY, product_id TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 2 AND 80), rating INT NOT NULL CHECK(rating BETWEEN 1 AND 5),
 body TEXT NOT NULL CHECK(length(body) BETWEEN 10 AND 2000), status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','rejected')),
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS reviews_product_status ON product_reviews(product_id,status);
CREATE TABLE IF NOT EXISTS delivery_rates (town TEXT PRIMARY KEY, fee INT NOT NULL CHECK(fee>=0), estimate TEXT NOT NULL);

ALTER TABLE product_reviews ADD COLUMN IF NOT EXISTS photo_data_url TEXT NOT NULL DEFAULT '';
