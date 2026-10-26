import {afterEach,beforeEach,expect,test,vi} from 'vitest';
import {render} from 'solid-js/web';
import {api} from '../../lib/bindings';
import {ManualPeerBlocks} from './ManualPeerBlocks';
vi.mock('../../lib/bindings',()=>({api:{listBlockedPeers:vi.fn(),setPeerBlocked:vi.fn()}}));
vi.mock('solid-sonner',()=>({toast:{success:vi.fn(),error:vi.fn()}}));
let dispose:()=>void;
beforeEach(()=>{vi.resetAllMocks();vi.mocked(api.listBlockedPeers).mockResolvedValue(['192.0.2.1','2001:db8::1']);});
afterEach(()=>{dispose?.();document.body.innerHTML='';});
function input(label:string,value:string){const el=document.querySelector<HTMLInputElement>(`[aria-label="${label}"]`)!;el.value=value;el.dispatchEvent(new Event('input',{bubbles:true}));}
test('searches, adds canonical results, and preserves blocks after failed removal',async()=>{
 dispose=render(()=><ManualPeerBlocks/>,document.body);
 await vi.waitFor(()=>expect(document.body.textContent).toContain('2001:db8::1'));
 input('Search blocked peers','2001');expect(document.body.textContent).not.toContain('192.0.2.1');
 vi.mocked(api.setPeerBlocked).mockRejectedValueOnce(new Error('Could not save'));
 Array.from(document.querySelectorAll('button')).find(b=>b.textContent==='Unblock')!.click();
 await vi.waitFor(()=>expect(document.querySelector('[role="alert"]')?.textContent).toContain('Could not save'));
 expect(document.body.textContent).toContain('2001:db8::1');
 input('Search blocked peers','');input('Peer IP to block','::ffff:192.0.2.9');
 vi.mocked(api.listBlockedPeers).mockResolvedValue(['192.0.2.1','2001:db8::1','192.0.2.9']);
 document.querySelector('form')!.dispatchEvent(new Event('submit',{bubbles:true,cancelable:true}));
 await vi.waitFor(()=>expect(document.body.textContent).toContain('192.0.2.9'));
 expect(api.setPeerBlocked).toHaveBeenLastCalledWith('::ffff:192.0.2.9',true);
 vi.mocked(api.listBlockedPeers).mockResolvedValue(['2001:db8::1','192.0.2.9']);
 Array.from(document.querySelectorAll('button')).find(b=>b.textContent==='Unblock')!.click();
 await vi.waitFor(()=>expect(document.body.textContent).not.toContain('192.0.2.1'));
});
