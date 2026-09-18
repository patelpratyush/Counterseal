"use client";
import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { Layers3, ShieldBan } from "lucide-react";
export function Navigation() {
  const pathname = usePathname();
  const params = useSearchParams();
  const blocked = pathname === "/" && params.get("filter") === "blocked";
  return (
    <nav aria-label="Workspace navigation">
      <Link
        href="/"
        className={`nav-item ${!blocked ? "active" : ""}`}
        aria-current={!blocked ? "page" : undefined}
      >
        <Layers3 size={17} />
        All runs
      </Link>
      <Link
        href="/?filter=blocked"
        className={`nav-item ${blocked ? "active" : ""}`}
        aria-current={blocked ? "page" : undefined}
      >
        <ShieldBan size={17} />
        Blocked runs
      </Link>
    </nav>
  );
}
