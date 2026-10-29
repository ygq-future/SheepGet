// @vitest-environment happy-dom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { Select } from './Select';

const options = [{ value: 'a', label: '选项 A' }];

describe('Select', () => {
  afterEach(cleanup);

  it('keeps the clear control outside the trigger button', () => {
    const { container } = render(
      <Select value="a" onChange={() => {}} options={options} clearable />,
    );

    const trigger = container.querySelector('[data-select-trigger]');
    expect(trigger).not.toBeNull();
    // 原生 <button> 里不能再出现可聚焦控件：读屏与键盘激活都会因此失效。
    expect(
      trigger?.querySelector('button, a, input, select, textarea, [role="button"], [tabindex]'),
    ).toBeNull();

    const clear = screen.getByRole('button', { name: '清空' });
    expect(trigger?.contains(clear)).toBe(false);
  });

  it('clears the value without opening the menu', () => {
    const onChange = vi.fn();
    render(<Select value="a" onChange={onChange} options={options} clearable />);

    fireEvent.click(screen.getByRole('button', { name: '清空' }));

    expect(onChange).toHaveBeenCalledWith('');
    expect(screen.queryByRole('menu')).toBeNull();
  });

  it('shows the chevron when there is nothing to clear', () => {
    const { container } = render(
      <Select value="" onChange={() => {}} options={options} clearable />,
    );

    expect(screen.queryByRole('button', { name: '清空' })).toBeNull();
    expect(container.querySelector('[data-select-trigger] svg')).not.toBeNull();
  });
});
