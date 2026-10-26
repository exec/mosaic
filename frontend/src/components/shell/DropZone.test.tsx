import {expect,test,vi} from 'vitest';
import {render} from 'solid-js/web';
import {DropZone} from './DropZone';
vi.mock('solid-sonner',()=>({toast:{error:vi.fn()}}));
test('reviews every supported dropped file sequentially and reports skipped and failed files',async()=>{
 let finish!:(added:boolean)=>void;const onTorrent=vi.fn().mockImplementationOnce(()=>new Promise(r=>finish=r)).mockRejectedValueOnce(new Error('broken torrent'));
 const dispose=render(()=><DropZone onMagnet={vi.fn()} onTorrentBytes={onTorrent}>List</DropZone>,document.body);
 try{
  const event=new Event('drop',{cancelable:true});Object.defineProperty(event,'dataTransfer',{value:{getData:()=>'',files:[new File(['a'],'first.torrent'),new File(['b'],'second.TORRENT'),new File(['c'],'readme.txt')]}});window.dispatchEvent(event);
  await vi.waitFor(()=>expect(onTorrent).toHaveBeenCalledTimes(1));finish(false);
  await vi.waitFor(()=>expect(document.body.textContent).toContain('Drop complete'));
  expect(onTorrent).toHaveBeenCalledTimes(2);expect(onTorrent.mock.calls.map(c=>c[1])).toEqual(['first.torrent','second.TORRENT']);expect(document.body.textContent).toContain('first.torrent · Skipped');expect(document.body.textContent).toContain('second.TORRENT · Error: broken torrent');expect(document.body.textContent).toContain('readme.txt · Error: Only .torrent');
 }finally{dispose();document.body.innerHTML='';}
});
