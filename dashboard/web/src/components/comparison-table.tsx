import {
  useReactTable,
  getCoreRowModel,
  getSortedRowModel,
  createColumnHelper,
  flexRender,
  type SortingState,
} from "@tanstack/react-table"
import { useState } from "react"
import type { SimulationResult } from "@/types"
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card"
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table"
import { Badge } from "@/components/ui/badge"
import { formatMs, formatMBs } from "@/lib/utils"

const columnHelper = createColumnHelper<SimulationResult>()

const columns = [
  columnHelper.accessor("plugin", {
    header: "Plugin",
    cell: (info) => <span className="font-medium">{info.getValue()}</span>,
  }),
  columnHelper.accessor("total_duration_ms", {
    header: "Total Time",
    cell: (info) => formatMs(info.getValue()),
  }),
  columnHelper.accessor("throughput_mb_s", {
    header: "Throughput",
    cell: (info) => formatMBs(info.getValue()),
  }),
  columnHelper.accessor("map_phase.duration_ms", {
    header: "Map Phase",
    cell: (info) => formatMs(info.getValue()),
  }),
  columnHelper.accessor("shuffle_phase.duration_ms", {
    header: "Shuffle Phase",
    cell: (info) => formatMs(info.getValue()),
  }),
  columnHelper.accessor("reduce_phase.duration_ms", {
    header: "Reduce Phase",
    cell: (info) => formatMs(info.getValue()),
  }),
  columnHelper.accessor("latency.avg", {
    header: "Avg Latency",
    cell: (info) => formatMs(info.getValue()),
  }),
  columnHelper.accessor("latency.p99", {
    header: "P99 Latency",
    cell: (info) => formatMs(info.getValue()),
  }),
  columnHelper.accessor("total_crashes", {
    header: "Crashes",
    cell: (info) => {
      const val = info.getValue()
      if (val === 0) return <Badge variant="secondary">0</Badge>
      return <Badge variant="destructive">{val}</Badge>
    },
  }),
]

export function ComparisonTable({ results }: { results: SimulationResult[] }) {
  const [sorting, setSorting] = useState<SortingState>([])

  const table = useReactTable({
    data: results,
    columns,
    state: { sorting },
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getSortedRowModel: getSortedRowModel(),
  })

  return (
    <Card>
      <CardHeader>
        <CardTitle>Detailed Comparison</CardTitle>
        <CardDescription>Click column headers to sort. {results.length} plugins compared.</CardDescription>
      </CardHeader>
      <CardContent>
        <Table>
          <TableHeader>
            {table.getHeaderGroups().map((headerGroup) => (
              <TableRow key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <TableHead
                    key={header.id}
                    className="cursor-pointer select-none whitespace-nowrap"
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
            {table.getRowModel().rows.map((row, rowIndex) => (
              <TableRow key={row.id}>
                {row.getVisibleCells().map((cell) => (
                  <TableCell key={cell.id} className={rowIndex === 0 ? "font-medium" : ""}>
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </CardContent>
    </Card>
  )
}
