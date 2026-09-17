"use client";
import { Button } from "@/components/ui/button";
export default function ErrorPage({ reset }: { reset: () => void }) {
  return (
    <div className="empty-state error-state" role="alert">
      <h2>We couldn’t load this view.</h2>
      <p>
        Check the control API connection and dashboard configuration, then try
        again.
      </p>
      <Button onClick={reset}>Try again</Button>
      <a href="/login">Return to sign in</a>
    </div>
  );
}
