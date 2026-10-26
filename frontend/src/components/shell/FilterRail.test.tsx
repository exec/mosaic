import {expect,test,vi} from 'vitest';
import {render} from 'solid-js/web';
import {FilterRail} from './FilterRail';
import {computeCounts} from '../../lib/store';
test('sidebar sections collapse accessibly and retain the choice on remount',()=>{
 localStorage.clear();const props={counts:computeCounts([]),active:'all' as const,categories:[],tags:[],selectedCategoryID:null,selectedTagID:null,onSelect:vi.fn(),onSelectCategory:vi.fn(),onSelectTag:vi.fn(),onNavigateSettingsPane:vi.fn()};
 let dispose=render(()=><FilterRail {...props}/>,document.body);
 try{const button=Array.from(document.querySelectorAll('button')).find(b=>b.textContent==='Categories')!;button.click();expect(button.getAttribute('aria-expanded')).toBe('false');expect(document.getElementById(button.getAttribute('aria-controls')!)?.hidden).toBe(true);dispose();document.body.innerHTML='';dispose=render(()=><FilterRail {...props}/>,document.body);expect(Array.from(document.querySelectorAll('button')).find(b=>b.textContent==='Categories')?.getAttribute('aria-expanded')).toBe('false');}finally{dispose();document.body.innerHTML='';localStorage.clear();}
});
