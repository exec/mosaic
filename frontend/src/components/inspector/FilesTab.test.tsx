import {expect, test, vi} from 'vitest';
import {render} from 'solid-js/web';
import {FilesTab} from './FilesTab';
import type {DetailDTO} from '../../lib/bindings';

test('file priority has one labeled button and sends the selected priority', () => {
  const onSetPriority = vi.fn();
  const detail = {files: [{index: 2, path: 'folder/example.iso', size: 1024, bytes_done: 512, progress: 0.5, priority: 'normal'}]} as DetailDTO;
  const dispose = render(() => <FilesTab canEdit={true} detail={detail} onSetPriority={onSetPriority} />, document.body);
  try {
    const button = document.querySelector<HTMLButtonElement>('button[aria-label="Priority for folder/example.iso"]')!;
    expect(button).not.toBeNull();
    expect(button.querySelector('button')).toBeNull();
    button.dispatchEvent(new PointerEvent('pointerdown', {button: 0, bubbles: true}));
    const high = Array.from(document.querySelectorAll<HTMLElement>('[role="menuitem"]')).find((item) => item.textContent === 'High')!;
    expect(high).toBeTruthy();
    high.dispatchEvent(new PointerEvent('pointerup', {button: 0, bubbles: true}));
    high.click();
    expect(onSetPriority).toHaveBeenCalledWith(2, 'high');
  } finally { dispose(); document.body.innerHTML = ''; }
});

test('viewers can read file priorities without opening editing controls', () => {
  const onSetPriority = vi.fn();
  const detail = {files: [{index: 2, path: 'example.iso', size: 1024, progress: 0.5, priority: 'high'}]} as DetailDTO;
  const dispose = render(() => <FilesTab canEdit={false} detail={detail} onSetPriority={onSetPriority} />, document.body);
  try {
    expect(document.body.textContent).toContain('high');
    expect(document.querySelector('[aria-haspopup="menu"]')).toBeNull();
    expect(onSetPriority).not.toHaveBeenCalled();
  } finally { dispose(); document.body.innerHTML = ''; }
});

test('folder actions use one batch and search preserves the selected size', () => {
  const onSetPriority = vi.fn(), onSetPriorities = vi.fn();
  const detail = {files: [
    {index: 0, path: 'folder/a.iso', size: 1024, bytes_done: 0, progress: 0, priority: 'normal'},
    {index: 1, path: 'folder/b.txt', size: 1024, bytes_done: 0, progress: 0, priority: 'skip'},
    {index: 2, path: 'other/c.txt', size: 1024, bytes_done: 0, progress: 0, priority: 'normal'},
  ]} as DetailDTO;
  const dispose = render(() => <FilesTab canEdit detail={detail} onSetPriority={onSetPriority} onSetPriorities={onSetPriorities} />, document.body);
  try {
    const search = document.querySelector<HTMLInputElement>('[aria-label="Search files"]')!;
    search.value = 'a.iso';search.dispatchEvent(new Event('input', {bubbles: true}));
    expect(document.body.textContent).toContain('2 selected');
    expect(document.body.textContent).not.toContain('c.txt');
    const button = document.querySelector<HTMLButtonElement>('[aria-label="Priority for folder"]')!;
    button.dispatchEvent(new PointerEvent('pointerdown', {button: 0, bubbles: true}));
    const skip = Array.from(document.querySelectorAll<HTMLElement>('[role="menuitem"]')).find((item) => item.textContent === 'Skip')!;
    skip.dispatchEvent(new PointerEvent('pointerup', {button: 0, bubbles: true}));
    skip.click();
    expect(onSetPriorities).toHaveBeenCalledWith({0: 'skip', 1: 'skip'});
    expect(onSetPriority).not.toHaveBeenCalled();
  } finally {dispose();document.body.innerHTML = '';}
});
