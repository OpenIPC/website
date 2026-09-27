-- tehno32.ru's archive of Xiongmai board documentation (interface
-- descriptions with connector drawings and pinouts, parameter sheets, board
-- outlines), and the PCB a module is built on.
--
-- A PCB is a card of its own, named by its silkscreen (BLK530WX1-0235P-38X38-S),
-- as OpenHisiIpCam's BLK16CV-S is: one PCB carries several modules that differ
-- by sensor, lens or firmware. "pcb" links a module to its PCB, "on_pcb" a PCB
-- to each module built on it.

ALTER TYPE board_source ADD VALUE IF NOT EXISTS 'tehno32';

ALTER TABLE board_links DROP CONSTRAINT board_links_kind_check;
ALTER TABLE board_links ADD CONSTRAINT board_links_kind_check
    CHECK (kind IN ('stock_firmware', 'source_page', 'vendor_page', 'successor', 'predecessor', 'related', 'pcb', 'on_pcb'));
