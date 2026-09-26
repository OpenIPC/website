# This file should contain all the record creation needed to seed the database with its default values.
# The data can then be loaded with the bin/rails db:seed command (or created alongside the database with db:setup).
#
# Examples:
#
#   movies = Movie.create([{ name: "Star Wars" }, { name: "Lord of the Rings" }])
#   Character.create(name: "Luke", movie: movies.first)

Vendor.delete_all
Vendor.create([
                { id: 1, name: 'Ambarella', full_name: 'Ambarella, Inc.', website_url: 'https://www.ambarella.com/' },
                { id: 2, name: 'Anyka', full_name: 'Guangzhou Anyka Microelectronics Co., Ltd.', website_url: 'http://www.anyka.com/' },
                { id: 3, name: 'Fullhan', full_name: 'Shanghai Fullhan Microelectonics Co., Ltd.', website_url: 'https://www.fullhan.com/' },
                { id: 4, name: 'Goke', full_name: 'Hunan Goke Microelectronics Co., Ltd.', website_url: 'http://www.goke.com/' },
                { id: 5, name: 'Grain Media', full_name: 'Grain Media Ltd.', website_url: 'https://www.grainmedia.co.uk/' },
                { id: 6, name: 'HiSilicon', full_name: 'HiSilicon (Shanghai) Technologies Co., Ltd.', website_url: 'https://www.hisilicon.com/' },
                { id: 7, name: 'Ingenic', full_name: 'Ingenic Semiconductor Co.,Ltd.', website_url: 'http://www.ingenic.com.cn/' },
                { id: 8, name: 'MStar', full_name: 'MStar Semiconductor, Inc', website_url: 'https://www.mstarsemi.com/' },
                { id: 9, name: 'Novatek', full_name: 'Novatek Microelectronics Corporation', website_url: 'https://www.novatek.com.tw/' },
                { id: 10, name: 'SigmaStar', full_name: 'SigmaStar Technology Ltd.', website_url: 'http://www.sigmastarsemi.com/' },
                { id: 11, name: 'Xiongmai', full_name: 'Hangzhou Xiongmai Technology Co.,Ltd.', website_url: 'https://www.xiongmaitech.com/' },
                { id: 12, name: 'AltaSens', full_name: 'AltaSens, Inc.', website_url: 'http://www.altasens.com/' },
                { id: 13, name: 'Aptina', full_name: 'Aptina Imaging Corporation', website_url: 'https://www.onsemi.com/' },
                { id: 14, name: 'Himax', full_name: 'Himax Technologies, Inc.', website_url: 'https://www.himax.com.tw/' },
                { id: 15, name: 'OmniVision', full_name: 'OmniVision Technologies Inc.', website_url: 'https://www.ovt.com/' },
                { id: 16, name: 'Panasonic', full_name: 'Panasonic Industry Co., Ltd.', website_url: 'https://www.panasonic.com/' },
                { id: 17, name: 'Pixelplus', full_name: 'Pixelplus Co., Ltd.', website_url: 'http://www.pixelplus.com/' },
                { id: 18, name: 'SmartSens', full_name: 'SmartSens Technology (Shanghai) Co., Ltd.', website_url: 'https://www.smartsenstech.com/' },
                { id: 19, name: 'SOI', full_name: 'Silicon Optronics, Inc', website_url: 'https://www.soinc.com.tw/' },
                { id: 20, name: 'Sony', full_name: 'Sony Semiconductor Solutions', website_url: 'https://www.sony-semicon.co.jp/' },
                { id: 21, name: 'Nuvoton', full_name: 'Nuvoton Technology Corporation Japan', website_url: 'https://www.nuvoton.com/' },
                { id: 22, name: 'Glaxycore', full_name: 'Galaxycore Inc', website_url: 'https://www.gcoreinc.com/' },
              ])

Soc.delete_all
Soc.create([
             { vendor_id: 1, model: 'S2L', status: 'rnd', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 1, model: 'S3L', status: 'wip', load_address: '', uboot_filename: '', linux_filename: 'openipc.ambarella-s3l-br.tgz' },
             { vendor_id: 2, model: 'AK3916EV300', status: 'neq', load_address: '', uboot_filename: '', linux_filename: 'openipc.ak3916ev300-br.tgz' },
             { vendor_id: 2, model: 'AK3916EV301', status: 'rnd', load_address: '', uboot_filename: '', linux_filename: 'openipc.ak3918ev200-br.tgz' },
             { vendor_id: 2, model: 'AK3918EV200', status: 'hlp', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 2, model: 'AK3918EV300', status: 'neq', load_address: '', uboot_filename: '', linux_filename: 'openipc.ak3918ev300-br.tgz' },
             { vendor_id: 2, model: 'AK3918EV330', status: 'rnd', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 3, model: 'FH8626V100', status: 'hlp', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 3, model: 'FH8632V100', status: 'neq', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 3, model: 'FH8652V100', status: 'hlp', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 3, model: 'FH8852V100', status: 'wip', load_address: '', uboot_filename: '', linux_filename: 'openipc.fh8852v100-br.tgz' },
             { vendor_id: 3, model: 'FH8852V200', status: 'wip', load_address: '0xA1000000', uboot_filename: '', linux_filename: 'openipc.fh8852v200-br.tgz' },
             { vendor_id: 3, model: 'FH8852V210', status: 'neq', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 3, model: 'FH8856V100', status: 'neq', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 3, model: 'FH8856V200', status: 'wip', load_address: '', uboot_filename: '', linux_filename: 'openipc.fh8856v200-br.tgz' },
             { vendor_id: 3, model: 'FH8856V210', status: 'neq', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 3, model: 'FH8858V200', status: 'neq', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 3, model: 'FH8858V210', status: 'neq', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 4, model: 'GK7102S', status: 'rnd', load_address: '0xC1000000', uboot_filename: '', linux_filename: '' },
             { vendor_id: 4, model: 'GK7202V300', status: 'done', load_address: '0x42000000', uboot_filename: 'u-boot-gk7202v300-universal.bin', linux_filename: 'openipc.gk7202v300-br.tgz' },
             { vendor_id: 4, model: 'GK7205V200', status: 'done', load_address: '0x42000000', uboot_filename: 'u-boot-gk7205v200-universal.bin', linux_filename: 'openipc.gk7205v200-br.tgz' },
             { vendor_id: 4, model: 'GK7205V210', status: 'done', load_address: '0x42000000', uboot_filename: '', linux_filename: 'openipc.gk7205v210-br.tgz' },
             { vendor_id: 4, model: 'GK7205V300', status: 'done', load_address: '0x42000000', uboot_filename: 'u-boot-gk7205v300-universal.bin', linux_filename: 'openipc.gk7205v300-br.tgz' },
             { vendor_id: 4, model: 'GK7605V100', status: 'done', load_address: '0x42000000', uboot_filename: 'u-boot-gk7605v100-universal.bin', linux_filename: 'openipc.gk7605v100-br.tgz' },
             { vendor_id: 5, model: 'GM8135', status: 'neq', load_address: '', uboot_filename: '', linux_filename: '' },
             { vendor_id: 5, model: 'GM8136', status: 'mvp', load_address: '', uboot_filename: '', linux_filename: 'openipc.gm8136-br.tgz' },
             { vendor_id: 6, model: 'HI3516AV100', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3516av100-universal.bin', linux_filename: 'openipc.hi3516av100-br.tgz' },
             { vendor_id: 6, model: 'HI3516AV200', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3516av200-universal.bin', linux_filename: 'openipc.hi3516av200-br.tgz' },
             { vendor_id: 6, model: 'HI3516AV300', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3516av300-universal.bin', linux_filename: 'openipc.hi3516av300-br.tgz' },
             { vendor_id: 6, model: 'HI3516CV100', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3516cv100-universal.bin', linux_filename: 'openipc.hi3516cv100-br.tgz' },
             { vendor_id: 6, model: 'HI3516CV200', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3516cv200-universal.bin', linux_filename: 'openipc.hi3516cv200-br.tgz' },
             { vendor_id: 6, model: 'HI3516CV300', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3516cv300-universal.bin', linux_filename: 'openipc.hi3516cv300-br.tgz' },
             { vendor_id: 6, model: 'HI3516CV500', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3516cv500-universal.bin', linux_filename: 'openipc.hi3516cv500-br.tgz' },
             { vendor_id: 6, model: 'HI3516DV100', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3516dv100-universal.bin', linux_filename: 'openipc.hi3516dv100-br.tgz' },
             { vendor_id: 6, model: 'HI3516DV200', status: 'done', load_address: '0x42000000', uboot_filename: 'u-boot-hi3516dv200-universal.bin', linux_filename: 'openipc.hi3516dv200-br.tgz' },
             { vendor_id: 6, model: 'HI3516DV300', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3516dv300-universal.bin', linux_filename: 'openipc.hi3516dv300-br.tgz' },
             { vendor_id: 6, model: 'HI3516EV100', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3516ev100-universal.bin', linux_filename: 'openipc.hi3516ev100-br.tgz' },
             { vendor_id: 6, model: 'HI3516EV200', status: 'done', load_address: '0x42000000', uboot_filename: 'u-boot-hi3516ev200-universal.bin', linux_filename: 'openipc.hi3516ev200-br.tgz' },
             { vendor_id: 6, model: 'HI3516EV300', status: 'done', load_address: '0x42000000', uboot_filename: 'u-boot-hi3516ev300-universal.bin', linux_filename: 'openipc.hi3516ev300-br.tgz' },
             { vendor_id: 6, model: 'HI3518CV100', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3518cv100-universal.bin', linux_filename: 'openipc.hi3518cv100-br.tgz' },
             { vendor_id: 6, model: 'HI3518EV100', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3518ev100-universal.bin', linux_filename: 'openipc.hi3518ev100-br.tgz' },
             { vendor_id: 6, model: 'HI3518EV200', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3518ev200-universal.bin', linux_filename: 'openipc.hi3518ev200-br.tgz' },
             { vendor_id: 6, model: 'HI3518EV201', status: 'done', load_address: '0x82000000', uboot_filename: '', linux_filename: '' },
             { vendor_id: 6, model: 'HI3518EV300', status: 'done', load_address: '0x42000000', uboot_filename: 'u-boot-hi3518ev300-universal.bin', linux_filename: 'openipc.hi3518ev300-br.tgz' },
             { vendor_id: 6, model: 'HI3519V101', status: 'done', load_address: '0x82000000', uboot_filename: 'u-boot-hi3519v101-universal.bin', linux_filename: 'openipc.hi3519v101-br.tgz' },
             { vendor_id: 6, model: 'HI3520DV100', status: '', load_address: '0x82000000', uboot_filename: '', linux_filename: '' },
             { vendor_id: 6, model: 'HI3520DV200', status: '', load_address: '0x82000000', uboot_filename: '', linux_filename: '' },
             { vendor_id: 7, model: 'T10', status: 'mvp', load_address: '0x80600000', uboot_filename: 'u-boot-t10-universal.bin', linux_filename: 'openipc.t10-br.tgz' },
             { vendor_id: 7, model: 'T20', status: 'mvp', load_address: '0x80600000', uboot_filename: 'u-boot-t20-universal.bin', linux_filename: 'openipc.t20-br.tgz' },
             { vendor_id: 7, model: 'T21', status: 'mvp', load_address: '0x80600000', uboot_filename: '', linux_filename: 'openipc.t21-br.tgz' },
             { vendor_id: 7, model: 'T31', status: 'mvp', load_address: '0x80600000', uboot_filename: '', linux_filename: 'openipc.t31-br.tgz' },
             { vendor_id: 8, model: 'MSC313E', status: 'wip', load_address: '0x21000000', uboot_filename: 'u-boot-msc313e-universal.bin', linux_filename: 'openipc.msc313e-br.tgz' },
             { vendor_id: 8, model: 'MSC316DC', status: 'wip', load_address: '0x21000000', uboot_filename: '', linux_filename: 'openipc.msc316dc-br.tgz' },
             { vendor_id: 8, model: 'MSC316DM', status: 'wip', load_address: '0x21000000', uboot_filename: '', linux_filename: 'openipc.msc316dm-br.tgz' },
             { vendor_id: 9, model: 'NT98562', status: 'wip', load_address: '', uboot_filename: '', linux_filename: 'openipc.nt98562-br.tgz' },
             { vendor_id: 9, model: 'NT98566', status: 'wip', load_address: '', uboot_filename: '', linux_filename: 'openipc.nt98566-br.tgz' },
             { vendor_id: 10, model: 'SSC325', status: 'rnd', load_address: '', uboot_filename: '', linux_filename: 'openipc.ssc325-br.tgz' },
             { vendor_id: 10, model: 'SSC335', status: 'mvp', load_address: '0x21000000', uboot_filename: '', linux_filename: 'openipc.ssc335-br.tgz' },
             { vendor_id: 10, model: 'SSC335DE', status: 'rnd', load_address: '0x21000000', uboot_filename: '', linux_filename: 'openipc.ssc335de-br.tgz' },
             { vendor_id: 10, model: 'SSC337', status: 'mvp', load_address: '0x21000000', uboot_filename: '', linux_filename: 'openipc.ssc337-br.tgz' },
             { vendor_id: 10, model: 'SSC337DE', status: 'rnd', load_address: '0x21000000', uboot_filename: '', linux_filename: 'openipc.ssc337de-br.tgz' },
             { vendor_id: 11, model: 'XM510', status: 'mvp', load_address: '', uboot_filename: '', linux_filename: 'openipc.xm510-br.tgz' },
             { vendor_id: 11, model: 'XM530', status: 'mvp', load_address: '', uboot_filename: '', linux_filename: 'openipc.xm530-br.tgz' },
             { vendor_id: 11, model: 'XM550', status: 'mvp', load_address: '', uboot_filename: '', linux_filename: 'openipc.xm550-br.tgz' },
           ])

# Segments are a classification of the rows above rather than a column in them,
# and this is the same call the migration makes. Without it a schema-loaded
# setup -- `db:prepare` on a fresh checkout, then `db:seed` -- never runs the
# migration's backfill, so every chip comes out unclassified and a `done` Goke
# part gets the generic business line instead of the CCTV one (#190).
Soc.classify_segments!
