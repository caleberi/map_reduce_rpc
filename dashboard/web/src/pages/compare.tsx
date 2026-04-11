import { useState } from "react"
import { useQuery, useMutation } from "@tanstack/react-query"
import { fetchPlugins, runComparison } from "@/lib/api"
import type { CompareResult } from "@/types"
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Slider } from "@/components/ui/slider"
import { Badge } from "@/components/ui/badge"
import { ComparisonChart } from "@/components/comparison-chart"
import { ComparisonTable } from "@/components/comparison-table"
import { GitCompare, Loader2 } from "lucide-react"

export function ComparePage() {
  const [selectedPlugins, setSelectedPlugins] = useState<string[]>(["wc", "indexer", "jobcount"])
  const [inputSizeMb, setInputSizeMb] = useState(50)
  const [numWorkers, setNumWorkers] = useState(4)
  const [numChunks, setNumChunks] = useState(16)
  const [networkLatencyMs, setNetworkLatencyMs] = useState(5)
  const [result, setResult] = useState<CompareResult | null>(null)

  const { data: plugins } = useQuery({
    queryKey: ["plugins"],
    queryFn: fetchPlugins,
  })

  const mutation = useMutation({
    mutationFn: () => runComparison(selectedPlugins, inputSizeMb, numWorkers, numChunks, networkLatencyMs),
    onSuccess: (data) => setResult(data),
  })

  const togglePlugin = (name: string) => {
    setSelectedPlugins((prev) =>
      prev.includes(name) ? prev.filter((p) => p !== name) : [...prev, name]
    )
  }

  return (
    <div className="mx-auto max-w-7xl p-6 space-y-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Plugin Comparison</h1>
        <p className="text-muted-foreground">
          Compare performance across different MapReduce plugins side by side.
        </p>
      </div>

      <div className="grid gap-6 lg:grid-cols-[360px_1fr]">
        {/* Config */}
        <Card>
          <CardHeader>
            <CardTitle>Configuration</CardTitle>
            <CardDescription>Select plugins and parameters</CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            <div className="space-y-2">
              <Label>Plugins</Label>
              <div className="flex flex-wrap gap-2">
                {plugins?.map((p) => (
                  <Badge
                    key={p.name}
                    variant={selectedPlugins.includes(p.name) ? "default" : "outline"}
                    className="cursor-pointer"
                    onClick={() => togglePlugin(p.name)}
                  >
                    {p.name}
                  </Badge>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">{selectedPlugins.length} selected</p>
            </div>

            <div className="space-y-2">
              <div className="flex justify-between">
                <Label>Input Size</Label>
                <span className="text-xs text-muted-foreground">{inputSizeMb} MB</span>
              </div>
              <Slider min={1} max={500} step={1} value={[inputSizeMb]} onValueChange={(v) => setInputSizeMb(v[0])} />
            </div>

            <div className="space-y-2">
              <div className="flex justify-between">
                <Label>Workers</Label>
                <span className="text-xs text-muted-foreground">{numWorkers}</span>
              </div>
              <Slider min={1} max={32} step={1} value={[numWorkers]} onValueChange={(v) => setNumWorkers(v[0])} />
            </div>

            <div className="space-y-2">
              <Label>Chunks</Label>
              <Input type="number" min={1} max={256} value={numChunks} onChange={(e) => setNumChunks(Number(e.target.value))} />
            </div>

            <div className="space-y-2">
              <div className="flex justify-between">
                <Label>Network Latency</Label>
                <span className="text-xs text-muted-foreground">{networkLatencyMs} ms</span>
              </div>
              <Slider min={0} max={100} step={1} value={[networkLatencyMs]} onValueChange={(v) => setNetworkLatencyMs(v[0])} />
            </div>

            <Button className="w-full" onClick={() => mutation.mutate()} disabled={mutation.isPending || selectedPlugins.length < 2}>
              {mutation.isPending ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Comparing...
                </>
              ) : (
                <>
                  <GitCompare className="h-4 w-4" />
                  Compare Plugins
                </>
              )}
            </Button>
            {selectedPlugins.length < 2 && (
              <p className="text-xs text-destructive">Select at least 2 plugins to compare.</p>
            )}
          </CardContent>
        </Card>

        {/* Results */}
        <div className="space-y-6">
          {mutation.isError && (
            <Card className="border-destructive">
              <CardContent className="pt-6">
                <p className="text-destructive text-sm">Comparison failed. Make sure the API server is running on port 4400.</p>
              </CardContent>
            </Card>
          )}

          {result && (
            <>
              <ComparisonChart results={result.results} />
              <ComparisonTable results={result.results} />
            </>
          )}

          {!result && !mutation.isPending && (
            <Card className="flex h-[400px] items-center justify-center">
              <div className="text-center text-muted-foreground">
                <GitCompare className="mx-auto h-12 w-12 mb-3 opacity-20" />
                <p className="text-sm">Select plugins and run a comparison to see results.</p>
              </div>
            </Card>
          )}
        </div>
      </div>
    </div>
  )
}
