import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import Layout from "./components/Layout";
import { ServersProvider } from "./context/servers";
import { getToken } from "./lib/api";
import Login from "./pages/Login";
import Overview from "./pages/Overview";
import ServerDetail from "./pages/ServerDetail";
import Servers from "./pages/Servers";
import Alerts from "./pages/Alerts";
import Crons from "./pages/Crons";
import Services from "./pages/Services";
import Files from "./pages/Files";
import Access from "./pages/Access";
import TerminalPage from "./pages/Terminal";
import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

function RequireAuth({ children }: { children: React.ReactNode }) {
  if (!getToken()) return <Navigate to="/login" replace />;
  return <>{children}</>;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route
            element={
              <RequireAuth>
                <ServersProvider>
                  <Layout />
                </ServersProvider>
              </RequireAuth>
            }
          >
            <Route path="/" element={<Overview />} />
            <Route path="/server/:id" element={<ServerDetail />} />
            <Route path="/terminal/:id" element={<TerminalPage />} />
            <Route path="/servers" element={<Servers />} />
            <Route path="/alerts" element={<Alerts />} />
            <Route path="/crons" element={<Crons />} />
            <Route path="/services" element={<Services />} />
            <Route path="/files" element={<Files />} />
            <Route path="/access" element={<Access />} />
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
