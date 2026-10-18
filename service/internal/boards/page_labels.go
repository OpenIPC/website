package boards

import (
	"fmt"
	"strings"
)

// labels are the words a board's page writes around the board, in the
// page's language. The island's own are data/locales/boards.*.yml; these are
// the few the written-out page needs, and the title and description.
type labels struct {
	Maker, SoC, Sensor, Flash, AlsoMarked, DeviceIDs, Listed string
	Photos, Files, Links, Features, Specs, AllBoards          string
	Unidentified, MB                                           string
	HavePhotos, HavePinout, HaveDump, HaveUBoot, HaveBootLog   string
	Kinds                                                      map[string]string
	FileKinds                                                  map[string]string

	// title is the page's title before " - OpenIPC": who (maker and code),
	// the SoC when known, and what it is.
	title func(who, soc, kind string) string
	// description says what the page has, for a board whose sources wrote
	// nothing in the page's language.
	description func(who, soc string, have []string) string
	photoAlt    func(what, board string) string
}

func labelsFor(locale string) labels {
	switch locale {
	case "ru":
		return ruLabels
	case "zh":
		return zhLabels
	}
	return enLabels
}

// list joins with commas and a last "and".
func list(xs []string, and string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + and + xs[len(xs)-1]
}

var enLabels = labels{
	Maker: "Maker", SoC: "SoC", Sensor: "Sensor", Flash: "Flash", AlsoMarked: "Also marked",
	DeviceIDs: "Device IDs", Listed: "Listed", Photos: "Photos", Files: "Files", Links: "Links",
	Features: "Features", Specs: "Specifications", AllBoards: "All camera boards",
	Unidentified: "Unidentified board", MB: "MB",
	HavePhotos: "photos", HavePinout: "the UART pinout", HaveDump: "a factory flash dump",
	HaveUBoot: "the U-Boot console", HaveBootLog: "a boot log",
	Kinds: map[string]string{"board": "board", "camera": "camera", "recorder": "recorder",
		"doorbell": "doorbell", "base_station": "base station"},
	FileKinds: map[string]string{"photo_front": "front", "photo_back": "back", "photo_other": "photo",
		"pinout": "pinout", "flash_dump": "flash dump", "uboot_env": "U-Boot console", "boot_log": "boot log",
		"document": "document", "note": "note", "firmware": "firmware"},
	title: func(who, soc, kind string) string {
		if soc == "" {
			return fmt.Sprintf("%s %s", who, kind)
		}
		return fmt.Sprintf("%s: %s %s", who, soc, kind)
	},
	description: func(who, soc string, have []string) string {
		s := who
		if soc != "" {
			s += " on " + soc
		}
		if len(have) > 0 {
			return fmt.Sprintf("%s: %s from real units, and how to install OpenIPC on it.", s, list(have, " and "))
		}
		return s + ": what is known about it, and how to install OpenIPC on it."
	},
	photoAlt: func(what, board string) string { return what + " of " + board },
}

var ruLabels = labels{
	Maker: "Производитель", SoC: "SoC", Sensor: "Сенсор", Flash: "Флеш-память", AlsoMarked: "Другие маркировки",
	DeviceIDs: "ID устройства", Listed: "В каталоге с", Photos: "Фото", Files: "Файлы", Links: "Ссылки",
	Features: "Особенности", Specs: "Характеристики", AllBoards: "Все платы камер",
	Unidentified: "Неопознанная плата", MB: "МБ",
	HavePhotos: "фото", HavePinout: "распиновка UART", HaveDump: "заводской дамп флеш-памяти",
	HaveUBoot: "консоль U-Boot", HaveBootLog: "лог загрузки",
	Kinds: map[string]string{"board": "плата", "camera": "камера", "recorder": "регистратор",
		"doorbell": "дверной звонок", "base_station": "базовая станция"},
	FileKinds: map[string]string{"photo_front": "лицевая", "photo_back": "обратная", "photo_other": "фото",
		"pinout": "распиновка", "flash_dump": "дамп флеш-памяти", "uboot_env": "консоль U-Boot", "boot_log": "лог загрузки",
		"document": "документ", "note": "заметка", "firmware": "прошивка"},
	title: func(who, soc, kind string) string {
		if soc == "" {
			return fmt.Sprintf("%s: %s", who, kind)
		}
		return fmt.Sprintf("%s: %s на %s", who, kind, soc)
	},
	description: func(who, soc string, have []string) string {
		s := who
		if soc != "" {
			s += " на " + soc
		}
		if len(have) > 0 {
			return fmt.Sprintf("%s: %s с реальных экземпляров и как установить на неё OpenIPC.", s, list(have, " и "))
		}
		return s + ": что о ней известно и как установить на неё OpenIPC."
	},
	photoAlt: func(what, board string) string { return what + ": " + board },
}

var zhLabels = labels{
	Maker: "厂商", SoC: "SoC", Sensor: "传感器", Flash: "闪存", AlsoMarked: "其他标记",
	DeviceIDs: "设备 ID", Listed: "收录年份", Photos: "照片", Files: "文件", Links: "链接",
	Features: "特性", Specs: "规格", AllBoards: "全部摄像机主板",
	Unidentified: "未识别的主板", MB: "MB",
	HavePhotos: "实物照片", HavePinout: "UART 引脚图", HaveDump: "出厂闪存转储",
	HaveUBoot: "U-Boot 控制台", HaveBootLog: "启动日志",
	Kinds: map[string]string{"board": "主板", "camera": "摄像机", "recorder": "录像机",
		"doorbell": "门铃", "base_station": "基站"},
	FileKinds: map[string]string{"photo_front": "正面", "photo_back": "背面", "photo_other": "照片",
		"pinout": "引脚图", "flash_dump": "闪存转储", "uboot_env": "U-Boot 控制台", "boot_log": "启动日志",
		"document": "文档", "note": "备注", "firmware": "固件"},
	title: func(who, soc, kind string) string {
		if soc == "" {
			return fmt.Sprintf("%s %s", who, kind)
		}
		return fmt.Sprintf("%s：%s %s", who, soc, kind)
	},
	description: func(who, soc string, have []string) string {
		s := who
		if soc != "" {
			s += "（" + soc + "）"
		}
		if len(have) > 0 {
			return fmt.Sprintf("%s：来自实物的%s，以及如何安装 OpenIPC。", s, list(have, "和"))
		}
		return s + "：已知信息，以及如何安装 OpenIPC。"
	},
	photoAlt: func(what, board string) string { return board + "：" + what },
}
