import Link from "next/link";
export default function NotFound() {
  return (
    <main className="empty-state error-state">
      <h1>Run not found.</h1>
      <p>This run may not exist in the connected workspace.</p>
      <Link href="/">Return to overview →</Link>
    </main>
  );
}
