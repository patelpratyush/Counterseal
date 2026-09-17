import { LoginForm } from "@/components/login-form";
import { ShieldCheck } from "lucide-react";
export default function Login() {
  return (
    <main className="login-page">
      <div className="login-brand">
        <ShieldCheck size={24} /> HandoffGuard
      </div>
      <section className="login-card">
        <span className="eyebrow">AUTHORIZATION OBSERVATORY</span>
        <h1>
          Every handoff.
          <br />
          Accounted for.
        </h1>
        <p>
          Follow delegated authority from the first agent to the final decision.
        </p>
        <LoginForm />
        <div className="login-foot">Private workspace · Read-only access</div>
      </section>
      <div className="login-grid" aria-hidden="true" />
    </main>
  );
}
