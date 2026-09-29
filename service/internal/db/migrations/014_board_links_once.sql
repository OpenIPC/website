-- The reviewed "same family" links (aliases.yml) were written by every
-- source's import under that source's name, so a board carried one copy per
-- source -- five once Anjoy Vision's archive was imported. relate() now
-- leaves a pair to the source that wrote it first; this drops the copies
-- already stored, keeping the earliest source's (board_sources.position).
DELETE FROM board_links l
USING (
    SELECT l2.model_id, l2.source, l2.position,
           row_number() OVER (PARTITION BY l2.model_id, l2.target_model_id
                              ORDER BY s.position, l2.source, l2.position) AS n
    FROM board_links l2 JOIN board_sources s ON s.id = l2.source
    WHERE l2.kind = 'related' AND l2.target_model_id IS NOT NULL
) d
WHERE d.n > 1 AND l.model_id = d.model_id AND l.source = d.source AND l.position = d.position;
