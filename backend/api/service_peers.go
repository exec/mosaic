package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"

	"mosaic/backend/persistence"
)

const settingBlockedPeers = "blocked_peers"

func (s *Service) ListBlockedPeers(ctx context.Context) ([]string, error) {
	if err := requireAuth(CallerFrom(ctx)); err != nil {
		return nil, err
	}
	if !CallerFrom(ctx).CanChangeSettings() {
		return nil, ErrForbidden
	}
	s.blockedPeersMu.Lock()
	defer s.blockedPeersMu.Unlock()
	return s.loadBlockedPeers(ctx)
}

func (s *Service) loadBlockedPeers(ctx context.Context) ([]string, error) {
	ips := []string{}
	raw, err := s.settings.Get(ctx, settingBlockedPeers)
	if errors.Is(err, persistence.ErrNotFound) {
		return ips, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &ips); err != nil {
		return nil, err
	}
	return ips, nil
}

// SetPeerBlocked persists a global IP block, then disconnects matching peers.
func (s *Service) SetPeerBlocked(ctx context.Context, address string, blocked bool) error {
	if err := requireAuth(CallerFrom(ctx)); err != nil {
		return err
	}
	if !CallerFrom(ctx).CanChangeSettings() {
		return ErrForbidden
	}
	ip := net.ParseIP(address)
	if ip == nil {
		return fmt.Errorf("invalid peer IP address")
	}
	address = ip.String()
	s.blockedPeersMu.Lock()
	defer s.blockedPeersMu.Unlock()
	ips, err := s.loadBlockedPeers(ctx)
	if err != nil {
		return err
	}
	if blocked {
		if !slices.Contains(ips, address) {
			ips = append(ips, address)
		}
	} else {
		ips = slices.DeleteFunc(ips, func(v string) bool { return v == address })
	}
	slices.Sort(ips)
	raw, err := json.Marshal(ips)
	if err != nil {
		return err
	}
	if err := s.settings.Set(ctx, settingBlockedPeers, string(raw)); err != nil {
		return err
	}
	s.engine.SetBlockedPeers(ips)
	return nil
}
