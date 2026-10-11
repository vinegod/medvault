import { BrowserRouter, Routes, Route, Navigate, Outlet, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { useEffect } from "react";
import { AuthProvider, useAuth } from "./contexts/AuthContext";
import { ToastProvider, useToast } from "./contexts/ToastContext";
import { Layout } from "./components/Layout";
import { setSyncErrorHandler } from "./services/sync";
import "./index.css";

import Login from "./pages/Login";
import Register from "./pages/Register";
import Medicines from "./pages/Medicines";
import MedicineForm from "./pages/MedicineForm";
import Alerts from "./pages/Alerts";
import Settings from "./pages/Settings";
import Household from "./pages/Household";
import JoinHousehold from "./pages/JoinHousehold";
import CreateHousehold from "./pages/CreateHousehold";
import OnboardingHousehold from "./pages/OnboardingHousehold";
import NotFound from "./pages/NotFound";
import Trash from "./pages/Trash";
import InventoryPrint from "./pages/InventoryPrint";
import Assistant from "./pages/Assistant";

// Server-only routes redirect to /settings if no server is connected.
function ConnectedOnly() {
  const { status } = useAuth();
  if (status !== "connected") return <Navigate to="/settings" replace />;
  return <Outlet />;
}

function InitializingScreen() {
  const { t } = useTranslation();
  return (
    <div
      style={{ display: "flex", alignItems: "center", justifyContent: "center", height: "100dvh" }}
    >
      <p style={{ color: "var(--color-text-muted)" }}>{t("common.loading")}</p>
    </div>
  );
}

function SyncErrorHandler() {
  const { t } = useTranslation();
  const toast = useToast();
  useEffect(() => {
    setSyncErrorHandler(() => toast.error(t("sync.error_failed")));
  }, [toast, t]);
  return null;
}

function AppRoutes() {
  const { initialized } = useAuth();
  const location = useLocation();

  if (!initialized) return <InitializingScreen />;

  return (
    <Routes>
      <Route element={<Layout />}>
        <Route path="/login" element={<Login />} />
        <Route path="/register" element={<Register />} />

        <Route index element={<Navigate to="/medicines" replace />} />
        <Route path="/medicines" element={<Medicines />} />
        <Route path="/medicines/new" element={<MedicineForm key="new" />} />
        <Route path="/medicines/:id/edit" element={<MedicineForm key={location.pathname} />} />
        <Route path="/alerts" element={<Alerts />} />
        <Route path="/settings" element={<Settings />} />
        <Route path="/trash" element={<Trash />} />
        <Route path="/inventory/print" element={<InventoryPrint />} />

        <Route element={<ConnectedOnly />}>
          <Route path="/household" element={<Household />} />
          <Route path="/household/new" element={<CreateHousehold />} />
          <Route path="/household/join" element={<JoinHousehold />} />
          <Route path="/onboarding/household" element={<OnboardingHousehold />} />
          <Route path="/assistant" element={<Assistant />} />
        </Route>
      </Route>

      <Route element={<Layout />}>
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <ToastProvider>
        <BrowserRouter>
          <SyncErrorHandler />
          <AppRoutes />
        </BrowserRouter>
      </ToastProvider>
    </AuthProvider>
  );
}
