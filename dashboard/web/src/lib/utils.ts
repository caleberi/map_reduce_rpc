import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function formatMs(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}μs`
  if (ms < 1000) return `${ms.toFixed(1)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

export function formatMBs(mbps: number): string {
  if (mbps < 1) return `${(mbps * 1024).toFixed(0)} KB/s`
  if (mbps >= 1024) return `${(mbps / 1024).toFixed(1)} GB/s`
  return `${mbps.toFixed(1)} MB/s`
}
