import {createEffect, createMemo, createSignal, on, Show} from 'solid-js';
import {api, type SeedPolicyDTO, type SeedingDefaultsDTO} from '../../lib/bindings';
import {toast} from 'solid-sonner';
import {userErr} from '../../lib/errors';
import {Button} from '../ui/Button';

export function policyDescription(policy: {ratio_limit?: number | null; time_min_limit?: number | null}) {
  const limits: string[] = [];
  if (policy.ratio_limit != null) limits.push(`ratio ${policy.ratio_limit}`);
  if (policy.time_min_limit != null) limits.push(`${policy.time_min_limit} minutes seeding`);
  return limits.length ? `Stop at ${limits.join(' or ')}` : 'No seeding limit';
}
export function SeedPolicySection(props: {infohash: string; canEdit: boolean; defaults?: SeedingDefaultsDTO}) {
  const [policy, setPolicy] = createSignal<SeedPolicyDTO | null>(null);
  const [mode, setMode] = createSignal<'global' | 'unlimited' | 'custom'>('global');
  const [ratioEnabled, setRatioEnabled] = createSignal(false), [ratio, setRatio] = createSignal('');
  const [timeEnabled, setTimeEnabled] = createSignal(false), [minutes, setMinutes] = createSignal('');
  const [busy, setBusy] = createSignal(false), [error, setError] = createSignal('');
  const id = createMemo(() => props.infohash);
  const load = async (hash: string) => {
    setPolicy(null);setError('');
    try {
      const p = await api.getTorrentSeedPolicy(hash);
      if (hash !== props.infohash) return;
      setPolicy(p);setMode(p.use_global ? 'global' : p.ratio_limit == null && p.time_min_limit == null ? 'unlimited' : 'custom');
      setRatioEnabled(p.ratio_limit != null);setRatio(p.ratio_limit == null ? '' : String(p.ratio_limit));
      setTimeEnabled(p.time_min_limit != null);setMinutes(p.time_min_limit == null ? '' : String(p.time_min_limit));
    } catch(e) {if (hash === props.infohash) setError(userErr(e));}
  };
  createEffect(on(id, load));
  const summary = () => {
    const p = policy();if (!p) return 'Loading policy…';
    return p.use_global ? `Global default · ${policyDescription(props.defaults ?? {ratio_limit: p.effective_ratio_limit, time_min_limit: p.effective_time_min_limit})}` : policyDescription(p);
  };
  const save = async () => {
    if (!props.canEdit || busy() || !policy()) return;
    const hash = props.infohash;
    const r = ratioEnabled() ? Number(ratio()) : null, m = timeEnabled() ? Number(minutes()) : null;
    if (mode() === 'custom' && ((!ratioEnabled() && !timeEnabled()) || r !== null && (ratio().trim() === '' || !Number.isFinite(r) || r < 0) || m !== null && (minutes().trim() === '' || !Number.isSafeInteger(m) || m < 1))) {
      setError('Choose at least one limit. Ratio must be non-negative; time must be a whole number of minutes greater than zero.');return;
    }
    const p: SeedPolicyDTO = {use_global: mode() === 'global', ratio_limit: mode() === 'custom' ? r : null, time_min_limit: mode() === 'custom' ? m : null};
    setBusy(true);setError('');
    try {await api.setTorrentSeedPolicy(hash, p);if (hash === props.infohash) {await load(hash);toast.success('Seeding policy saved');}}
    catch(e) {if (hash === props.infohash) setError(userErr(e));}
    finally {setBusy(false);}
  };
  const input = 'w-20 rounded border border-white/[.06] bg-black/30 px-2 py-1 text-xs text-zinc-200 focus:outline-none focus:ring-1 focus:ring-accent-500';
  return <details class="my-3 border-t border-white/[.05] pt-3">
    <summary class="cursor-pointer text-xs font-medium text-zinc-300">Seeding policy</summary>
    <p class="mt-2 text-xs text-accent-300" aria-label="Effective seeding policy">{summary()}</p>
    <p class="mt-1 text-[10px] leading-relaxed text-zinc-500">Set this before completion. Seeding time starts when the torrent finishes; the first limit reached pauses it.</p>
    <fieldset disabled={!props.canEdit || busy() || !policy()} class="mt-3 min-w-0 space-y-2 text-xs text-zinc-300">
      <label class="flex items-center gap-2"><input type="radio" name="seed-mode" value="global" checked={mode() === 'global'} onChange={() => setMode('global')} class="accent-accent-500" />Use global defaults</label>
      <label class="flex items-center gap-2"><input type="radio" name="seed-mode" value="unlimited" checked={mode() === 'unlimited'} onChange={() => setMode('unlimited')} class="accent-accent-500" />No limit</label>
      <label class="flex items-center gap-2"><input type="radio" name="seed-mode" value="custom" checked={mode() === 'custom'} onChange={() => setMode('custom')} class="accent-accent-500" />Custom</label>
      <Show when={mode() === 'custom'}>
        <label class="flex items-center gap-2"><input type="checkbox" checked={ratioEnabled()} onChange={(e) => setRatioEnabled(e.currentTarget.checked)} class="accent-accent-500" />Stop at ratio <input aria-label="Seeding ratio limit" type="number" min="0" step="0.1" disabled={!ratioEnabled()} class={input} value={ratio()} onInput={(e) => setRatio(e.currentTarget.value)} /></label>
        <label class="flex flex-wrap items-center gap-2"><input type="checkbox" checked={timeEnabled()} onChange={(e) => setTimeEnabled(e.currentTarget.checked)} class="accent-accent-500" />Stop after <input aria-label="Seeding time limit" type="number" min="1" step="1" disabled={!timeEnabled()} class={input} value={minutes()} onInput={(e) => setMinutes(e.currentTarget.value)} />minutes</label>
      </Show>
      <Show when={props.canEdit}><Button type="button" class="text-xs" onClick={save}>{busy() ? 'Saving…' : 'Save seeding policy'}</Button></Show>
    </fieldset>
    <Show when={error()}><p role="alert" class="mt-2 text-xs text-rose-300">{error()}</p><Show when={!policy()}><button type="button" class="mt-1 text-xs text-accent-300" onClick={() => void load(props.infohash)}>Retry</button></Show></Show>
  </details>;
}
