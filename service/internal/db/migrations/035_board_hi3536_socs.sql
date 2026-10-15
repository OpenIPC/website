-- Xiongmai's catalogue writes its NVR SoCs short: Hi3536C, Hi3536D, and
-- Hi3536E for the Hi3536DV100 variant on its KL-V2 boards (firmware#1796).
-- socShorthand now resolves them; this gives the boards imported before it
-- the SoC a re-import would, so they link to their SoC page and its
-- firmware. Only rows without a SoC are touched.
UPDATE board_models SET soc = CASE lower(replace(soc_label, ' ', ''))
        WHEN 'hi3536c' THEN 'hi3536cv100'
        ELSE 'hi3536dv100'
    END
WHERE soc IS NULL
  AND lower(replace(soc_label, ' ', '')) IN ('hi3536c', 'hi3536d', 'hi3536e');
