import React from "react"
import ReactDOM from "react-dom/client"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { RouterProvider, createRouter, createRootRoute, createRoute } from "@tanstack/react-router"
import { ThemeProvider } from "@/components/theme-provider"
import "./index.css"

import { DashboardLayout } from "@/components/layout"
import { SimulatePage } from "@/pages/simulate"
import { ComparePage } from "@/pages/compare"

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 30_000 },
  },
})

const rootRoute = createRootRoute({
  component: DashboardLayout,
})

const simulateRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: SimulatePage,
})

const compareRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/compare",
  component: ComparePage,
})

const routeTree = rootRoute.addChildren([simulateRoute, compareRoute])

const router = createRouter({ routeTree })

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router
  }
}

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    </ThemeProvider>
  </React.StrictMode>
)
