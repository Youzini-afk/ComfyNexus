-- 003_library_indexes.sql -- Phase 3 library compatibility fields/indexes
ALTER TABLE models ADD COLUMN remote_mtime DATETIME;
CREATE INDEX IF NOT EXISTS idx_models_instance_type ON models(instance_id, model_type);
CREATE INDEX IF NOT EXISTS idx_models_instance_path ON models(instance_id, rel_path);
CREATE INDEX IF NOT EXISTS idx_images_instance_path ON images(instance_id, rel_path);
