import {createSignal, Match, Switch} from 'solid-js';
import type {CategoryDTO, LimitsDTO, QueueLimitsDTO, TagDTO} from '../../lib/bindings';
import {SettingsSidebar, type SettingsPane} from './SettingsSidebar';
import {GeneralPane} from './GeneralPane';
import {ConnectionPane} from './ConnectionPane';
import {CategoriesPane} from './CategoriesPane';
import {TagsPane} from './TagsPane';
import {AboutPane} from './AboutPane';

type Props = {
  defaultSavePath: string;
  categories: CategoryDTO[];
  tags: TagDTO[];
  limits: LimitsDTO;
  queueLimits: QueueLimitsDTO;
  onSetDefaultSavePath: (path: string) => Promise<void>;
  onSetLimits: (l: LimitsDTO) => Promise<void>;
  onSetQueueLimits: (q: QueueLimitsDTO) => Promise<void>;
  onCreateCategory: (name: string, savePath: string, color: string) => Promise<void>;
  onUpdateCategory: (id: number, name: string, savePath: string, color: string) => Promise<void>;
  onDeleteCategory: (id: number) => Promise<void>;
  onCreateTag: (name: string, color: string) => Promise<void>;
  onDeleteTag: (id: number) => Promise<void>;
};

export function SettingsRoute(props: Props) {
  const [pane, setPane] = createSignal<SettingsPane>('general');

  return (
    <div class="flex h-full">
      <SettingsSidebar active={pane()} onSelect={setPane} />
      <div class="flex-1 overflow-auto">
        <Switch>
          <Match when={pane() === 'general'}>
            <GeneralPane defaultSavePath={props.defaultSavePath} onSetDefaultSavePath={props.onSetDefaultSavePath} />
          </Match>
          <Match when={pane() === 'connection'}>
            <ConnectionPane limits={props.limits} queueLimits={props.queueLimits} onSetLimits={props.onSetLimits} onSetQueueLimits={props.onSetQueueLimits} />
          </Match>
          <Match when={pane() === 'categories'}>
            <CategoriesPane categories={props.categories} onCreate={props.onCreateCategory} onUpdate={props.onUpdateCategory} onDelete={props.onDeleteCategory} />
          </Match>
          <Match when={pane() === 'tags'}>
            <TagsPane tags={props.tags} onCreate={props.onCreateTag} onDelete={props.onDeleteTag} />
          </Match>
          <Match when={pane() === 'about'}>
            <AboutPane />
          </Match>
        </Switch>
      </div>
    </div>
  );
}
