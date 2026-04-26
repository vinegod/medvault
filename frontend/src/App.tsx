import { BrowserRouter, Routes, Route, Navigate, Outlet, useLocation } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { AuthProvider, useAuth } from "./contexts/AuthContext";
import { ToastProvider } from "./contexts/ToastContext";
import { Layout } from "./components/Layout";
import "./index.css";

import Login from "./pages/Login";
import Register from "./pages/Register";
import Medicines from "./pages/Medicines";
import MedicineForm from "./pages/MedicineForm";
import Alerts from "./pages/Alerts";
import Settings from "./pages/Settings";
import Household from "./pages/Household";
import JoinHousehold from "./pages/JoinHousehold";

// Server-only routes redirect to /settings if no server is connected.
function ConnectedOnly() {
  const { status } = useAuth();
  if (status !== "connected") return <Navigate to="/settings" replace />;
  return <Outlet />;
}

function InitializingScreen() {
  const { t } = useTranslation();
  return (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "center", height: "100dvh" }}>
      <p style={{ color: "var(--color-text-muted)" }}>{t("common.loading")}</p>
    </div>
  );
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

        <Route element={<ConnectedOnly />}>
          <Route path="/household" element={<Household />} />
          <Route path="/household/join" element={<JoinHousehold />} />
        </Route>
      </Route>

      <Route path="*" element={<Navigate to="/medicines" replace />} />
    </Routes>
  );
}

export default function App() {
  return (
    <AuthProvider>
      <ToastProvider>
        <BrowserRouter>
          <AppRoutes />
        </BrowserRouter>
      </ToastProvider>
    </AuthProvider>
  );
}
