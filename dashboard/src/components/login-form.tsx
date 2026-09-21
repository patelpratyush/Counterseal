"use client";
import { useActionState, useState } from "react";
import { login } from "@/app/actions";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
export function LoginForm() {
  const [state, action, pending] = useActionState(login, { error: "" });
  const [username, setUsername] = useState("");
  return (
    <form action={action} className="login-form">
      <label htmlFor="username">Username</label>
      <Input id="username" name="username" autoComplete="username" required maxLength={64} autoCapitalize="none" spellCheck={false}
        value={username} onChange={event => setUsername(event.target.value)} />
      <label htmlFor="password">Password</label>
      <Input
        id="password"
        name="password"
        type="password"
        autoComplete="current-password"
        required
        maxLength={256}
      />
      {state.error ? (
        <p role="alert" className="danger-text">
          {state.error}
        </p>
      ) : null}
      <Button disabled={pending} type="submit">
        {pending ? "Signing in…" : "Open console →"}
      </Button>
    </form>
  );
}
