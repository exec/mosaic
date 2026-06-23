package persistence

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestUsers_DeleteNullsGrantedBy: deleting a user who granted access to others
// must not violate the torrent_access.granted_by FK (which has no ON DELETE
// action while foreign_keys is ON). The grantee's row must survive with
// granted_by set to NULL.
func TestUsers_DeleteNullsGrantedBy(t *testing.T) {
	db := newTestDB(t)
	users := NewUsers(db)
	access := NewTorrentAccess(db)
	tor := NewTorrents(db)
	ctx := context.Background()

	// torrent_access.infohash references torrents(infohash); create the torrent.
	require.NoError(t, tor.Save(ctx, TorrentRecord{
		InfoHash: "hash-a", Name: "t", SavePath: "/tmp", AddedAt: time.Now(),
	}))

	idA, err := users.Create(ctx, User{Username: "alice", Role: RoleUser})
	require.NoError(t, err)
	idB, err := users.Create(ctx, User{Username: "bob", Role: RoleUser})
	require.NoError(t, err)

	// A grants B access to the torrent.
	require.NoError(t, access.Grant(ctx, "hash-a", idB, AccessEditor, &idA))

	// Deleting A must succeed despite the granted_by reference.
	require.NoError(t, users.Delete(ctx, idA))

	// B's grant must remain, with granted_by now NULL.
	rows, err := access.ListForTorrent(ctx, "hash-a")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, idB, rows[0].UserID)
	require.Equal(t, AccessEditor, rows[0].Access)
	require.Nil(t, rows[0].GrantedBy, "granted_by should be NULL after the granter is deleted")
}
