import {createEffect, createSignal, onMount} from 'solid-js';
import {toast} from 'solid-sonner';
import type {DesktopIntegrationDTO} from '../../lib/bindings';
import {api} from '../../lib/bindings';
import {isWailsRuntime} from '../../lib/runtime';
import {Button} from '../ui/Button';

type Props = {
  desktopIntegration: DesktopIntegrationDTO;
  onSetDesktopIntegration: (d: DesktopIntegrationDTO) => Promise<void>;
};

function PaneHeader(props: {title: string; subtitle?: string}) {
  return (
    <div class="mb-4 border-b border-white/[.04] pb-3">
      <h2 class="text-lg font-semibold text-zinc-100">{props.title}</h2>
      {props.subtitle && <p class="mt-0.5 text-sm text-zinc-500">{props.subtitle}</p>}
    </div>
  );
}

function Field(props: {label: string; help?: string; children: any; disabled?: boolean}) {
  return (
    <div
      class="grid grid-cols-[200px_1fr] items-start gap-4 py-3 border-b border-white/[.03]"
      classList={{'opacity-50': props.disabled}}
    >
      <div>
        <div class="text-sm text-zinc-200">{props.label}</div>
        {props.help && <div class="mt-0.5 text-xs text-zinc-500">{props.help}</div>}
      </div>
      <div>{props.children}</div>
    </div>
  );
}

function Checkbox(props: {checked: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean; testId?: string}) {
  return (
    <label
      class="inline-flex items-center gap-2 text-sm text-zinc-200"
      classList={{
        'cursor-pointer': !props.disabled,
        'cursor-not-allowed': props.disabled,
      }}
    >
      <input
        type="checkbox"
        checked={props.checked}
        disabled={props.disabled}
        onChange={(e) => props.onChange(e.currentTarget.checked)}
        class="accent-accent-500"
        data-testid={props.testId}
      />
      {props.label}
    </label>
  );
}

export function DesktopPane(props: Props) {
  const [trayEnabled, setTrayEnabled] = createSignal(props.desktopIntegration.tray_enabled);
  const [closeToTray, setCloseToTray] = createSignal(props.desktopIntegration.close_to_tray);
  const [startMinimized, setStartMinimized] = createSignal(props.desktopIntegration.start_minimized);
  const [notifyOnComplete, setNotifyOnComplete] = createSignal(props.desktopIntegration.notify_on_complete);
  const [notifyOnError, setNotifyOnError] = createSignal(props.desktopIntegration.notify_on_error);
  const [notifyOnUpdate, setNotifyOnUpdate] = createSignal(props.desktopIntegration.notify_on_update);
  const [platform, setPlatform] = createSignal('');

  // Re-sync when prop changes (initial fetch races).
  createEffect(() => { setTrayEnabled(props.desktopIntegration.tray_enabled); });
  createEffect(() => { setCloseToTray(props.desktopIntegration.close_to_tray); });
  createEffect(() => { setStartMinimized(props.desktopIntegration.start_minimized); });
  createEffect(() => { setNotifyOnComplete(props.desktopIntegration.notify_on_complete); });
  createEffect(() => { setNotifyOnError(props.desktopIntegration.notify_on_error); });
  createEffect(() => { setNotifyOnUpdate(props.desktopIntegration.notify_on_update); });

  // Detect platform — gracefully degrade if api.platform() is unavailable
  // (e.g. running outside Wails or on a build that doesn't expose it).
  onMount(async () => {
    if (!isWailsRuntime()) return;
    try {
      const p = await api.platform();
      setPlatform(p);
    } catch (err) {
      console.error('platform detection failed:', err);
    }
  });

  const isMac = () => platform() === 'darwin';

  const save = async () => {
    try {
      await props.onSetDesktopIntegration({
        tray_enabled: trayEnabled(),
        close_to_tray: closeToTray(),
        start_minimized: startMinimized(),
        notify_on_complete: notifyOnComplete(),
        notify_on_error: notifyOnError(),
        notify_on_update: notifyOnUpdate(),
      });
      toast.success('Desktop settings saved');
    } catch (e) {
      toast.error(String(e));
    }
  };

  return (
    <div class="mx-auto max-w-2xl px-6 py-6">
      <PaneHeader title="Desktop" subtitle="System tray, window behavior, and native notifications." />

      <div class="text-xs uppercase tracking-wider text-zinc-500 mt-2 mb-1 px-1">Tray icon</div>
      <Field label="Enable system tray icon" help="Show Mosaic in the menu bar / system tray. Requires restart.">
        <Checkbox
          checked={trayEnabled()}
          onChange={setTrayEnabled}
          label="Enabled"
          testId="desktop-tray-enabled"
        />
      </Field>
      <Field
        label="Close to tray"
        help={
          isMac()
            ? 'macOS handles this via the red close button by default.'
            : 'Clicking the close button minimizes Mosaic to the tray instead of quitting.'
        }
        disabled={isMac()}
      >
        <Checkbox
          checked={closeToTray()}
          onChange={setCloseToTray}
          label="Enabled"
          disabled={isMac()}
          testId="desktop-close-to-tray"
        />
      </Field>
      <Field label="Start minimized" help="Launch Mosaic hidden in the tray. Useful at login.">
        <Checkbox
          checked={startMinimized()}
          onChange={setStartMinimized}
          label="Enabled"
          testId="desktop-start-minimized"
        />
      </Field>

      <div class="text-xs uppercase tracking-wider text-zinc-500 mt-6 mb-1 px-1">Notifications</div>
      <Field label="Torrent complete" help="Native OS notification when a download finishes.">
        <Checkbox
          checked={notifyOnComplete()}
          onChange={setNotifyOnComplete}
          label="Enabled"
          testId="desktop-notify-complete"
        />
      </Field>
      <Field label="Torrent error" help="Notify when a torrent stalls or hits an unrecoverable error.">
        <Checkbox
          checked={notifyOnError()}
          onChange={setNotifyOnError}
          label="Enabled"
          testId="desktop-notify-error"
        />
      </Field>
      <Field label="Update installed" help="Notify after Mosaic auto-installs a new version.">
        <Checkbox
          checked={notifyOnUpdate()}
          onChange={setNotifyOnUpdate}
          label="Enabled"
          testId="desktop-notify-update"
        />
      </Field>

      <div class="flex justify-end mt-3">
        <Button variant="primary" onClick={save} data-testid="desktop-save">Save</Button>
      </div>
    </div>
  );
}
