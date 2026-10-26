package persistence

import "context"

func (t *Torrents) FilePriorities(ctx context.Context, id string) (map[int]int, error) {
	rows, err := t.db.SQL().QueryContext(ctx, `SELECT file_index,priority FROM torrent_file_priorities WHERE infohash=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]int{}
	for rows.Next() {
		var i, p int
		if err = rows.Scan(&i, &p); err != nil {
			return nil, err
		}
		out[i] = p
	}
	return out, rows.Err()
}
func (t *Torrents) SetFilePriorities(ctx context.Context, id string, prios map[int]int) error {
	tx, err := t.db.SQL().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, p := range prios {
		if _, err = tx.ExecContext(ctx, `INSERT INTO torrent_file_priorities(infohash,file_index,priority) VALUES(?,?,?) ON CONFLICT(infohash,file_index) DO UPDATE SET priority=excluded.priority`, id, i, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (t *Torrents) SetSavePath(ctx context.Context, id, path string) error {
	_, err := t.db.SQL().ExecContext(ctx, `UPDATE torrents SET save_path=? WHERE infohash=?`, path, id)
	return err
}
