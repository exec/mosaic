# Signing & Release Setup

This repo's `release.yml` workflow signs and notarizes release artifacts.
For signing to actually run, you must add the following GitHub Actions
**repository secrets** (Settings → Secrets and variables → Actions → New repository secret).

If a secret is missing, the corresponding signing step is **skipped** and
the workflow produces an unsigned artifact (still uploaded to the draft
release; you'll see a warning in the build log).

---

## macOS — Apple Developer ID + notarization

| Secret | What it is | How to get it |
|---|---|---|
| `APPLE_DEVELOPER_ID` | The certificate Common Name, e.g. `Developer ID Application: Dylan Hart (ABCDE12345)` | Visible in Keychain Access for the imported `.p12`. |
| `APPLE_ID` | Your Apple Developer account email | https://developer.apple.com |
| `APPLE_TEAM_ID` | 10-char team ID | https://developer.apple.com/account → Membership |
| `APPLE_APP_SPECIFIC_PASSWORD` | App-specific password (NOT your Apple ID password) | https://appleid.apple.com → App-Specific Passwords |

The `.p12` cert itself must also be imported into the runner's Keychain.
The simplest path: encode the `.p12` to base64 and add as
`APPLE_DEVELOPER_ID_CERT_P12_BASE64` + `APPLE_DEVELOPER_ID_CERT_PASSWORD`,
then add a workflow step that decodes + imports before `build-macos.sh`:

```yaml
- name: Import Apple cert
  if: secrets.APPLE_DEVELOPER_ID_CERT_P12_BASE64 != ''
  env:
    P12_B64: ${{ secrets.APPLE_DEVELOPER_ID_CERT_P12_BASE64 }}
    P12_PASS: ${{ secrets.APPLE_DEVELOPER_ID_CERT_PASSWORD }}
  run: |
    KEYCHAIN=mosaic-build.keychain
    P12=$RUNNER_TEMP/cert.p12
    echo "$P12_B64" | base64 --decode > $P12
    security create-keychain -p ci $KEYCHAIN
    security default-keychain -s $KEYCHAIN
    security unlock-keychain -p ci $KEYCHAIN
    security import $P12 -k $KEYCHAIN -P "$P12_PASS" -T /usr/bin/codesign
    security set-key-partition-list -S apple-tool:,apple: -s -k ci $KEYCHAIN
```

Add this step to `release.yml` before the `Build (macOS)` step.

---

## Windows — Azure Key Vault + AzureSignTool

| Secret | Source |
|---|---|
| `AZURE_KEY_VAULT_URI` | `https://<your-vault>.vault.azure.net` |
| `AZURE_KEY_VAULT_CERT_NAME` | The cert friendly name in the vault |
| `AZURE_TENANT_ID` | Service principal tenant |
| `AZURE_CLIENT_ID` | Service principal client ID |
| `AZURE_CLIENT_SECRET` | Service principal client secret |

The cert itself must be uploaded to Azure Key Vault as an OV (or EV)
code-signing cert. Spin up the SP via:

```bash
az ad sp create-for-rbac --name mosaic-signing --role Reader --scopes <vault-resource-id>
az keyvault set-policy --name <vault-name> --spn <sp-app-id> --certificate-permissions get --key-permissions sign
```

---

## Linux

No secrets required — `.deb`, `.rpm`, and `.AppImage` ship unsigned. Users
verify via `SHA256SUMS` (auto-generated, attached to every release).

---

## Cutting a release

1. Bump version in commit subject + tag: `git tag v0.8.0 && git push origin v0.8.0`
2. Watch the `Release` workflow. ~10-15 min for the matrix to complete.
3. Visit the GitHub Releases page → the draft release is waiting.
4. Click "Edit" → review notes → "Publish release."
5. Mosaic clients running v<0.8.0 will pick up the new release within 24h
   (or immediately via Settings → Updates → Check now).
