CREATE TABLE accounts (
    id         SERIAL PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    available  NUMERIC(20, 2) NOT NULL
);

INSERT INTO accounts (name, available) VALUES ('test-account', 1000.00)
ON CONFLICT (name) DO NOTHING;
