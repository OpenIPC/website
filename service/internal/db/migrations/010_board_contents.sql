-- What is inside a finished device: the board a camera,
-- recorder, doorbell or base station is built on.
--
-- A row is 'likely' when the evidence is the vendor's own firmware: its
-- landing page names the board (a recorder's C638024T（AHB80N04R-GS-V3）),
-- or its build names the module (a camera's IPC_GK7205V200_G4F_S38). It is
-- 'confirmed' when an owner opened the device and sent a photo of the board,
-- reviewed and recorded in service/internal/boards/contents.yml.
--
-- board_code is the code as the evidence names it, normalised like an alias;
-- it is joined to a catalogue board through board_model_aliases when read,
-- so a board the catalogue gains later links itself.

-- Confirmed rows are the source 'owners', which the web role creates when
-- contents.yml has a line (and removes when it has none), so the catalogue's
-- list of sources names it only once it has said something.

CREATE TABLE board_contents (
    model_id   text NOT NULL REFERENCES board_models (id) ON DELETE CASCADE,
    board_code text NOT NULL CHECK (board_code ~ '^[A-Z0-9][A-Z0-9-]*$'),
    status     text NOT NULL CHECK (status IN ('likely', 'confirmed')),
    basis      text NOT NULL CHECK (basis IN ('firmware_page', 'firmware_build', 'owner')),
    -- The page that says so: the vendor's firmware page, or the owner's issue.
    evidence   text NOT NULL,
    -- What that page shows, quoted: the firmware file's name.
    evidence_label text,
    source     text NOT NULL REFERENCES board_sources (id),
    PRIMARY KEY (model_id, board_code, source)
);
CREATE INDEX board_contents_code ON board_contents (board_code);
