import * as React from "react"
import { cn } from "@/lib/utils"

function Slider({
  className,
  min = 0,
  max = 100,
  step = 1,
  value,
  onValueChange,
  ...props
}: React.HTMLAttributes<HTMLDivElement> & {
  min?: number
  max?: number
  step?: number
  value: number[]
  onValueChange: (value: number[]) => void
}) {
  const pct = ((value[0] - min) / (max - min)) * 100

  return (
    <div className={cn("relative flex w-full touch-none select-none items-center", className)} {...props}>
      <div className="relative h-1.5 w-full grow overflow-hidden rounded-full bg-primary/20">
        <div className="absolute h-full bg-primary" style={{ width: `${pct}%` }} />
      </div>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={value[0]}
        onChange={(e) => onValueChange([Number(e.target.value)])}
        className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
      />
      <div
        className="absolute h-4 w-4 rounded-full border border-primary/50 bg-background shadow transition-colors"
        style={{ left: `calc(${pct}% - 8px)` }}
      />
    </div>
  )
}

export { Slider }
