-- Enable pgcrypto for generating clean unique hashes if needed
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- 1. Raw Ingested Records
CREATE TABLE IF NOT EXISTS ingested_records (
    id SERIAL PRIMARY KEY,
    source_file VARCHAR(20) NOT NULL, -- 'payments' or 'settlements'
    line_number INTEGER NOT NULL,
    settlement_id VARCHAR(50) NOT NULL,
    transaction_status VARCHAR(50) NOT NULL DEFAULT '', -- 'Released', 'Deferred', or empty
    raw_payload JSONB NOT NULL,
    idempotency_key VARCHAR(128) UNIQUE NOT NULL, -- source_file + line_number + hash(raw_payload)
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_ingested_settlement ON ingested_records(settlement_id);
CREATE INDEX IF NOT EXISTS idx_ingested_source ON ingested_records(source_file);

-- 2. Payments Configuration Rules
CREATE TABLE IF NOT EXISTS payment_configs (
    id SERIAL PRIMARY KEY,
    transaction_type TEXT NOT NULL,
    description TEXT NOT NULL,
    amount_field TEXT NOT NULL,
    record_ref TEXT NOT NULL,
    to_summary_field_when_positive_amount TEXT NOT NULL DEFAULT '',
    to_summary_field_when_negative_amount TEXT NOT NULL DEFAULT ''
);

-- 3. Settlements Configuration Rules
CREATE TABLE IF NOT EXISTS settlement_configs (
    id SERIAL PRIMARY KEY,
    transaction_type TEXT NOT NULL,
    amount_type TEXT NOT NULL,
    amount_description TEXT NOT NULL,
    record_ref TEXT NOT NULL,
    to_summary_field_when_positive_amount TEXT NOT NULL DEFAULT '',
    to_summary_field_when_negative_amount TEXT NOT NULL DEFAULT ''
);

-- 4. Mapped Transactions (Audit Trail Table)
CREATE TABLE IF NOT EXISTS mapped_records (
    id SERIAL PRIMARY KEY,
    ingested_record_id INTEGER NOT NULL REFERENCES ingested_records(id) ON DELETE CASCADE,
    config_type VARCHAR(20) NOT NULL, -- 'payments' or 'settlements'
    config_id INTEGER, -- References payment_configs(id) or settlement_configs(id)
    record_ref VARCHAR(255) NOT NULL,
    amount NUMERIC(15, 4) NOT NULL,
    summary_field VARCHAR(100) NOT NULL,
    mapping_status VARCHAR(50) NOT NULL, -- 'reconciled_eligible', 'excluded_deferred'
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_mapped_ref ON mapped_records(record_ref);
CREATE INDEX IF NOT EXISTS idx_mapped_field ON mapped_records(summary_field);
CREATE INDEX IF NOT EXISTS idx_mapped_status ON mapped_records(mapping_status);
