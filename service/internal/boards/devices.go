package boards

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/OpenIPC/website/service/internal/vendorfw"
)

// Ready is the tag of a board OpenIPC runs on: cctvsp's OpenIPC section says
// so, or coupler publishes an image for one of its device IDs.
const Ready = "openipc-ready"

// devices fills each model's XM device IDs with what they can be flashed
// with (vendorfw), and marks a model OpenIPC-ready when coupler has an image
// for any of them. The mark is derived here on every read, never stored, so
// it follows coupler's releases: an image withdrawn in a push withdraws it.
func devices(ctx context.Context, tx pgx.Tx, byModel map[string]*modelJSON) error {
	ids := make([]string, 0, len(byModel))
	for id, m := range byModel {
		ids = append(ids, id)
		m.Devices = []*vendorfw.Device{}
	}
	rows, err := tx.Query(ctx, `SELECT model_id, device_id FROM (`+vendorfw.BoardDevicesSQL+`) x WHERE model_id = ANY($1) ORDER BY model_id, device_id`, ids)
	if err != nil {
		return err
	}
	perModel := map[string][]string{}
	var all []string
	for rows.Next() {
		var model, dev string
		if err := rows.Scan(&model, &dev); err != nil {
			rows.Close()
			return err
		}
		perModel[model] = append(perModel[model], dev)
		if !slices.Contains(all, dev) {
			all = append(all, dev)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	found, err := vendorfw.ForDevices(ctx, tx, all)
	if err != nil {
		return err
	}
	for model, devs := range perModel {
		m := byModel[model]
		for _, dev := range devs {
			d := found[dev]
			if d == nil {
				d = &vendorfw.Device{ID: dev, Stock: []vendorfw.Firmware{}}
			}
			m.Devices = append(m.Devices, d)
			if d.Coupler != nil && !slices.Contains(m.Tags, Ready) {
				m.Tags = append(m.Tags, Ready)
				slices.Sort(m.Tags)
			}
		}
	}
	return nil
}
