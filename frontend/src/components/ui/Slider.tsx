import { useRef, useCallback, type KeyboardEvent } from 'react';

interface SliderProps {
  min: number;
  max: number;
  step?: number;
  value: number;
  onChange: (val: number) => void;
  className?: string;
  disabled?: boolean;
}

export function Slider({
  min,
  max,
  step = 1,
  value,
  onChange,
  className = '',
  disabled = false,
}: SliderProps) {
  const trackRef = useRef<HTMLDivElement>(null);

  const percentage = Math.min(100, Math.max(0, ((value - min) / (max - min)) * 100));

  const updateFromPosition = useCallback(
    (clientX: number) => {
      if (!trackRef.current || disabled) return;
      const rect = trackRef.current.getBoundingClientRect();
      const pos = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
      const rawValue = min + pos * (max - min);
      const steppedValue = Math.round((rawValue - min) / step) * step + min;
      const clamped = Math.min(max, Math.max(min, steppedValue));
      onChange(clamped);
    },
    [min, max, step, onChange, disabled],
  );

  const handleMouseDown = (e: React.MouseEvent) => {
    if (disabled) return;
    updateFromPosition(e.clientX);

    const onMouseMove = (moveEvent: MouseEvent) => {
      updateFromPosition(moveEvent.clientX);
    };

    const onMouseUp = () => {
      window.removeEventListener('mousemove', onMouseMove);
      window.removeEventListener('mouseup', onMouseUp);
    };

    window.addEventListener('mousemove', onMouseMove);
    window.addEventListener('mouseup', onMouseUp);
  };

  const handleKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    if (disabled) return;
    if (e.key === 'ArrowRight' || e.key === 'ArrowUp') {
      e.preventDefault();
      onChange(Math.min(max, value + step));
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') {
      e.preventDefault();
      onChange(Math.max(min, value - step));
    }
  };

  return (
    <div
      role="slider"
      tabIndex={disabled ? -1 : 0}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-valuenow={value}
      onKeyDown={handleKeyDown}
      onMouseDown={handleMouseDown}
      className={`group relative flex h-6 w-full cursor-pointer items-center select-none focus:outline-hidden ${
        disabled ? 'cursor-not-allowed opacity-50' : ''
      } ${className}`}
    >
      {/* Background track */}
      <div
        ref={trackRef}
        className="relative h-1.5 w-full overflow-hidden rounded-full bg-[var(--bg-subtle)] transition-colors"
      >
        {/* Active fill track */}
        <div
          className="h-full rounded-full bg-[var(--accent)] transition-all duration-75"
          style={{ width: `${percentage}%` }}
        />
      </div>

      {/* Thumb handle */}
      <div
        className="absolute top-1/2 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-[var(--bg-surface)] bg-[var(--accent)] shadow-md transition-transform duration-100 group-hover:scale-110 group-focus:scale-110 group-focus:ring-4 group-focus:ring-[var(--border-focus)] active:scale-95"
        style={{
          left: `${percentage}%`,
          width: '14px',
          height: '14px',
        }}
      />
    </div>
  );
}
