package api

import (
	"context"

	"mosaic/backend/engine"
)

// TorrentAdder is the narrow surface that engine-side subsystems (RSSPoller,
// WatchFolder) need to add torrents through the Service. Extracting it as an
// interface — instead of taking *Service directly — breaks what would otherwise
// be a circular dependency: Service wants references to its sub-pollers (so
// e.g. PollFeedNow can route to the live RSSPoller), and the sub-pollers want
// to call back into Service to add torrents. The Service still implements this
// interface trivially because the methods already exist with these exact
// signatures.
//
// The practical wins:
//   - sub-pollers are testable in isolation against a tiny mock instead of a
//     fully-constructed Service
//   - the post-Attach* pattern survives, but for a different reason now: it
//     wires Service-to-poller (so Service can delegate PollFeedNow / refresh /
//     watch-folder restart), not poller-to-Service, which is the direction
//     that had the import-cycle smell
type TorrentAdder interface {
	AddMagnet(ctx context.Context, magnet, savePath string) (engine.TorrentID, error)
	AddTorrentBytes(ctx context.Context, blob []byte, savePath string) (engine.TorrentID, error)
	SetTorrentCategory(ctx context.Context, infohash string, categoryID *int) error
}
