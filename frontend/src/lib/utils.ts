import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function unwrapEventData<T>(event: unknown): T | null | undefined {
  const payload = event && typeof event === 'object' && 'data' in event ? event.data : event;
  return payload as T | null | undefined;
}
