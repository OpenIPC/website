-- SMTSEC (Shenzhen Shimaite, 视迈特): its IP camera modules from its site
-- (tools/board-donors/smtsec_snapshot.py). Its source row is written by the
-- import, as every donor's is.

ALTER TYPE board_source ADD VALUE IF NOT EXISTS 'smtsec';
