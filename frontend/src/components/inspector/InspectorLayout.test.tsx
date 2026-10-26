import {afterEach,expect,test,vi} from 'vitest';
import {createSignal} from 'solid-js';
import {render} from 'solid-js/web';
import {Inspector} from './Inspector';
import {BandwidthRing} from '../../lib/ringbuffer';
vi.mock('./OverviewTab',()=>({OverviewTab:()=>null}));
let dispose:()=>void;
afterEach(()=>{dispose?.();document.body.innerHTML='';localStorage.clear();vi.unstubAllGlobals();});
const props={canBlockPeers:false,canEdit:false,detail:null,downloadRate:0,tab:'overview' as const,bandwidthRing:new BandwidthRing(5),bandwidthTick:0,sequential:false,onTabChange:vi.fn(),onSetFilePriority:vi.fn(),onToggleSequential:vi.fn()};
test('resize keyboard controls persist width and compact drawer restores focus',async()=>{
 vi.stubGlobal('innerWidth',1400);
 const opener=document.createElement('button');document.body.append(opener);opener.focus();
 const [open,setOpen]=createSignal(true);dispose=render(()=><Inspector {...props} open={open()} onClose={()=>setOpen(false)}/>,document.body);
 const handle=document.querySelector('[role="separator"]')!;handle.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowLeft',bubbles:true}));
 expect(localStorage.getItem('mosaic.inspector-width')).toBe('440');expect(document.querySelector('aside')?.style.width).toBe('440px');
 vi.stubGlobal('innerWidth',800);window.dispatchEvent(new Event('resize'));await Promise.resolve();
 expect(document.querySelector('[role="dialog"]')?.getAttribute('aria-modal')).toBe('true');expect(document.querySelector('[role="separator"]')).toBeNull();
 document.querySelector('aside')!.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true}));expect(document.querySelector('aside')).toBeNull();expect(document.activeElement).toBe(opener);
});
