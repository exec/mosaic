import {expect,test,vi} from 'vitest';
import {render} from 'solid-js/web';
import {api} from '../../lib/bindings';
import {SettingsBackup} from './SettingsBackup';
vi.mock('../../lib/bindings',()=>({api:{exportSettings:vi.fn(),importSettings:vi.fn()}}));
test('reviews an import before writing, and reports errors without claiming success',async()=>{
 const dispose=render(()=><SettingsBackup/>,document.body);
 try{
 const input=document.querySelector<HTMLInputElement>('input[type="file"]')!;
 const backup={version:1,limits:{down_kbps:12},queue:{max_active_downloads:2},peers:{listen_port:12345},seeding:{ratio_limit:2}};
 Object.defineProperty(input,'files',{configurable:true,value:[new File([JSON.stringify(backup)],'backup.json')]});input.dispatchEvent(new Event('change',{bubbles:true}));
 await vi.waitFor(()=>expect(document.body.textContent).toContain('Ready to replace'));
 expect(api.importSettings).not.toHaveBeenCalled();vi.mocked(api.importSettings).mockRejectedValueOnce(new Error('Invalid backup'));
 Array.from(document.querySelectorAll('button')).find(b=>b.textContent==='Import settings')!.click();
 await vi.waitFor(()=>expect(document.querySelector('[role="alert"]')?.textContent).toContain('Invalid backup'));
 expect(document.body.textContent).not.toContain('Settings imported.');expect(api.importSettings).toHaveBeenCalledWith(backup);
 }finally{dispose();document.body.innerHTML='';}
});
