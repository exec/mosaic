import {expect, test, vi} from 'vitest';
import {render} from 'solid-js/web';
import {IconRail} from './IconRail';
import {FilterRail} from './FilterRail';
import {SettingsSidebar} from '../settings/SettingsSidebar';
import {computeCounts} from '../../lib/store';

const runtime = vi.hoisted(() => ({desktop: false}));
vi.mock('../../lib/runtime', () => ({isWailsRuntime: () => runtime.desktop}));

for (const desktop of [false, true]) {
  test(`${desktop ? 'native' : 'browser'} sidebars reserve only the appropriate top spacing`, () => {
    runtime.desktop = desktop;
    const host = document.createElement('div');
    const noop = () => {};
    const dispose = render(() => <>
      <IconRail view="torrents" settingsPane="general" onNavigate={noop} onNavigateSettingsPane={noop} flavor="daemon" currentUser={null} />
      <FilterRail counts={computeCounts([])} active="all" categories={[]} tags={[]} selectedCategoryID={null} selectedTagID={null} onSelect={noop} onSelectCategory={noop} onSelectTag={noop} onNavigateSettingsPane={noop} />
      <SettingsSidebar active="general" onSelect={noop} flavor="daemon" currentUser={null} />
    </>, host);
    try {
      const sidebars = host.querySelectorAll('nav, aside');
      expect(sidebars).toHaveLength(3);
      for (const sidebar of Array.from(sidebars)) {
        expect(sidebar.classList.contains('pt-10')).toBe(desktop);
        expect(sidebar.classList.contains('pt-2')).toBe(!desktop);
      }
    } finally {dispose();}
  });
}
