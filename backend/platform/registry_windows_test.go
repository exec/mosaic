//go:build windows

package platform

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows/registry"
)

func TestFileAssociationsPreserveOtherClientDefaults(t *testing.T) {
	path := fmt.Sprintf(`Software\MosaicAssociationTest-%d`, time.Now().UnixNano())
	root, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	require.NoError(t, err)
	t.Cleanup(func() {
		deleteRegistryTree(t, root)
		require.NoError(t, root.Close())
		require.NoError(t, registry.DeleteKey(registry.CURRENT_USER, path))
	})
	for _, name := range []string{`.torrent`, `magnet\shell\open\command`} {
		key, _, err := registry.CreateKey(root, `Software\Classes\`+name, registry.ALL_ACCESS)
		require.NoError(t, err)
		require.NoError(t, key.SetStringValue("", "other-client"))
		require.NoError(t, key.Close())
	}
	require.NoError(t, ensureFileAssociations(root, `C:\Program Files\Mosaic\mosaic.exe`))
	for _, name := range []string{`.torrent`, `magnet\shell\open\command`} {
		key, err := registry.OpenKey(root, `Software\Classes\`+name, registry.READ)
		require.NoError(t, err)
		value, _, err := key.GetStringValue("")
		require.NoError(t, err)
		require.Equal(t, "other-client", value)
		require.NoError(t, key.Close())
	}
	key, err := registry.OpenKey(root, `Software\Classes\MosaicTorrent\shell\open\command`, registry.READ)
	require.NoError(t, err)
	defer key.Close()
	value, _, err := key.GetStringValue("")
	require.NoError(t, err)
	require.Equal(t, `"C:\Program Files\Mosaic\mosaic.exe" "%1"`, value)
}

func deleteRegistryTree(t *testing.T, root registry.Key) {
	t.Helper()
	names, err := root.ReadSubKeyNames(-1)
	require.NoError(t, err)
	for _, name := range names {
		key, err := registry.OpenKey(root, name, registry.ALL_ACCESS)
		require.NoError(t, err)
		deleteRegistryTree(t, key)
		require.NoError(t, key.Close())
		require.NoError(t, registry.DeleteKey(root, name))
	}
}
