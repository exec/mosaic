package platform

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
)

func TestMacBundleDeclaresTorrentType(t *testing.T) {
	for _, name := range []string{"Info.plist", "Info.dev.plist"} {
		t.Run(name, func(t *testing.T) {
			tmpl, err := template.ParseFiles(filepath.Join("..", "..", "build", "darwin", name))
			require.NoError(t, err)
			var rendered bytes.Buffer
			require.NoError(t, tmpl.Execute(&rendered, map[string]any{
				"Name": "mosaic", "OutputFilename": "mosaic",
				"Info": map[string]any{"ProductName": "Mosaic", "ProductVersion": "1.8.0", "Comments": "", "Copyright": ""},
			}))
			path := filepath.Join(t.TempDir(), "Info.plist")
			require.NoError(t, os.WriteFile(path, rendered.Bytes(), 0o600))
			data, err := exec.Command("plutil", "-convert", "json", "-o", "-", path).CombinedOutput()
			require.NoError(t, err, "%s", data)
			var info struct {
				Imports []struct {
					Identifier string         `json:"UTTypeIdentifier"`
					Conforms   []string       `json:"UTTypeConformsTo"`
					Tags       map[string]any `json:"UTTypeTagSpecification"`
				} `json:"UTImportedTypeDeclarations"`
				Documents []struct {
					Types []string `json:"LSItemContentTypes"`
				} `json:"CFBundleDocumentTypes"`
			}
			require.NoError(t, json.Unmarshal(data, &info))
			require.Len(t, info.Imports, 1)
			require.Equal(t, "org.bittorrent.torrent", info.Imports[0].Identifier)
			require.Contains(t, info.Imports[0].Conforms, "public.data")
			require.Equal(t, []any{"torrent"}, info.Imports[0].Tags["public.filename-extension"])
			require.Equal(t, "application/x-bittorrent", info.Imports[0].Tags["public.mime-type"])
			require.Len(t, info.Documents, 1)
			require.Contains(t, info.Documents[0].Types, info.Imports[0].Identifier)
		})
	}
}
