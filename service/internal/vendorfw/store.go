package vendorfw

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Save replaces the source's rows with the push, in one transaction: the
// table then holds what the project last published, and a re-push is a no-op.
func Save(ctx context.Context, db *pgxpool.Pool, p *Payload, by string) (int, error) {
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM vendor_firmware WHERE source = $1`, p.Source); err != nil {
			return err
		}
		rows := make([][]any, len(p.Items))
		for i, it := range p.Items {
			var sha, soc, org, orgURL, dev, dtype, app, cat, module, coll *string
			var variant any
			str := func(v string) *string {
				if v == "" {
					return nil
				}
				return &v
			}
			dev, dtype, app, cat, module, coll = str(it.DeviceID), str(it.DeviceType), str(it.App), str(it.Category), str(it.Module), str(it.Collection)
			if len(it.Variant) > 0 {
				variant = it.Variant
			}
			if it.SHA256 != "" {
				sha = &it.SHA256
			}
			if it.SoC != "" {
				soc = &it.SoC
			}
			if it.Origin != "" {
				org = &it.Origin
			}
			if it.OriginURL != "" {
				orgURL = &it.OriginURL
			}
			var size *int64
			if it.Size > 0 {
				size = &it.Size
			}
			rows[i] = []any{p.Source, it.Key, it.Version, dev, it.Build, it.AssetURL, sha, size, it.PublishedAt, soc, org, orgURL, by, dtype, app, cat, module, variant, coll}
		}
		if len(rows) == 0 {
			return nil
		}
		_, err := tx.CopyFrom(ctx, pgx.Identifier{"vendor_firmware"},
			[]string{"source", "key", "version", "device_id", "build", "asset_url", "sha256", "size", "published_at", "soc", "origin", "origin_url", "pushed_by", "device_type", "app", "category", "module", "variant", "collection"},
			pgx.CopyFromRows(rows))
		return err
	})
	if err != nil {
		return 0, err
	}
	return len(p.Items), nil
}
