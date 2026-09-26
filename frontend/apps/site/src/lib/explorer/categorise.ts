// Heuristic mapping from Buildroot package name to a coarse category, used
// to colour the treemap. The grouping mirrors what a human would call out
// when looking at the top of the leaderboard — vendor SDK / kernel /
// userspace stack / toolchain runtime / overlay-or-post-build bucket.

export type Category =
  | "vendor"
  | "kernel"
  | "userspace"
  | "toolchain"
  | "overlay"
  | "other";

export const CATEGORY_LABEL: Record<Category, string> = {
  vendor: "Vendor SDK / drivers",
  kernel: "Kernel + in-tree modules",
  userspace: "Userspace",
  toolchain: "Toolchain runtime",
  overlay: "Overlay / post-build",
  other: "Other",
};

// The site's palette: brand blue for the vendor SDK that usually dominates,
// then colours that stay apart from it and from each other on white.
export const CATEGORY_COLOUR: Record<Category, string> = {
  vendor: "#4c60d8",
  kernel: "#7a5af5",
  userspace: "#2f9e8f",
  toolchain: "#b26b00",
  overlay: "#d92839",
  other: "#8a93a3",
};

// Package-name → category. First matching prefix/substring wins.
const RULES: Array<[RegExp, Category]> = [
  // Vendor SDKs and OSDRV ports for each SoC family.
  [/^hisilicon-(opensdk|osdrv|gpio-i2c)/, "vendor"],
  [/^sigmastar-/, "vendor"],
  [/^goke-/, "vendor"],
  [/^anyka-/, "vendor"],
  [/^xiongmai-/, "vendor"],
  [/^ingenic-/, "vendor"],
  [/^rockchip-/, "vendor"],
  [/^grainmedia-/, "vendor"],
  [/^fullhan-/, "vendor"],
  [/^ti-/, "vendor"],
  [/^ambarella-/, "vendor"],
  [/^novatek-/, "vendor"],
  [/^allwinner-/, "vendor"],

  // Kernel + kernel modules + firmware blobs.
  [/^linux($|-)/, "kernel"],
  [/^wireguard-linux/, "kernel"],

  // Toolchain runtime (libc, libstdc++, gcc-runtime).
  [/^toolchain-external/, "toolchain"],
  [/^(uclibc|musl|glibc)-compat/, "toolchain"],

  // Synthetic bucket emitted by size_report.py for overlay/post-build paths
  // that aren't in packages-file-list.txt.
  [/^_overlay_or_post_build$/, "overlay"],
  [/^skeleton-/, "overlay"],
];

export function categorise(pkg: string): Category {
  for (const [re, cat] of RULES) {
    if (re.test(pkg)) return cat;
  }
  return "userspace";
}
