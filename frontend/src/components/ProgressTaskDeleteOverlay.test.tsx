// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ProgressTaskDeleteOverlay } from './ProgressTaskDeleteOverlay';
import * as taskModels from '../../bindings/sheep-get/internal/task/models';

function mockTask(filename = 'test-download-archive.zip'): taskModels.Task {
  return new taskModels.Task({
    id: 'task-test-1',
    filename,
    directory: 'C:/Downloads',
    status: taskModels.Status.StatusDownloading,
    totalBytes: 1024 * 1024,
    downloaded: 512 * 1024,
    speed: 1024,
    createdAt: '2026-11-05T00:00:00.000Z',
  });
}

describe('ProgressTaskDeleteOverlay', () => {
  afterEach(cleanup);

  it('renders confirmation title, filename and action controls within the overlay', () => {
    const task = mockTask('test-download-archive.zip');
    render(
      <ProgressTaskDeleteOverlay
        task={task}
        defaultDeleteDiskFile={false}
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );

    expect(screen.getByText('确认删除下载任务')).toBeTruthy();
    expect(screen.getByText('"test-download-archive.zip"')).toBeTruthy();
    expect(screen.getByRole('checkbox', { name: '同时删除本地文件' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '取消' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /确认删除/ })).toBeTruthy();
  });

  it('toggles deleteDiskFile checkbox and passes the checked value to onConfirm', () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    const task = mockTask('test.mp4');

    render(
      <ProgressTaskDeleteOverlay
        task={task}
        defaultDeleteDiskFile={false}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );

    const checkbox = screen.getByRole('checkbox', { name: '同时删除本地文件' });
    expect(checkbox.getAttribute('aria-checked')).toBe('false');

    // Toggle checkbox
    fireEvent.click(checkbox);
    expect(checkbox.getAttribute('aria-checked')).toBe('true');

    // Click confirm
    const confirmBtn = screen.getByRole('button', { name: /确认删除/ });
    fireEvent.click(confirmBtn);

    expect(onConfirm).toHaveBeenCalledTimes(1);
    expect(onConfirm).toHaveBeenCalledWith(true);
    expect(onCancel).not.toHaveBeenCalled();
  });

  it('honors defaultDeleteDiskFile=true and passes true when confirmed without toggle', () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    const task = mockTask('test.mp4');

    render(
      <ProgressTaskDeleteOverlay
        task={task}
        defaultDeleteDiskFile={true}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );

    const checkbox = screen.getByRole('checkbox', { name: '同时删除本地文件' });
    expect(checkbox.getAttribute('aria-checked')).toBe('true');

    const confirmBtn = screen.getByRole('button', { name: /确认删除/ });
    fireEvent.click(confirmBtn);

    expect(onConfirm).toHaveBeenCalledWith(true);
  });

  it('triggers onCancel and does not call onConfirm when cancel button is clicked', () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    const task = mockTask('test.mp4');

    render(
      <ProgressTaskDeleteOverlay
        task={task}
        defaultDeleteDiskFile={false}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );

    const cancelBtn = screen.getByRole('button', { name: '取消' });
    fireEvent.click(cancelBtn);

    expect(onCancel).toHaveBeenCalledTimes(1);
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it('cancels on Escape key press', () => {
    const onCancel = vi.fn();
    const task = mockTask('test.mp4');

    render(
      <ProgressTaskDeleteOverlay
        task={task}
        defaultDeleteDiskFile={false}
        onConfirm={() => {}}
        onCancel={onCancel}
      />,
    );

    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('cancels when pointer down occurs outside the overlay', () => {
    const onCancel = vi.fn();
    const task = mockTask('test.mp4');

    render(
      <div>
        <div data-testid="outside-area">Outside</div>
        <ProgressTaskDeleteOverlay
          task={task}
          defaultDeleteDiskFile={false}
          onConfirm={() => {}}
          onCancel={onCancel}
        />
      </div>,
    );

    const outside = screen.getByTestId('outside-area');
    fireEvent.pointerDown(outside);
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
