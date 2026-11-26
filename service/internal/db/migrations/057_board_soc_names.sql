-- SoCs written another way than the catalogue names them: SigmaStar's parts
-- under MStar's prefix (MSC335), MStar's under its name (MStar 313E), and a
-- part after its maker's name (Ambarella S2L). socKey and socShorthand now
-- resolve them; this gives the boards stored before it the SoC, those made
-- from owners' reports too, which no import revisits. Only rows without a
-- SoC are touched, and soc_label stays as its source wrote it.
UPDATE board_models SET soc = CASE lower(replace(soc_label, ' ', ''))
        WHEN 'msc335' THEN 'ssc335'
        WHEN 'msc30kq' THEN 'ssc30kq'
        WHEN 'mstar313e' THEN 'msc313e'
        WHEN 'mstar316dm' THEN 'msc316dm'
        WHEN 'ambarellas2l' THEN 's2l'
        WHEN 'fullhanfh8852v200' THEN 'fh8852v200'
        WHEN 'xiongmaixm530' THEN 'xm530'
    END
WHERE soc IS NULL
  AND lower(replace(soc_label, ' ', '')) IN ('msc335', 'msc30kq', 'mstar313e', 'mstar316dm',
      'ambarellas2l', 'fullhanfh8852v200', 'xiongmaixm530');
