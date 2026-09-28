-- Finished devices beside bare boards (cameras, recorders, doorbells, Wi-Fi
-- base stations), from any maker: JFTech, Xiongmai's continuing brand, sells
-- both, and a finished device is often a known board in a housing. Every
-- existing row is a board.

ALTER TYPE board_source ADD VALUE IF NOT EXISTS 'jftech';

ALTER TABLE board_models ADD COLUMN kind text NOT NULL DEFAULT 'board'
    CHECK (kind IN ('board', 'camera', 'recorder', 'doorbell', 'base_station'));
