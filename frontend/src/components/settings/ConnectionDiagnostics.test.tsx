import {expect,test,vi} from 'vitest';
import {render} from 'solid-js/web';
import {api} from '../../lib/bindings';
import {ConnectionDiagnostics} from './ConnectionDiagnostics';
vi.mock('../../lib/bindings',()=>({api:{getConnectionDiagnostics:vi.fn()}}));
test('shows observed engine data and flags readings stale after a failed refresh',async()=>{
 vi.mocked(api.getConnectionDiagnostics).mockResolvedValue({available:true,listeners:['tcp · [::]:45678'],inbound_peers:2,outbound_peers:3,dht_servers:1,dht_nodes:15,dht_good_nodes:10,dht_announces:4});
 const dispose=render(()=><ConnectionDiagnostics/>,document.body);
 try{await vi.waitFor(()=>expect(document.body.textContent).toContain('2 inbound · 3 outbound'));expect(document.body.textContent).toContain('15 known nodes');vi.mocked(api.getConnectionDiagnostics).mockRejectedValueOnce(new Error('Offline'));document.querySelector<HTMLButtonElement>('button')!.click();await vi.waitFor(()=>expect(document.querySelector('[role="alert"]')?.textContent).toContain('out of date'));expect(document.body.textContent).toContain('[::]:45678');}finally{dispose();document.body.innerHTML='';}
});
