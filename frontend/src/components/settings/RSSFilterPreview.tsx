import {createMemo, createSignal, For, Show} from 'solid-js';
import {api, type FilterDTO, type FilterPreviewDTO} from '../../lib/bindings';
import {userErr} from '../../lib/errors';
import {Button} from '../ui/Button';

export function RSSFilterPreview(props:{filter:FilterDTO}) {
 const key=createMemo(()=>JSON.stringify(props.filter));
 const [result,setResult]=createSignal<FilterPreviewDTO|null>(null),[tested,setTested]=createSignal('');
 const [busy,setBusy]=createSignal(false),[error,setError]=createSignal('');
 const [onlyMatches,setOnlyMatches]=createSignal(true);
 const current=()=>tested()===key();
 const preview=async()=>{
  const request=key();setBusy(true);setError('');setResult(null);setTested(request);
  try{const data=await api.previewRSSFilter(props.filter);if(request===key())setResult(data);}
  catch(e){if(request===key())setError(userErr(e));}finally{setBusy(false);}
 };
 return <section class="mt-2 rounded border border-white/[.05] p-3 text-xs" aria-label="RSS filter preview">
  <div class="flex flex-wrap items-center gap-2"><Button type="button" class="text-xs" disabled={busy()||!props.filter.regex.trim()} onClick={preview}>{busy()?'Testing…':'Test against feed items'}</Button><span class="text-zinc-500">Preview only · nothing is added</span></div>
  <Show when={error()&&current()}><p role="alert" class="mt-2 text-rose-300">{error()}</p></Show>
  <Show when={current()?result():null}>{(data)=><>
   <p class="mt-3 text-zinc-300">{data().items.filter(i=>i.matches).length} of {data().items.length} titles match · {props.filter.enabled?'Rule enabled':'Rule disabled'}</p>
   <p class="mt-1 break-all text-zinc-500">Destination: {data().destination}. Category is assigned after adding; the first matching enabled rule wins.</p>
   <label class="mt-2 flex items-center gap-2 text-zinc-400"><input type="checkbox" checked={onlyMatches()} onChange={e=>setOnlyMatches(e.currentTarget.checked)}/>Show matches only</label>
   <ul class="mt-2 max-h-56 overflow-auto divide-y divide-white/[.04]">
    <For each={data().items.filter(i=>!onlyMatches()||i.matches)} fallback={<li class="py-2 text-zinc-500">No matching items.</li>}>{item=><li class="py-2"><div class="break-words text-zinc-300">{item.title}</div><div class="mt-1 text-zinc-500">{!item.matches?'No match':item.already_handled?'Already handled by RSS':item.in_library?'Already in library':!item.guid?'No item identifier':!item.torrent_url?'No torrent source':props.filter.enabled?'Would add':'Would add if enabled'}{item.pub_date?` · ${item.pub_date}`:''}</div></li>}</For>
   </ul>
  </>}</Show>
  <Show when={tested()&&!current()}><p class="mt-2 text-amber-300/80">Filter changed. Test again to update the preview.</p></Show>
 </section>;
}
