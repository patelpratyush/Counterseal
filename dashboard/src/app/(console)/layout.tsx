import Link from "next/link";
import { ShieldCheck, LogOut, LockKeyhole } from "lucide-react";
import { Suspense } from "react";
import { Navigation } from "@/components/navigation";
import { requireSession } from "@/lib/auth";
import { logout } from "@/app/actions";
import { ThemeSwitch } from "@/components/theme";
import { Button } from "@/components/ui/button";
export default async function Layout({
  children,
}: {
  children: React.ReactNode;
}) {
  await requireSession();
  return (
    <div className="console">
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <aside className="sidebar">
        <Link href="/" className="brand">
          <span className="brand-icon">
            <ShieldCheck size={21} />
          </span>
          HandoffGuard
        </Link>
        <div className="workspace">
          <span className="workspace-avatar">HG</span>
          <div>
            Local workspace<small>Authorization console</small>
          </div>
        </div>
        <span className="nav-label">WORKSPACE</span>
        <Suspense
          fallback={
            <nav>
              <Link className="nav-item" href="/">
                All runs
              </Link>
            </nav>
          }
        >
          <Navigation />
        </Suspense>
        <div className="sidebar-bottom">
          <div className="access-note">
            <LockKeyhole size={14} />
            <span>Read-only access</span>
          </div>
          <div className="user-row">
            <span className="avatar">OP</span>
            <div>
              Operator<small>Read-only viewer</small>
            </div>
            <form action={logout}>
              <Button
                type="submit"
                variant="ghost"
                size="icon"
                aria-label="Sign out"
              >
                <LogOut size={17} />
              </Button>
            </form>
          </div>
        </div>
      </aside>
      <div className="console-body">
        <header className="topbar">
          <span>
            <Link href="/">Workspace</Link> <span className="slash">/</span>{" "}
            Runs
          </span>
          <div>
            <span className="viewer-label">
              <LockKeyhole size={12} /> Viewer
            </span>
            <ThemeSwitch />
            <form action={logout} className="mobile-signout">
              <Button
                type="submit"
                variant="ghost"
                size="icon"
                aria-label="Sign out"
              >
                <LogOut size={17} />
              </Button>
            </form>
          </div>
        </header>
        <main id="main">{children}</main>
        <footer className="console-footer">
          <span>
            HandoffGuard <span className="slash">/</span> Authorization console
          </span>
          <span>Decision records · UTC</span>
        </footer>
      </div>
    </div>
  );
}
