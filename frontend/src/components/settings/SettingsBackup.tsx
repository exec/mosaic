import {createSignal, Show} from 'solid-js';
import {api, type SettingsBackupDTO} from '../../lib/bindings';
import {downloadBlob} from '../../lib/download';
import {userErr} from '../../lib/errors';
import {Button} from '../ui/Button';

export function SettingsBackup(){
 const [draft,setDraft]=createSignal<SettingsBackupDTO|null>(null),[busy,setBusy]=createSignal(false),[message,setMessage]=createSignal(''),[error,setError]=createSignal('');
 const run=async(action:()=>Promise<void>)=>{if(busy())return;setBusy(true);setError('');setMessage('');try{await action();}catch(e){setError(userErr(e));}finally{setBusy(false);}};
 return <details class="mt-8 border-t border-white/[.05] pt-4 text-xs"><summary class="cursor-pointer font-medium text-zinc-300">Settings backup & import</summary>
  <p class="mt-2 leading-relaxed text-zinc-500">Includes bandwidth, queue, peer connection, and seeding defaults. Credentials, web access, paths, RSS, categories, and browser appearance are excluded. Import replaces these defaults. Listening, DHT, encryption, and NAT changes require restarting Mosaic.</p>
  <div class="mt-3 flex flex-wrap items-center gap-3"><Button type="button" class="text-xs" disabled={busy()} onClick={()=>run(async()=>{const backup=await api.exportSettings();downloadBlob(new Blob([JSON.stringify(backup,null,2)],{type:'application/json'}),'mosaic-settings.json');})}>Export settings</Button>
   <label class="text-zinc-400">Choose backup <input aria-label="Settings backup file" type="file" accept=".json,application/json" class="mt-1 block max-w-full text-xs" disabled={busy()} onChange={e=>{const file=e.currentTarget.files?.[0];e.currentTarget.value='';setDraft(null);if(file)void run(async()=>{if(file.size>65536)throw new Error('Settings backup must be smaller than 64 KB');const value=JSON.parse(await file.text());if(value.version!==1||!value.limits||!value.queue||!value.peers||!value.seeding)throw new Error('Unsupported or incomplete Mosaic settings backup');setDraft(value);});}}/></label>
  </div>
  <Show when={draft()}>{backup=><div class="mt-3 rounded border border-white/[.06] p-3 text-zinc-400"><p>Ready to replace bandwidth, queue, peer, and seeding defaults.</p><pre class="mt-2 max-h-40 overflow-auto text-[10px]">{JSON.stringify(backup(),null,2)}</pre><div class="mt-3 flex gap-2"><Button type="button" class="text-xs" disabled={busy()} onClick={()=>run(async()=>{await api.importSettings(backup());setDraft(null);setMessage('Settings imported. Reload the interface to read the saved defaults; restart Mosaic for connection changes.');})}>Import settings</Button><Button type="button" variant="ghost" class="text-xs" disabled={busy()} onClick={()=>setDraft(null)}>Cancel</Button></div></div>}</Show>
  <Show when={error()}><p role="alert" class="mt-2 text-rose-300">{error()}</p></Show><Show when={message()}><p role="status" class="mt-2 text-accent-300">{message()} <button type="button" class="underline" onClick={()=>window.location.reload()}>Reload interface</button></p></Show>
 </details>;
}
