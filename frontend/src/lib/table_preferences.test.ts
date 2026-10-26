import {beforeEach, expect, test} from 'vitest';
import {loadTablePreferences, TABLE_PREFERENCES_KEY} from './table_preferences';
beforeEach(() => localStorage.clear());
test('corrupt preferences restore compact defaults', () => {
 localStorage.setItem(TABLE_PREFERENCES_KEY,'{broken');
 expect(loadTablePreferences()).toEqual({sorting:[{id:'added_at',desc:true}],visibility:{ratio:false,total_up:false,seeds:false,queue_position:false}});
});
test('restores known sorts and visibility, preserving the name column', () => {
 localStorage.setItem(TABLE_PREFERENCES_KEY,JSON.stringify({sorting:[{id:'total_up',desc:true},{id:'bogus',desc:false},null],visibility:{name:false,ratio:true,seeds:true,progress:false,bogus:true}}));
 const p=loadTablePreferences();
 expect(p.sorting).toEqual([{id:'total_up',desc:true}]);
 expect(p.visibility).toMatchObject({name:true,ratio:true,seeds:true,progress:false});
 expect(p.visibility).not.toHaveProperty('bogus');
});
