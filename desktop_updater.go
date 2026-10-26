package main

import (
	"context"
	"mosaic/backend/api"
	"mosaic/backend/updater"
)

type desktopUpdater struct {
	updater *updater.Updater
	version string
}

func (d desktopUpdater) Check(ctx context.Context) (api.UpdateInfoDTO, error) {
	info, err := d.updater.Check(ctx)
	return updateInfoDTO(info, d.version), err
}

func (d desktopUpdater) Install(ctx context.Context) (string, error) {
	info := d.updater.Last()
	return info.LatestVersion, d.updater.Install(ctx, info)
}

func updateInfoDTO(info updater.Info, version string) api.UpdateInfoDTO {
	return api.UpdateInfoDTO{
		Available: info.Available, LatestVersion: info.LatestVersion,
		AssetURL: info.AssetURL, AssetFilename: info.AssetFilename,
		CheckedAt: info.CheckedAt.Unix(), CurrentVersion: version,
	}
}
