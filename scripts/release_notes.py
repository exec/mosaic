#!/usr/bin/env python3
"""Build published notes from the exact version's reviewed changelog entry."""
from pathlib import Path
import sys


def release_notes(changelog: str, tag: str) -> str:
    version = tag.removeprefix("v")
    lines = changelog.splitlines()
    start = next((i for i, line in enumerate(lines) if line.startswith(f"## {version} — ")), None)
    if start is None:
        raise ValueError(f"No changelog entry for {tag}")
    end = next((i for i in range(start + 1, len(lines)) if lines[i].startswith("## ")), len(lines))
    body = "\n".join(lines[start + 1:end]).strip()
    if not body:
        raise ValueError(f"Empty changelog entry for {tag}")
    return f"""# Mosaic v{version}

{body}

## Downloads

- **macOS:** universal DMG; universal tarball for the in-app updater.
- **Linux desktop:** DEB, RPM, and AppImage.
- **Linux headless daemon:** DEB, RPM, and tarball.
- **Windows:** installer and portable EXE.

See `SHA256SUMS` for checksum verification. Desktop clients with the in-app updater can check via
Settings → Updates → Check now; package-managed installs use their package manager.
"""


if __name__ == "__main__":
    print(release_notes(Path("CHANGELOG.md").read_text(), sys.argv[1]), end="")
