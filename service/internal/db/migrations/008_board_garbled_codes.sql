-- The tehno32 import of #324 made one card from a file name whose mangled
-- Chinese suffix sat in parentheses (NBD8016S-ULAzhCGeDJe(R)ldNo0.docx ->
-- NBD8016S-ULAZHCGEDJE). The builder now cuts such suffixes, and the next
-- tehno32 import files those documents under NBD8016S-ULA. This removes a
-- card made only of such a code and only of tehno32's units; a card any other
-- source contributed to is left alone. Its files under BOARDS_ROOT are
-- removed by hand (the unit directory xiongmai-nbd8016s-ulazhcgedje-tehno32).

DELETE FROM board_models m
WHERE m.model ~ '(ZHCGEDJ|ZHYOU|JIEKOU|PJEU|LDNO)'
  AND NOT EXISTS (SELECT 1 FROM board_units u WHERE u.model_id = m.id AND u.source <> 'tehno32');
