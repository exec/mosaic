import {createEffect, createSignal, lazy, Match, onCleanup, onMount, Show, Suspense, Switch} from 'solid-js';
import {api} from '../../lib/bindings';
import {toast} from 'solid-sonner';
import {userErr} from '../../lib/errors';
import type {DetailDTO, InspectorTab, SeedingDefaultsDTO} from '../../lib/bindings';
import type {BandwidthRing} from '../../lib/ringbuffer';
import {InspectorHeader} from './InspectorHeader';
import {InspectorTabs} from './InspectorTabs';
import {OverviewTab} from './OverviewTab';
import {FilesTab} from './FilesTab';
import {PeersTab} from './PeersTab';
import {TrackersTab} from './TrackersTab';

// SpeedTab pulls in uPlot (~45KB) and is only reached when the user opens
// the Speed tab — lazy-load it so the chart bundle stays off the hot path.
const SpeedTab = lazy(() => import('./SpeedTab').then((m) => ({default: m.SpeedTab})));

type Props = {
  open: boolean;
  canBlockPeers: boolean;
  canEdit: boolean;
  seedingDefaults?: SeedingDefaultsDTO;
  canRelocate?: boolean;
  detail: DetailDTO | null;
  // Live download rate from the matching Torrent in the list — see header.
  downloadRate: number;
  tab: InspectorTab;
  bandwidthRing: BandwidthRing;
  bandwidthTick: number;
  sequential: boolean;
  onTabChange: (t: InspectorTab) => void;
  onClose: () => void;
  onSetFilePriority: (index: number, priority: 'skip' | 'normal' | 'high' | 'max') => void;
  onToggleSequential: () => void;
};

export function Inspector(props: Props) {
  const [viewport,setViewport]=createSignal(window.innerWidth);
  const [width,setWidth]=createSignal((()=>{try{const value=Number(localStorage.getItem('mosaic.inspector-width'));return value>=320&&value<=720?value:420;}catch{return 420;}})());
  const compact=()=>viewport()<1100;
  const actualWidth=()=>Math.min(width(),compact()?Math.max(280,viewport()-48):Math.max(320,viewport()-608));
  let aside:HTMLElement|undefined,restoreFocus:HTMLElement|null=null,stopResize=()=>{};
  const remember=(value:number)=>{setWidth(Math.max(320,Math.min(720,value)));try{localStorage.setItem('mosaic.inspector-width',String(width()));}catch{}};
  onMount(()=>{const resize=()=>setViewport(window.innerWidth);window.addEventListener('resize',resize);onCleanup(()=>window.removeEventListener('resize',resize));});
  onCleanup(()=>{stopResize();restoreFocus?.focus();});
  createEffect(()=>{
    const drawer=props.open&&compact();
    if(drawer){restoreFocus=document.activeElement as HTMLElement;queueMicrotask(()=>{if(props.open&&compact())aside?.querySelector<HTMLButtonElement>('[aria-label="Close inspector"]')?.focus();});}
    else if(restoreFocus){restoreFocus.focus();restoreFocus=null;}
  });
  const resize=(event:PointerEvent)=>{
    if(event.button!==0)return;event.preventDefault();const start=event.clientX,initial=actualWidth();
    const move=(e:PointerEvent)=>remember(initial+start-e.clientX);
    stopResize=()=>{document.removeEventListener('pointermove',move);document.removeEventListener('pointerup',stopResize);document.removeEventListener('pointercancel',stopResize);};
    document.addEventListener('pointermove',move);document.addEventListener('pointerup',stopResize);document.addEventListener('pointercancel',stopResize);
  };
  const drawerKeys=(event:KeyboardEvent)=>{
    if(!compact())return;
    if(event.key==='Escape'){event.preventDefault();event.stopPropagation();props.onClose();}
    if(event.key==='Tab'){
      const focusable=Array.from(aside?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),summary,[tabindex="0"]')??[]).filter(el=>el.getClientRects().length);
      const first=focusable[0],last=focusable.at(-1);
      if(event.shiftKey&&document.activeElement===first){event.preventDefault();last?.focus();}
      else if(!event.shiftKey&&document.activeElement===last){event.preventDefault();first?.focus();}
    }
  };
  return (
    <Show when={props.open}>
      {/* pt-7 keeps the header (title + close X) below the drag overlay at
          the top of the window. Background paints to the top edge so the
          inspector visually extends top-to-bottom like the side rails. */}
      <Show when={compact()}><button type="button" aria-label="Close inspector backdrop" tabIndex={-1} class="fixed inset-0 bottom-7 z-30 bg-black/45 backdrop-blur-sm" onClick={props.onClose}/></Show>
      <aside ref={aside} aria-label="Torrent inspector" role={compact()?'dialog':undefined} aria-modal={compact()?true:undefined} onKeyDown={drawerKeys}
        class="flex min-w-0 shrink-0 flex-col border-l border-white/[.04] backdrop-blur-sm animate-in fade-in pt-7"
        classList={{'relative h-full bg-white/[.01]':!compact(),'fixed right-0 top-0 bottom-7 z-40 bg-zinc-950 shadow-2xl':compact()}} style={{width:`${actualWidth()}px`}}>
        <Show when={!compact()}><div role="separator" aria-label="Resize torrent inspector" aria-orientation="vertical" aria-valuemin={320} aria-valuemax={Math.min(720,Math.max(320,viewport()-608))} aria-valuenow={actualWidth()} tabIndex={0} onPointerDown={resize} onDblClick={()=>remember(420)} onKeyDown={e=>{if(e.key==='ArrowLeft'||e.key==='ArrowRight'){e.preventDefault();e.stopPropagation();remember(actualWidth()+(e.key==='ArrowLeft'?20:-20));}else if(e.key==='Home'){e.preventDefault();remember(420);}}} class="absolute bottom-0 left-0 top-7 z-10 w-1.5 -translate-x-1/2 cursor-col-resize touch-none rounded hover:bg-accent-500/40 focus-visible:bg-accent-500/50 focus-visible:outline-none"/></Show>
        <InspectorHeader detail={props.detail} downloadRate={props.downloadRate} onClose={props.onClose} />
        <div class="border-b border-white/[.04] px-3 py-2">
          <InspectorTabs active={props.tab} onChange={props.onTabChange} />
        </div>
        <Show when={!props.canEdit}>
          <p class="border-b border-white/[.04] px-4 py-2 text-xs text-zinc-500">Read-only access · Only owners and editors can change this torrent.</p>
        </Show>
        <div class="min-h-0 min-w-0 flex-1 overflow-y-auto overflow-x-hidden">
          <Switch>
            <Match when={props.tab === 'overview'}>
              <OverviewTab
                seedingDefaults={props.seedingDefaults}
                downloadRate={props.downloadRate}
                canRelocate={props.canRelocate}
                canChangeSettings={props.canBlockPeers}
                canEdit={props.canEdit}
                detail={props.detail}
                sequential={props.sequential}
                onToggleSequential={props.onToggleSequential}
                onTabChange={props.onTabChange}
              />
            </Match>
            <Match when={props.tab === 'files'}>
              <FilesTab canEdit={props.canEdit} detail={props.detail} onSetPriority={props.onSetFilePriority} onSetPriorities={async (prios) => {if (!props.canEdit || !props.detail) return;try {await api.setFilePriorities(props.detail.id, prios);}catch(e){toast.error(`Couldn't update files — ${userErr(e)}`);}}} />
            </Match>
            <Match when={props.tab === 'peers'}>
              <PeersTab detail={props.detail} canBlock={props.canBlockPeers} />
            </Match>
            <Match when={props.tab === 'trackers'}>
              <TrackersTab canEdit={props.canEdit} detail={props.detail} />
            </Match>
            <Match when={props.tab === 'speed'}>
              <Suspense fallback={<div class="p-4 text-xs text-zinc-500">Loading chart…</div>}>
                <SpeedTab ring={props.bandwidthRing} tick={props.bandwidthTick} />
              </Suspense>
            </Match>
          </Switch>
        </div>
      </aside>
    </Show>
  );
}
