import {
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  createColumnHelper,
  flexRender,
  type SortingState,
} from "@tanstack/react-table"
import { useState } from "react"
import type { WorkerMetrics } from "@/types"
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { formatMs } from "@/lib/utils"

const columnHelper = createColumnHelper<WorkerMetrics>()

const columns = [
  columnHelper.accessor("worker_id", {
    header: "Worker",
    cell: (info) => `#${info.getValue()}`,
  }),
  columnHelper.accessor("tasks_completed", {
    header: "Tasks",
    cell: (info) => info.getValue(),
  }),
  columnHelper.accessor("total_time_ms", {
    header: "Total Time",
    cell: (info) => formatMs(info.getValue()),
  }),
  columnHelper.accessor("utilization", {
    header: "Utilization",
    cell: (info) => {
      const pct = (info.getValue() * 100).toFixed(1)
      const variant = info.getValue() > 0.8 ? "default" : info.getValue() > 0.5 ? "secondary" : "outline"
      return <Badge variant={variant}>{pct}%</Badge>
    },
  }),
  columnHelper.accessor("crashes", {
    header: "Crashes",
    cell: (info) => {
      const val = info.getValue()
      if (val === 0) return <span className="text-muted-foreground">0</span>
      return <Badge variant="destructive">{val}</Badge>
    },
  }),
]

export function WorkerTable({ workers }: { workers: WorkerMetrics[] }) {
  const [sorting, setSorting] = useState<SortingState>([])

  const table = useReactTable({
    data: workers,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  return (
    <Table>
      <TableHeader>
        {table.getHeaderGroups().map((headerGroup) => (
          <TableRow key={headerGroup.id}>
            {headerGroup.headers.map((header) => (
              <TableHead
                key={header.id}
                className="cursor-pointer select-none"
                onClick={header.column.getToggleSortingHandler()}
              >
                {flexRender(header.column.columnDef.header, header.getContext())}
                {{ asc: " ↑", desc: " ↓" }[header.column.getIsSorted() as string] ?? ""}
              </TableHead>
            ))}
          </TableRow>
        ))}
      </TableHeader>
      <TableBody>
        {table.getRowModel().rows.map((row) => (
          <TableRow key={row.id}>
            {row.getVisibleCells().map((cell) => (
              <TableCell key={cell.id}>
                {flexRender(cell.column.columnDef.cell, cell.getContext())}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
