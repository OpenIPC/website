-- The year a board first appeared in its maker's own catalogue, so the
-- gallery can open on current hardware rather than on 2014's (#321 follow-up).
--
-- It is a fact a source states, not an estimate: for Xiongmai, the earliest
-- upload date in the paths of the product's pictures on xiongmaitech.com
-- (/upload/2016/09/18/...). That site dates from 2015, so 2015 means "2015 or
-- earlier". A board no source dates stays NULL; the site places it by its SoC.
-- Two sources dating one board keep the earlier year.

ALTER TABLE board_models ADD COLUMN listed_year smallint CHECK (listed_year BETWEEN 2000 AND 2100);
