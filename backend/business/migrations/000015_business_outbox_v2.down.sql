ALTER TABLE business_outbox
    DROP CHECK chk_business_outbox_schema_version,
    ADD CONSTRAINT chk_business_outbox_schema_version CHECK (schema_version = 1);
