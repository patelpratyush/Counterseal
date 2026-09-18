import { LoginForm } from "@/components/login-form";
import { ShieldCheck } from "lucide-react";
export default function Login() {
  return (
    <main className="login-page">
      <div className="login-brand">
        <ShieldCheck size={24} /> HandoffGuard
      </div>
      <section className="login-card">
        <span className="eyebrow">HANDOFFGUARD CONSOLE</span>
        <h1>
          Sign in to your
          <br />
          workspace.
        </h1>
        <p>
          Inspect agent runs, delegated permissions, and authorization
          decisions.
        </p>
        <LoginForm />
        <div className="login-foot">Private workspace · Read-only access</div>
      </section>
      <div className="login-grid" aria-hidden="true" />
    </main>
  );
}
