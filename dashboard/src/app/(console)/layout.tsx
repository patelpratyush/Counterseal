import Link from "next/link";
import { ShieldCheck, LayoutGrid, ArrowUpRight, LogOut } from "lucide-react";
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
        <span className="nav-label">OBSERVE</span>
        <nav>
          <Link href="/" className="nav-item">
            <LayoutGrid size={17} /> Runs & overview
            <ArrowUpRight size={14} />
          </Link>
        </nav>
        <div className="sidebar-bottom">
          <div className="boundary">
            <ShieldCheck size={18} />
            <p>
              Authority only narrows.<small>Inspect every delegation.</small>
            </p>
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
            Workspace <span className="slash">/</span> Observability
          </span>
          <div>
            <span className="viewer-label">VIEWER</span>
            <ThemeSwitch />
          </div>
        </header>
        <main id="main">{children}</main>
        <footer className="console-footer">
          <span>
            HANDOFFGUARD <span className="slash">/</span> Trust, with a record.
          </span>
          <span>Decision records · UTC</span>
        </footer>
      </div>
    </div>
  );
}
