package persistence

import (
	"context"
	"time"
)

type TransferDay struct {
	Day        string `json:"day"`
	Uploaded   int64  `json:"uploaded"`
	Downloaded int64  `json:"downloaded"`
}

func (t *Torrents) TransferDays(ctx context.Context, userID int, all bool) ([]TransferDay, error) {
	rows, err := t.db.SQL().QueryContext(ctx, `SELECT d.day,SUM(d.uploaded),SUM(d.downloaded) FROM transfer_daily d
 WHERE d.day>=? AND (? OR EXISTS(SELECT 1 FROM torrent_access a WHERE a.infohash=d.infohash AND a.user_id=?))
 GROUP BY d.day ORDER BY d.day`, time.Now().UTC().AddDate(0, 0, -89).Format("2006-01-02"), all, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TransferDay{}
	for rows.Next() {
		var d TransferDay
		if err = rows.Scan(&d.Day, &d.Uploaded, &d.Downloaded); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (t *Torrents) PruneTransferDays(ctx context.Context) error {
	_, err := t.db.SQL().ExecContext(ctx, `DELETE FROM transfer_daily WHERE day<?`, time.Now().UTC().AddDate(0, 0, -89).Format("2006-01-02"))
	return err
}
