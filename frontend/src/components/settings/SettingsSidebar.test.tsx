import {expect, test} from 'vitest';
import {render} from 'solid-js/web';
import {SettingsSidebar} from './SettingsSidebar';
import {isItemVisible} from '../../lib/sidebar_pins';

test('daemon settings and saved sidebar pins cannot expose Updates', () => {
 const host = document.createElement('div');
 const dispose = render(() => <SettingsSidebar active="general" onSelect={() => {}} flavor="daemon" currentUser={null} />, host);
 try {
  expect(host.textContent).not.toContain('Updates');
  expect(isItemVisible('updates', {flavor: 'daemon', user: null})).toBe(false);
  expect(isItemVisible('updates', {flavor: 'desktop', user: null})).toBe(true);
 } finally {dispose();}
});
