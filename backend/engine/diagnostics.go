package engine

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/anacrolix/dht/v2"
	"github.com/anacrolix/torrent"
)

type ConnectionDiagnostics struct {
	Available     bool     `json:"available"`
	Listeners     []string `json:"listeners"`
	InboundPeers  int      `json:"inbound_peers"`
	OutboundPeers int      `json:"outbound_peers"`
	DHTServers    int      `json:"dht_servers"`
	DHTNodes      int      `json:"dht_nodes"`
	DHTGoodNodes  int      `json:"dht_good_nodes"`
	DHTAnnounces  int64    `json:"dht_announces"`
}

func (e *Engine) ConnectionDiagnostics() ConnectionDiagnostics {
	if p, ok := e.backend.(interface{ ConnectionDiagnostics() ConnectionDiagnostics }); ok {
		return p.ConnectionDiagnostics()
	}
	return ConnectionDiagnostics{Listeners: []string{}}
}
func (a *AnacrolixBackend) ConnectionDiagnostics() ConnectionDiagnostics {
	out := ConnectionDiagnostics{Available: true, Listeners: []string{}}
	for _, addr := range a.client.ListenAddrs() {
		out.Listeners = append(out.Listeners, fmt.Sprintf("%s · %s", addr.Network(), addr.String()))
	}
	sort.Strings(out.Listeners)
	for _, t := range a.client.Torrents() {
		for _, pc := range t.PeerConns() {
			if pc.Discovery == torrent.PeerSourceIncoming {
				out.InboundPeers++
			} else {
				out.OutboundPeers++
			}
		}
	}
	servers := a.client.DhtServers()
	out.DHTServers = len(servers)
	for _, server := range servers {
		if stats, ok := server.Stats().(dht.ServerStats); ok {
			out.DHTNodes += stats.Nodes
			out.DHTGoodNodes += stats.GoodNodes
			out.DHTAnnounces += stats.SuccessfulOutboundAnnouncePeerQueries
		}
	}
	return out
}
func (e *Engine) ExportTorrent(id TorrentID) ([]byte, error) {
	if p, ok := e.backend.(interface {
		ExportTorrent(TorrentID) ([]byte, error)
	}); ok {
		return p.ExportTorrent(id)
	}
	return nil, errors.New("torrent metadata is unavailable")
}
func (a *AnacrolixBackend) ExportTorrent(id TorrentID) ([]byte, error) {
	t, ok := a.find(id)
	if !ok {
		return nil, errors.New("not found")
	}
	select {
	case <-t.GotInfo():
	default:
		return nil, errors.New("torrent metadata is unavailable")
	}
	mi := t.Metainfo()
	var buf bytes.Buffer
	if err := mi.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
