import {afterEach,expect,test,vi} from 'vitest';
import {createSignal} from 'solid-js';
import {render} from 'solid-js/web';
import {api,type FilterDTO} from '../../lib/bindings';
import {RSSFilterPreview} from './RSSFilterPreview';
vi.mock('../../lib/bindings',()=>({api:{previewRSSFilter:vi.fn()}}));
let dispose:()=>void;afterEach(()=>{dispose?.();document.body.innerHTML='';vi.resetAllMocks();});
test('uses the backend for RE2, shows handled items, and invalidates edited previews',async()=>{
 vi.mocked(api.previewRSSFilter).mockResolvedValue({destination:'/downloads',items:[{guid:'a',title:'Ubuntu',pub_date:'',torrent_url:'magnet:x',matches:true,already_handled:true,in_library:false},{guid:'b',title:'Fedora',pub_date:'',torrent_url:'',matches:false,already_handled:false,in_library:false}]});
 const [filter,setFilter]=createSignal<FilterDTO>({id:0,feed_id:1,regex:'(?i)ubuntu',category_id:null,save_path:'',enabled:false});
 dispose=render(()=><RSSFilterPreview filter={filter()}/>,document.body);
 document.querySelector<HTMLButtonElement>('button')!.click();
 await vi.waitFor(()=>expect(document.body.textContent).toContain('1 of 2 titles match'));
 expect(api.previewRSSFilter).toHaveBeenCalledWith(filter());expect(document.body.textContent).toContain('Already handled by RSS');
 expect(document.body.textContent).not.toContain('Fedora');document.querySelector<HTMLInputElement>('input')!.click();expect(document.body.textContent).toContain('Fedora');
 setFilter({...filter(),regex:'fedora'});expect(document.body.textContent).toContain('Test again');expect(document.body.textContent).not.toContain('Destination:');
});
