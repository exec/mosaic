import {createEffect} from 'solid-js';
import {toast} from 'solid-sonner';
import type {UpdateInfoDTO} from '../../lib/bindings';

type Props = {
  info: UpdateInfoDTO | null;
  onInstall: () => void;
  onDismiss: () => void;
};

// Module-scoped dedupe so the same `latest_version` only toasts once across
// the session even if multiple WS frames re-deliver the same envelope.
// Exposed via __resetUpdateToastDedupeForTesting for tests.
let lastShownFor = '';

// Test-only escape hatch.
export function __resetUpdateToastDedupeForTesting() {
  lastShownFor = '';
}

export function UpdateToast(props: Props) {
  createEffect(() => {
    const info = props.info;
    if (!info?.available) return;
    if (info.latest_version === lastShownFor) return;
    lastShownFor = info.latest_version;
    toast(`Update available — ${info.latest_version}`, {
      duration: 30_000,
      action: {label: 'Install', onClick: () => props.onInstall()},
      cancel: {label: 'Later', onClick: () => props.onDismiss()},
    });
  });
  return null;
}
