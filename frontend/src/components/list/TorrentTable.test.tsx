import {expect,test,vi} from 'vitest';
import {render} from 'solid-js/web';
import {TorrentTable} from './TorrentTable';
import fixture from '../../../../backend/api/testdata/dto_snapshots/TorrentDTO_null_category.json';
vi.mock('@tanstack/solid-virtual',()=>({createVirtualizer:()=>({getVirtualItems:()=>[],getTotalSize:()=>0,scrollToIndex:vi.fn()})}));
test('keyboard navigation uses visible sort order and Enter inspects without opening on arrows',()=>{
 localStorage.clear();const select=vi.fn(),inspect=vi.fn();const dispose=render(()=><TorrentTable torrents={[{...fixture,access:'owner',id:'older',added_at:1},{...fixture,access:'owner',id:'newer',added_at:2}]} selection={new Set()} onRowClick={vi.fn()} onKeyboardSelect={select} onInspect={inspect}/>,document.body);
 try{const table=document.querySelector('table')!;const key=(value:string,shiftKey=false)=>table.dispatchEvent(new KeyboardEvent('keydown',{key:value,shiftKey,bubbles:true,cancelable:true}));
  key('ArrowDown');expect(select).toHaveBeenLastCalledWith('newer',false,['newer','older']);expect(inspect).not.toHaveBeenCalled();key('ArrowDown',true);expect(select).toHaveBeenLastCalledWith('older',true,['newer','older']);key('Enter');expect(inspect).toHaveBeenCalledWith('older');
 }finally{dispose();document.body.innerHTML='';localStorage.clear();}
});
