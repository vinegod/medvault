import { Outlet, useLocation } from "react-router-dom";
import { Header } from "./Header";
import { BottomNav } from "./BottomNav";

// Routes that are intentionally chrome-less (no header / no nav).
// Login & Register are full-screen — no nav makes sense before connecting.
const bareRoutes = new Set(["/login", "/register"]);

export function Layout() {
  const { pathname } = useLocation();
  const bare = bareRoutes.has(pathname);

  if (bare) return <Outlet />;

  return (
    <>
      <Header />
      <main style={main}>
        <Outlet />
      </main>
      <BottomNav />
    </>
  );
}

const main: React.CSSProperties = {
  flex: 1,
  width: "100%",
  maxWidth: 700,
  margin: "0 auto",
  padding: "1rem 1rem calc(var(--nav-h) + var(--safe-bottom) + 1rem)",
  // Bottom padding leaves room for fixed BottomNav so content isn't hidden.
};
