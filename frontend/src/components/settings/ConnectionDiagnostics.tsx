import {createSignal, For, onCleanup, onMount, Show} from 'solid-js';
import {api,type ConnectionDiagnosticsDTO} from '../../lib/bindings';
import {fmtTimestamp} from '../../lib/format';
import {userErr} from '../../lib/errors';
import {Button} from '../ui/Button';

export function ConnectionDiagnostics(){
 const [data,setData]=createSignal<ConnectionDiagnosticsDTO|null>(null),[error,setError]=createSignal(''),[busy,setBusy]=createSignal(false),[sampled,setSampled]=createSignal(0);
 let disposed=false;
 const refresh=async()=>{if(busy())return;setBusy(true);try{const result=await api.getConnectionDiagnostics();if(!disposed){setData(result);setSampled(Date.now()/1000);setError('');}}catch(e){if(!disposed)setError(userErr(e));}finally{if(!disposed)setBusy(false);}};
 onMount(()=>{void refresh();const timer=setInterval(refresh,10000);onCleanup(()=>{disposed=true;clearInterval(timer);});});
 return <section class="mb-6 rounded-lg border border-white/[.06] bg-white/[.015] p-4 text-xs" aria-label="Live connection diagnostics">
  <div class="flex items-center justify-between gap-3"><h3 class="font-medium text-zinc-200">Live connection diagnostics</h3><Button type="button" class="text-xs" variant="ghost" disabled={busy()} onClick={refresh}>Refresh diagnostics</Button></div>
  <Show when={error()}><p role="alert" class="mt-2 text-rose-300">{error()} · Last readings may be out of date.</p></Show>
  <Show when={data()} fallback={<p class="mt-2 text-zinc-500">Loading diagnostics…</p>}>{d=><Show when={d().available} fallback={<p class="mt-2 text-zinc-500">Diagnostics unavailable for this engine.</p>}>
   <dl class="mt-3 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-zinc-400"><dt>Listening endpoints</dt><dd class="min-w-0 break-all font-mono"><For each={d().listeners} fallback="No listeners">{a=><div>{a}</div>}</For></dd>
    <dt>Connected peers</dt><dd>{d().inbound_peers} inbound · {d().outbound_peers} outbound</dd>
    <dt>DHT observed state</dt><dd>{!d().dht_servers?'Not running':`${d().dht_servers} servers · ${d().dht_nodes} known nodes · ${d().dht_good_nodes} good or unqueried`}</dd>
    <dt>DHT announce replies</dt><dd>{d().dht_announces} successful this session</dd>
   </dl><p class="mt-3 leading-relaxed text-zinc-500">These are live engine readings. An inbound peer confirms a connection reached Mosaic; zero inbound peers does not prove a closed port. DHT nodes include unqueried nodes. No external port test is performed.</p>
   <p class="mt-1 text-[10px] text-zinc-600">Updated {fmtTimestamp(sampled())} · saved connection changes may require a restart</p>
  </Show>}</Show>
 </section>;
}
