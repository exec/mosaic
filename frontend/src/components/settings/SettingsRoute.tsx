import {Match, Switch} from 'solid-js';
import type {BlocklistDTO, CategoryDTO, FeedDTO, FilterDTO, LimitsDTO, QueueLimitsDTO, ScheduleRuleDTO, TagDTO, UpdaterConfigDTO, UpdateInfoDTO, WebConfigDTO} from '../../lib/bindings';
import {SettingsSidebar, type SettingsPane} from './SettingsSidebar';
import {GeneralPane} from './GeneralPane';
import {ConnectionPane} from './ConnectionPane';
import {WebInterfacePane} from './WebInterfacePane';
import {UpdatesPane} from './UpdatesPane';
import {SchedulePane} from './SchedulePane';
import {BlocklistPane} from './BlocklistPane';
import {RSSPane} from './RSSPane';
import {CategoriesPane} from './CategoriesPane';
import {TagsPane} from './TagsPane';
import {AboutPane} from './AboutPane';

type Props = {
  pane: SettingsPane;
  onPaneChange: (p: SettingsPane) => void;
  defaultSavePath: string;
  categories: CategoryDTO[];
  tags: TagDTO[];
  limits: LimitsDTO;
  queueLimits: QueueLimitsDTO;
  scheduleRules: ScheduleRuleDTO[];
  blocklist: BlocklistDTO;
  feeds: FeedDTO[];
  filtersByFeed: Record<number, FilterDTO[]>;
  webConfig: WebConfigDTO;
  updaterConfig: UpdaterConfigDTO;
  updateInfo: UpdateInfoDTO | null;
  appVersion: string;
  onSetDefaultSavePath: (path: string) => Promise<void>;
  onSetWebConfig: (c: WebConfigDTO) => Promise<void>;
  onSetWebPassword: (plain: string) => Promise<void>;
  onRotateAPIKey: () => Promise<string>;
  onSetUpdaterConfig: (c: UpdaterConfigDTO) => Promise<void>;
  onCheckForUpdate: () => Promise<UpdateInfoDTO>;
  onInstallUpdate: () => Promise<void>;
  onSetLimits: (l: LimitsDTO) => Promise<void>;
  onSetQueueLimits: (q: QueueLimitsDTO) => Promise<void>;
  onCreateCategory: (name: string, savePath: string, color: string) => Promise<void>;
  onUpdateCategory: (id: number, name: string, savePath: string, color: string) => Promise<void>;
  onDeleteCategory: (id: number) => Promise<void>;
  onCreateTag: (name: string, color: string) => Promise<void>;
  onDeleteTag: (id: number) => Promise<void>;
  onCreateScheduleRule: (r: ScheduleRuleDTO) => Promise<void>;
  onUpdateScheduleRule: (r: ScheduleRuleDTO) => Promise<void>;
  onDeleteScheduleRule: (id: number) => Promise<void>;
  onSetBlocklistURL: (url: string, enabled: boolean) => Promise<void>;
  onRefreshBlocklist: () => Promise<void>;
  onCreateFeed: (f: FeedDTO) => Promise<void>;
  onUpdateFeed: (f: FeedDTO) => Promise<void>;
  onDeleteFeed: (id: number) => Promise<void>;
  onLoadFiltersForFeed: (feedID: number) => Promise<void>;
  onCreateFilter: (f: FilterDTO) => Promise<void>;
  onUpdateFilter: (f: FilterDTO) => Promise<void>;
  onDeleteFilter: (feedID: number, id: number) => Promise<void>;
};

export function SettingsRoute(props: Props) {
  return (
    <div class="flex h-full">
      <SettingsSidebar active={props.pane} onSelect={props.onPaneChange} />
      <div class="flex-1 overflow-auto">
        <Switch>
          <Match when={props.pane === 'general'}>
            <GeneralPane defaultSavePath={props.defaultSavePath} onSetDefaultSavePath={props.onSetDefaultSavePath} />
          </Match>
          <Match when={props.pane === 'connection'}>
            <ConnectionPane limits={props.limits} queueLimits={props.queueLimits} onSetLimits={props.onSetLimits} onSetQueueLimits={props.onSetQueueLimits} />
          </Match>
          <Match when={props.pane === 'web'}>
            <WebInterfacePane
              webConfig={props.webConfig}
              onSetWebConfig={props.onSetWebConfig}
              onSetWebPassword={props.onSetWebPassword}
              onRotateAPIKey={props.onRotateAPIKey}
            />
          </Match>
          <Match when={props.pane === 'updates'}>
            <UpdatesPane
              config={props.updaterConfig}
              info={props.updateInfo}
              appVersion={props.appVersion}
              onSet={props.onSetUpdaterConfig}
              onCheck={props.onCheckForUpdate}
              onInstall={props.onInstallUpdate}
            />
          </Match>
          <Match when={props.pane === 'schedule'}>
            <SchedulePane
              rules={props.scheduleRules}
              onCreate={props.onCreateScheduleRule}
              onUpdate={props.onUpdateScheduleRule}
              onDelete={props.onDeleteScheduleRule}
            />
          </Match>
          <Match when={props.pane === 'blocklist'}>
            <BlocklistPane
              blocklist={props.blocklist}
              onSetBlocklistURL={props.onSetBlocklistURL}
              onRefreshBlocklist={props.onRefreshBlocklist}
            />
          </Match>
          <Match when={props.pane === 'rss'}>
            <RSSPane
              feeds={props.feeds}
              filtersByFeed={props.filtersByFeed}
              categories={props.categories}
              onCreateFeed={props.onCreateFeed}
              onUpdateFeed={props.onUpdateFeed}
              onDeleteFeed={props.onDeleteFeed}
              onLoadFilters={props.onLoadFiltersForFeed}
              onCreateFilter={props.onCreateFilter}
              onUpdateFilter={props.onUpdateFilter}
              onDeleteFilter={props.onDeleteFilter}
            />
          </Match>
          <Match when={props.pane === 'categories'}>
            <CategoriesPane categories={props.categories} onCreate={props.onCreateCategory} onUpdate={props.onUpdateCategory} onDelete={props.onDeleteCategory} />
          </Match>
          <Match when={props.pane === 'tags'}>
            <TagsPane tags={props.tags} onCreate={props.onCreateTag} onDelete={props.onDeleteTag} />
          </Match>
          <Match when={props.pane === 'about'}>
            <AboutPane />
          </Match>
        </Switch>
      </div>
    </div>
  );
}
