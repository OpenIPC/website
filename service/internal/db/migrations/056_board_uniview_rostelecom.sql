-- Two Uniview cameras Rostelecom sells under its own names, put in order on
-- the owner's word (#618). The IPC8232SWC-WE is a finished camera, not a
-- board. C1L-2WN-G is the IPC8232SWC-WE-B under Uniview's name, and so is
-- C1L-2WN-G-RU, what the stock firmware read from that camera calls itself
-- (r-3dirfnxp, manuinfo DEVICE_NAME): its markings lead to one board. The
-- reports on uniview-c1l-2wn-g are moved by `openipc reports link`/`unlink`
-- -- nothing else may write those tables -- and the empty board goes then.
UPDATE board_models SET kind = 'camera' WHERE id = 'uniview-ipc8232swc-we';

UPDATE board_model_aliases SET model_id = 'uniview-ipc8232swc-we-b'
WHERE maker_id = 'uniview' AND code_norm IN ('C1L-2WN-G', 'C1L-2WN-G-RU')
  AND EXISTS (SELECT 1 FROM board_models WHERE id = 'uniview-ipc8232swc-we-b');

INSERT INTO board_model_texts (model_id, source, locale, field, text, translated_from)
SELECT t.model_id, 'contributor', t.locale, 'description', t.text, t.translated_from
FROM (VALUES
    ('uniview-ipc8232swc-we', 'ru', NULL,
     E'Под маркой Ростелеком эта камера продаётся как IPC8232SWC-WE, а сам Uniview продаёт её как C1L-2WN. По железу это одна и та же камера: Hi3516EV300 и сенсор OV2735 (в стоковой прошивке его драйвер называется SP2305).\nНе путать с IPC8232SWC-WE-B (C1L-2WN-G): она на SSC335DE.'),
    ('uniview-ipc8232swc-we', 'en', 'ru',
     E'Rostelecom sells this camera as the IPC8232SWC-WE; Uniview sells it under its own name as the C1L-2WN. The hardware is the same: a Hi3516EV300 and an OV2735 sensor (the stock firmware''s driver calls it SP2305).\nNot the IPC8232SWC-WE-B (C1L-2WN-G), which is built on the SSC335DE.'),
    ('uniview-ipc8232swc-we', 'zh', 'ru',
     E'俄罗斯电信（Rostelecom）以 IPC8232SWC-WE 的型号销售这款摄像机，Uniview 自己则以 C1L-2WN 的型号销售。两者硬件完全相同：Hi3516EV300 和 OV2735 传感器（原厂固件中的驱动名为 SP2305）。\n请勿与 IPC8232SWC-WE-B（C1L-2WN-G）混淆，后者采用 SSC335DE。'),
    ('uniview-ipc8232swc-we-b', 'ru', NULL,
     E'Под маркой Ростелеком эта камера продаётся как IPC8232SWC-WE-B, а сам Uniview продаёт её как C1L-2WN-G. По железу это одна и та же камера: SSC335DE и сенсор OS02G10.\nНе путать с IPC8232SWC-WE (C1L-2WN): она на Hi3516EV300.'),
    ('uniview-ipc8232swc-we-b', 'en', 'ru',
     E'Rostelecom sells this camera as the IPC8232SWC-WE-B; Uniview sells it under its own name as the C1L-2WN-G. The hardware is the same: an SSC335DE and an OS02G10 sensor.\nNot the IPC8232SWC-WE (C1L-2WN), which is built on the Hi3516EV300.'),
    ('uniview-ipc8232swc-we-b', 'zh', 'ru',
     E'俄罗斯电信（Rostelecom）以 IPC8232SWC-WE-B 的型号销售这款摄像机，Uniview 自己则以 C1L-2WN-G 的型号销售。两者硬件完全相同：SSC335DE 和 OS02G10 传感器。\n请勿与 IPC8232SWC-WE（C1L-2WN）混淆，后者采用 Hi3516EV300。')
) AS t (model_id, locale, translated_from, text)
JOIN board_models m ON m.id = t.model_id
WHERE EXISTS (SELECT 1 FROM board_sources WHERE id = 'contributor')
ON CONFLICT (model_id, source, locale, field) DO NOTHING;
