-- The catalogue names HiSilicon's parts as HiSilicon does. Hi3516CV610's
-- variants were filed under made-up memory names (hi3516cv610-ddr2-64m,
-- -ddr3-128m, -ddr3-512m) and are -10B, -20S and -00S; Hi3519DV500 was two
-- entries for HiSilicon's demo boards (DMEB, DMEBPRO) and is one, with
-- Hi3516DV500 beside it. Boards on the old names take the new ones, and
-- those whose source names Hi3519DV500 or Hi3516DV500, left without a SoC
-- while the catalogue had neither, take theirs. soc_label stays as its
-- source wrote it.
UPDATE board_models SET soc = CASE soc
        WHEN 'hi3516cv610-ddr2-64m' THEN 'hi3516cv610-10b'
        WHEN 'hi3516cv610-ddr3-128m' THEN 'hi3516cv610-20s'
        WHEN 'hi3516cv610-ddr3-512m' THEN 'hi3516cv610-00s'
        WHEN 'hi3519dv500-dmeb' THEN 'hi3519dv500'
        WHEN 'hi3519dv500-dmebpro' THEN 'hi3519dv500'
    END
WHERE soc IN ('hi3516cv610-ddr2-64m', 'hi3516cv610-ddr3-128m', 'hi3516cv610-ddr3-512m',
    'hi3519dv500-dmeb', 'hi3519dv500-dmebpro');

UPDATE board_models SET soc = lower(replace(soc_label, ' ', ''))
WHERE soc IS NULL
  AND lower(replace(soc_label, ' ', '')) IN ('hi3519dv500', 'hi3516dv500');
